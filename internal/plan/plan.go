// Package plan turns a validated config.Flow into a schedulable plan.
//
// Planning is pure: no I/O, no goroutines, fully unit-testable. The engine
// consumes the resulting Plan and runs the work.
package plan

import (
	"fmt"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/template"
)

// Plan is the schedulable shape of a Flow.
//
// For step mode, Waves holds one entry per declared step (preserving order
// and parallelism). For dag mode, Waves is nil and Predecessors / Successors
// drive a Kahn-style scheduler; Roots is the initial ready set. Predecessors
// holds every scheduling edge; EdgeKind tells data edges from after: ordering
// edges, because the two react differently to a skipped predecessor.
// When carries the raw when: predicate for each group that declares one
// (dag mode only; absent = unconditional).
type Plan struct {
	Flow         string
	Mode         config.Mode
	Members      []string            // every group in the flow, in declaration order
	Waves        [][]string          // step mode only
	Predecessors map[string][]string // dag mode only
	Successors   map[string][]string // dag mode only
	Roots        []string            // dag mode only
	When         map[string]string   // dag mode only: group -> when predicate (absent = unconditional)

	orderEdges map[edge]struct{} // dag mode only: edges declared solely by after:
}

// EdgeKind classifies a dag edge.
type EdgeKind int

const (
	// EdgeData means the successor consumes the predecessor's output, so it
	// cannot run when the predecessor was skipped.
	EdgeData EdgeKind = iota
	// EdgeOrder is an after: edge: the successor only waits for the
	// predecessor to finish and runs even when it was skipped.
	EdgeOrder
)

type edge struct{ from, to string }

// EdgeKind reports how from -> to was declared. A pair that is both consumed
// and listed in after: is a data edge; the stronger contract wins.
func (p *Plan) EdgeKind(from, to string) EdgeKind {
	if _, ok := p.orderEdges[edge{from, to}]; ok {
		return EdgeOrder
	}
	return EdgeData
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
	for i := range flow.Run {
		memberSet[flow.Run[i].Group] = struct{}{}
	}
	p.Predecessors = make(map[string][]string, len(flow.Run))
	p.Successors = make(map[string][]string, len(flow.Run))
	p.When = make(map[string]string)
	p.orderEdges = make(map[edge]struct{})

	for i := range flow.Run {
		m := flow.Run[i].Group
		w := flow.Run[i].When
		if w != "" {
			p.When[m] = w
		}
		seenPred := make(map[string]struct{})
		// addEdge reports whether from -> m was new; duplicates collapse so the
		// scheduler counts each predecessor once.
		addEdge := func(from string) bool {
			if _, in := memberSet[from]; !in {
				return false // ValidateReferences already rejected out-of-flow refs
			}
			if _, dup := seenPred[from]; dup {
				return false
			}
			seenPred[from] = struct{}{}
			p.Predecessors[m] = append(p.Predecessors[m], from)
			p.Successors[from] = append(p.Successors[from], m)
			return true
		}
		for _, ref := range dataRefs(cfg.GroupByName(m), w) {
			addEdge(ref)
		}
		// Data edges are added first so an after: naming a consumed producer
		// collapses into the data edge instead of weakening it.
		for _, pred := range flow.Run[i].After {
			if addEdge(pred) {
				p.orderEdges[edge{pred, m}] = struct{}{}
			}
		}
	}
	for i := range flow.Run {
		m := flow.Run[i].Group
		if len(p.Predecessors[m]) == 0 {
			p.Roots = append(p.Roots, m)
		}
	}
}

// dataRefs returns the groups whose output g consumes, in its commands or in
// its run entry's when: predicate.
func dataRefs(g *config.Group, when string) []string {
	// Refs cannot error here: NewConfig validated every template already.
	refs, _ := config.ExtractRefs(g)
	if when != "" {
		whenRefs, _ := template.Refs(when)
		refs = append(refs, whenRefs...)
	}
	return refs
}
