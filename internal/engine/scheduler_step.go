package engine

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/plan"
	"github.com/quike/keepup/internal/result"
	"github.com/quike/keepup/internal/template"
)

// evalWhen renders a step's `when` predicate and reports whether the step
// should run. The result is falsey (skip) for "", "false", "0", "no", "off".
func (e *Engine) evalWhen(expr string, baseline map[string]result.RunResult) (bool, error) {
	out, err := e.expander.Expand(expr, template.Data{Outputs: baseline, Env: e.cfg.Env})
	if err != nil {
		return false, err
	}
	switch strings.TrimSpace(strings.ToLower(out)) {
	case "", "false", "0", "no", "off": //nolint:goconst // falsey-literal set
		return false, nil
	default:
		return true, nil
	}
}

// runStepPlan runs each wave of a step-mode plan in sequence. Within a wave,
// groups run in parallel; a barrier separates consecutive waves. Each wave
// sees a baseline snapshot of outputs from prior waves only, and runs under
// the envelope resolved from its step (overriding the flow defaults).
func (e *Engine) runStepPlan(ctx context.Context, p *plan.Plan, flow *config.Flow) error {
	for waveIdx, wave := range p.Waves {
		step := &flow.Steps[waveIdx]
		e.log.Info("step", "step", waveIdx+1, "groups", wave)

		baseline := e.outputs.Snapshot()
		if step.When != "" {
			run, err := e.evalWhen(step.When, baseline)
			if err != nil {
				return fmt.Errorf("step %d: when: %w", waveIdx+1, err)
			}
			if !run {
				e.log.Info("step skipped", "step", waveIdx+1, "reason", "when", "predicate", step.When)
				for _, name := range step.Run {
					e.outputs.Set(name, result.RunResult{Status: resultStatusSkipped})
				}
				continue
			}
		}

		env := resolveEnvelope(flow, step)
		g, gctx := errgroup.WithContext(ctx)
		if e.maxConcurrency > 0 {
			g.SetLimit(e.maxConcurrency)
		}
		for _, name := range wave {
			group := e.groups[name]
			g.Go(func() error { return e.runGroup(gctx, &group, baseline, env) })
		}
		if err := g.Wait(); err != nil {
			return fmt.Errorf("step %d: %w", waveIdx+1, err)
		}
		e.log.Info("step completed", "step", waveIdx+1)
	}
	return nil
}
