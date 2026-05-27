// Package plan turns a validated config.Flow into a schedulable plan.
//
// Planning is pure: no I/O, no goroutines, fully unit-testable. The engine
// consumes the resulting Plan and runs the work.
package plan

import (
	"fmt"

	"github.com/quike/keepup/internal/config"
)

// Plan is the schedulable shape of a Flow.
//
// For step mode, Waves holds one entry per declared step (preserving order
// and parallelism). For dag mode, Waves is nil and Predecessors / Successors
// drive a Kahn-style scheduler; Roots is the initial ready set.
type Plan struct {
	Flow         string
	Mode         config.Mode
	Members      []string            // every group in the flow, in declaration order
	Waves        [][]string          // step mode only
	Predecessors map[string][]string // dag mode only
	Successors   map[string][]string // dag mode only
	Roots        []string            // dag mode only
}

// Build returns a Plan for the named flow.
//
// The caller is expected to have called Config.ValidateReferences (it runs
// inside NewConfig); Build trusts that contract and only re-checks structural
// invariants needed for scheduling.
func Build(cfg *config.Config, flowName string) (*Plan, error) {
	flow, ok := cfg.Flows[flowName]
	if !ok {
		return nil, fmt.Errorf("flow %q not found", flowName)
	}
	p := &Plan{Flow: flowName, Mode: flow.Mode, Members: flow.Members()}
	switch flow.Mode {
	case config.ModeStep:
		p.Waves = make([][]string, len(flow.Steps))
		for i, s := range flow.Steps {
			p.Waves[i] = append([]string(nil), s.Run...)
		}
	case config.ModeDAG:
		buildDAGEdges(cfg, &flow, p)
	default:
		return nil, fmt.Errorf("flow %q: unsupported mode %q", flowName, flow.Mode)
	}
	return p, nil
}

func buildDAGEdges(cfg *config.Config, flow *config.Flow, p *Plan) {
	memberSet := make(map[string]struct{}, len(flow.Run))
	for _, m := range flow.Run {
		memberSet[m] = struct{}{}
	}
	p.Predecessors = make(map[string][]string, len(flow.Run))
	p.Successors = make(map[string][]string, len(flow.Run))

	for _, m := range flow.Run {
		g := cfg.GroupByName(m)
		seenPred := make(map[string]struct{})
		// Refs cannot error here: NewConfig validated every template already.
		refs, _ := config.ExtractRefs(g)
		for _, ref := range refs {
			if _, in := memberSet[ref]; !in {
				continue // ValidateReferences would have rejected this
			}
			if _, dup := seenPred[ref]; dup {
				continue
			}
			seenPred[ref] = struct{}{}
			p.Predecessors[m] = append(p.Predecessors[m], ref)
			p.Successors[ref] = append(p.Successors[ref], m)
		}
	}
	for _, m := range flow.Run {
		if len(p.Predecessors[m]) == 0 {
			p.Roots = append(p.Roots, m)
		}
	}
}
