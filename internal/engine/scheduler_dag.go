package engine

import (
	"context"
	"maps"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/quike/keepup/internal/plan"
)

// runDAGPlan runs the topological closure of the plan. A group starts as soon
// as every group it references via {{ output.X }} has finished. The classical
// Kahn-style ready queue is layered on top of errgroup so cancellation and
// max-concurrency carry over without bespoke synchronization.
func (e *Engine) runDAGPlan(ctx context.Context, p *plan.Plan) error {
	// indeg tracks unresolved predecessors per group. It is mutated by the
	// scheduler goroutine only.
	indeg := make(map[string]int, len(p.Members))
	for _, m := range p.Members {
		indeg[m] = len(p.Predecessors[m])
	}

	// doneCh carries finished group names from workers back to the scheduler.
	doneCh := make(chan string, len(p.Members))

	g, gctx := errgroup.WithContext(ctx)
	if e.maxConcurrency > 0 {
		g.SetLimit(e.maxConcurrency)
	}

	// Outputs are appended to a flow-local snapshot as groups finish. This
	// snapshot is what we hand to each newly-ready group so reads are stable.
	var (
		snapMu sync.RWMutex
		snap   = make(map[string]string, len(p.Members))
	)
	maps.Copy(snap, e.outputs.Snapshot())

	launch := func(name string) {
		group := e.groups[name]
		g.Go(func() error {
			snapMu.RLock()
			baseline := cloneSnapshot(snap)
			snapMu.RUnlock()
			if err := e.runGroup(gctx, &group, baseline); err != nil {
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

	pending := len(p.Members)
	for _, r := range p.Roots {
		launch(r)
	}

	// Scheduler loop: dispatch successors as their predecessors complete.
loop:
	for pending > 0 {
		select {
		case finished := <-doneCh:
			pending--
			for _, succ := range p.Successors[finished] {
				indeg[succ]--
				if indeg[succ] == 0 {
					launch(succ)
				}
			}
		case <-gctx.Done():
			break loop
		}
	}

	return g.Wait()
}

func cloneSnapshot(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	maps.Copy(out, m)
	return out
}
