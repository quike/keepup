package engine

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/quike/keepup/internal/plan"
)

// runStepPlan runs each wave of a step-mode plan in sequence. Within a wave,
// groups run in parallel; a barrier separates consecutive waves. Each wave
// sees a baseline snapshot of outputs from prior waves only.
func (e *Engine) runStepPlan(ctx context.Context, p *plan.Plan) error {
	for waveIdx, wave := range p.Waves {
		e.log.Info("step", "step", waveIdx+1, "groups", wave)

		baseline := e.outputs.Snapshot()
		g, gctx := errgroup.WithContext(ctx)
		if e.maxConcurrency > 0 {
			g.SetLimit(e.maxConcurrency)
		}
		for _, name := range wave {
			group := e.groups[name]
			g.Go(func() error { return e.runGroup(gctx, &group, baseline) })
		}
		if err := g.Wait(); err != nil {
			return fmt.Errorf("step %d: %w", waveIdx+1, err)
		}
		e.log.Info("step completed", "step", waveIdx+1)
	}
	return nil
}
