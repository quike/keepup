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

// dagScheduler holds the scheduler-goroutine-local bookkeeping for a dag run.
// Every field and method here is touched only by the single scheduler goroutine,
// so none of it needs synchronization; workers communicate solely through
// doneCh and the snapMu-guarded snapshot owned by runDAGPlan.
type dagScheduler struct {
	engine *Engine
	plan   *plan.Plan

	// unresolvedPreds counts each group's predecessors that have not yet
	// reached a terminal state (run or skipped). A group becomes ready when
	// its count drops to zero.
	unresolvedPreds map[string]int
	// skipped marks groups the scheduler has decided to skip — either because
	// their own when: rendered falsey, or because they are downstream of a
	// skipped group (cascade).
	skipped map[string]bool
	// skipReason carries the human-readable reason for each skip ("when" or
	// `upstream "<name>" skipped`), surfaced to logs and the event stream.
	skipReason map[string]string
	// ready is the worklist of groups whose predecessors are all terminal and
	// that still need a skip-or-launch decision.
	ready []string

	// remaining is the count of groups not yet at a terminal state; the run
	// is complete when it reaches zero.
	remaining int
	// schedErr records a scheduler-originated failure (e.g. a when: render
	// error). It is propagated ahead of any worker error from g.Wait().
	schedErr error

	launch func(name string)
	cancel context.CancelFunc

	baseline func() map[string]string // stable snapshot for when: evaluation
}

// onDone resolves a terminal node's successors. A skipped node poisons its
// successors (first poisoner wins the reason); every successor's unresolved
// predecessor count is decremented and those that reach zero join the ready
// worklist.
func (s *dagScheduler) onDone(name string, wasSkipped bool) {
	s.remaining--
	for _, succ := range s.plan.Successors[name] {
		if wasSkipped && !s.skipped[succ] {
			s.skipped[succ] = true
			s.skipReason[succ] = fmt.Sprintf("upstream %q skipped", name)
		}
		s.unresolvedPreds[succ]--
		if s.unresolvedPreds[succ] == 0 {
			s.ready = append(s.ready, succ)
		}
	}
}

// decide reports whether name should be skipped, evaluating its when: predicate
// against a stable snapshot when one is declared. A predicate error records
// schedErr, cancels the run, and reports skip=true to unwind the worklist
// quickly.
func (s *dagScheduler) decide(name string) bool {
	if s.skipped[name] {
		return true
	}
	expr := s.plan.When[name]
	if expr == "" {
		return false
	}
	run, err := s.engine.evalWhen(expr, s.baseline())
	if err != nil {
		s.schedErr = fmt.Errorf("flow %q: group %q: when: %w", s.plan.Flow, name, err)
		s.cancel()
		return true
	}
	if !run {
		s.skipReason[name] = "when"
		return true
	}
	return false
}

// drainReady decides each ready node: skip (cascading synchronously) or launch.
// A skip can make successors ready, which may themselves skip, so this loops
// until the worklist is empty or a predicate error aborts the run.
func (s *dagScheduler) drainReady() {
	for len(s.ready) > 0 {
		name := s.ready[len(s.ready)-1]
		s.ready = s.ready[:len(s.ready)-1]

		if s.decide(name) {
			if s.schedErr != nil {
				return
			}
			s.skipped[name] = true
			s.engine.emitGroupSkipped(name, s.skipReason[name])
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
// (the dagScheduler value); workers only run groups, publish outputs under
// snapMu, and signal completion on doneCh. That keeps the conditional logic
// free of additional synchronization.
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

	s := &dagScheduler{
		engine:          e,
		plan:            p,
		unresolvedPreds: make(map[string]int, len(p.Members)),
		skipped:         make(map[string]bool, len(p.Members)),
		skipReason:      make(map[string]string, len(p.Members)),
		ready:           append(make([]string, 0, len(p.Members)), p.Roots...),
		remaining:       len(p.Members),
		launch:          launch,
		cancel:          cancel,
		baseline:        baseline,
	}
	for _, m := range p.Members {
		s.unresolvedPreds[m] = len(p.Predecessors[m])
	}

	s.drainReady()
	if s.schedErr == nil {
		e.runDAGLoop(gctx, doneCh, s)
	}

	if err := g.Wait(); err != nil && s.schedErr == nil {
		return err
	}
	return s.schedErr
}

// runDAGLoop pumps completed groups from doneCh back into the scheduler until
// every group has terminated (run or skipped), a predicate error aborts, or the
// context is canceled.
func (e *Engine) runDAGLoop(gctx context.Context, doneCh <-chan string, s *dagScheduler) {
	for s.remaining > 0 {
		select {
		case finished := <-doneCh:
			s.onDone(finished, false)
			s.drainReady()
			if s.schedErr != nil {
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
	e.emitter.Emit(Event{Event: EventGroupEnd, Group: name, Status: StatusSkipped, Reason: reason})
}

func cloneSnapshot(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	maps.Copy(out, m)
	return out
}
