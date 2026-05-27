package engine

import (
	"context"
	"fmt"
	"maps"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/plan"
)

// dagSched holds the scheduler-goroutine-local bookkeeping for a dag run. Every
// field and method here is touched only by the single scheduler goroutine, so
// none of it needs synchronization; workers communicate solely through doneCh
// and the snapMu-guarded snapshot owned by runDAGPlan.
type dagSched struct {
	e      *Engine
	p      *plan.Plan
	indeg  map[string]int
	skip   map[string]bool
	reason map[string]string
	ready  []string

	pending int
	err     error

	launch func(name string)
	cancel context.CancelFunc

	baseline func() map[string]string // stable snapshot for when: evaluation
}

// onDone resolves a terminal node's successors. A skipped node poisons its
// successors (first poisoner wins the reason); every successor's indeg is
// decremented and those that reach zero join the ready worklist.
func (s *dagSched) onDone(name string, wasSkipped bool) {
	s.pending--
	for _, succ := range s.p.Successors[name] {
		if wasSkipped && !s.skip[succ] {
			s.skip[succ] = true
			s.reason[succ] = fmt.Sprintf("upstream %q skipped", name)
		}
		s.indeg[succ]--
		if s.indeg[succ] == 0 {
			s.ready = append(s.ready, succ)
		}
	}
}

// decide reports whether name should be skipped, evaluating its when: predicate
// against a stable snapshot when one is declared. A predicate error records
// s.err, cancels the run, and reports skip=true to unwind the worklist quickly.
func (s *dagSched) decide(name string) bool {
	if s.skip[name] {
		return true
	}
	expr := s.p.When[name]
	if expr == "" {
		return false
	}
	run, err := s.e.evalWhen(expr, s.baseline())
	if err != nil {
		s.err = fmt.Errorf("flow %q: group %q: when: %w", s.p.Flow, name, err)
		s.cancel()
		return true
	}
	if !run {
		s.reason[name] = "when"
		return true
	}
	return false
}

// drainReady decides each ready node: skip (cascading synchronously) or launch.
// A skip can make successors ready, which may themselves skip, so this loops
// until the worklist is empty or a predicate error aborts the run.
func (s *dagSched) drainReady() {
	for len(s.ready) > 0 {
		name := s.ready[len(s.ready)-1]
		s.ready = s.ready[:len(s.ready)-1]

		if s.decide(name) {
			if s.err != nil {
				return
			}
			s.skip[name] = true
			s.e.emitGroupSkipped(name, s.reason[name])
			s.onDone(name, true)
			continue
		}
		s.launch(name)
	}
}

// runDAGPlan runs the topological closure of the plan. A group starts as soon
// as every group it references (via {{ output.X }} in command/params OR in its
// when: predicate) has finished. A group whose when: predicate renders falsey
// is skipped, and every group downstream of it cascades to skipped too — a
// consumer cannot run on a producer's missing output.
//
// All skip decisions and bookkeeping live in this single scheduler goroutine
// (the dagSched value); workers only run groups, publish outputs under snapMu,
// and signal completion on doneCh. That keeps the conditional logic free of
// additional synchronization.
func (e *Engine) runDAGPlan(ctx context.Context, p *plan.Plan, flow *config.Flow) error {
	env := resolveEnvelope(flow, nil)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	doneCh := make(chan string, len(p.Members))
	g, gctx := errgroup.WithContext(ctx)
	if e.maxConcurrency > 0 {
		g.SetLimit(e.maxConcurrency)
	}

	var (
		snapMu sync.RWMutex
		snap   = make(map[string]string, len(p.Members))
	)
	maps.Copy(snap, e.outputs.Snapshot())
	baseline := func() map[string]string {
		snapMu.RLock()
		defer snapMu.RUnlock()
		return cloneSnapshot(snap)
	}

	launch := func(name string) {
		group := e.groups[name]
		g.Go(func() error {
			if err := e.runGroup(gctx, &group, baseline(), env); err != nil {
				return err
			}
			if v, ok := e.outputs.Get(name); ok {
				snapMu.Lock()
				snap[name] = v
				snapMu.Unlock()
			}
			select {
			case doneCh <- name:
			case <-gctx.Done():
			}
			return nil
		})
	}

	s := &dagSched{
		e:        e,
		p:        p,
		indeg:    make(map[string]int, len(p.Members)),
		skip:     make(map[string]bool, len(p.Members)),
		reason:   make(map[string]string, len(p.Members)),
		ready:    append(make([]string, 0, len(p.Members)), p.Roots...),
		pending:  len(p.Members),
		launch:   launch,
		cancel:   cancel,
		baseline: baseline,
	}
	for _, m := range p.Members {
		s.indeg[m] = len(p.Predecessors[m])
	}

	s.drainReady()
	if s.err == nil {
		e.runDAGLoop(gctx, doneCh, s)
	}

	if err := g.Wait(); err != nil && s.err == nil {
		return err
	}
	return s.err
}

// runDAGLoop pumps completed groups from doneCh back into the scheduler until
// every group has terminated (run or skipped), a predicate error aborts, or the
// context is canceled.
func (e *Engine) runDAGLoop(gctx context.Context, doneCh <-chan string, s *dagSched) {
	for s.pending > 0 {
		select {
		case finished := <-doneCh:
			s.onDone(finished, false)
			s.drainReady()
			if s.err != nil {
				return
			}
		case <-gctx.Done():
			return
		}
	}
}

// emitGroupSkipped reports a skipped dag group: a log line carrying the reason
// and a group.end event marking it skipped on the structured stream.
func (e *Engine) emitGroupSkipped(name, reason string) {
	e.log.Info("group skipped", "group", name, "reason", reason)
	e.emitter.Emit(Event{Event: EventGroupEnd, Group: name, Status: StatusSkipped})
}

func cloneSnapshot(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	maps.Copy(out, m)
	return out
}
