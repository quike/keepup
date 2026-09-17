// Package graph turns a validated flow into a renderable diagram model and
// emits it as Mermaid or Graphviz dot.
//
// The model is derived from the same plan.Plan the engine schedules, so the
// picture cannot drift from what actually runs.
package graph

import (
	"sort"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/plan"
)

// Node is one group in the flow.
type Node struct {
	Name        string
	Description string
	// Conditional marks a dag-mode group carrying a when: predicate. In step
	// mode the predicate belongs to the wave, not the group.
	Conditional bool
	Cacheable   bool
}

// Edge is a data dependency, drawn from the producing group to the consumer.
type Edge struct {
	From string
	To   string
}

// Wave is one step-mode execution wave, in declared order.
type Wave struct {
	Index       int
	Groups      []string
	Conditional bool
}

// Model is everything a renderer needs. Waves is empty in dag mode.
type Model struct {
	Flow  string
	Mode  config.Mode
	Nodes []Node
	Edges []Edge
	Waves []Wave
}

// Build derives the diagram model for a flow.
func Build(cfg *config.Config, flowName string) (*Model, error) {
	p, err := plan.Build(cfg, flowName)
	if err != nil {
		return nil, err
	}
	flow := cfg.Flows[flowName]
	m := &Model{Flow: flowName, Mode: p.Mode}

	for _, name := range p.Members {
		g := cfg.GroupByName(name)
		n := Node{Name: name}
		if g != nil {
			n.Description = g.Description
			n.Cacheable = g.Cache != nil
		}
		_, n.Conditional = p.When[name]
		m.Nodes = append(m.Nodes, n)
	}

	if p.Mode == config.ModeDAG {
		m.Edges = dagEdges(p)
	} else {
		m.Edges = stepEdges(cfg, p.Members)
		m.Waves = waves(p, &flow)
	}
	return m, nil
}

// dagEdges reads the scheduler's own predecessor map, so every dependency it
// enforces — including those a when: predicate introduces — is drawn.
func dagEdges(p *plan.Plan) []Edge {
	var edges []Edge
	for _, to := range p.Members {
		for _, from := range p.Predecessors[to] {
			edges = append(edges, Edge{From: from, To: to})
		}
	}
	return sortEdges(edges)
}

// stepEdges draws the data dependencies between groups in a step-mode flow.
// They do not set the ordering (waves do) but they explain it.
func stepEdges(cfg *config.Config, members []string) []Edge {
	memberSet := make(map[string]struct{}, len(members))
	for _, m := range members {
		memberSet[m] = struct{}{}
	}
	var edges []Edge
	for _, to := range members {
		seen := make(map[string]struct{})
		refs, _ := config.ExtractRefs(cfg.GroupByName(to)) // config validated these
		for _, from := range refs {
			if _, in := memberSet[from]; !in {
				continue
			}
			if _, dup := seen[from]; dup {
				continue
			}
			seen[from] = struct{}{}
			edges = append(edges, Edge{From: from, To: to})
		}
	}
	return sortEdges(edges)
}

func waves(p *plan.Plan, flow *config.Flow) []Wave {
	out := make([]Wave, 0, len(p.Waves))
	for i, groups := range p.Waves {
		w := Wave{Index: i + 1, Groups: groups}
		if i < len(flow.Steps) {
			w.Conditional = flow.Steps[i].When != ""
		}
		out = append(out, w)
	}
	return out
}

func sortEdges(edges []Edge) []Edge {
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	return edges
}
