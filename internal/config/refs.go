package config

import (
	"fmt"

	"github.com/quike/keepup/internal/template"
)

// ExtractRefs returns every group name referenced by a group's command or
// params via the template output() function (or the legacy "{{ output.X }}"
// form). Duplicates are preserved by position. An error is returned when any
// template string is malformed, surfacing the problem at config-load time.
func ExtractRefs(g *Group) ([]string, error) {
	out := make([]string, 0)
	collect := func(s string) error {
		refs, err := template.Refs(s)
		if err != nil {
			return fmt.Errorf("group %q: %w", g.Name, err)
		}
		out = append(out, refs...)
		return nil
	}
	if err := collect(g.Command); err != nil {
		return nil, err
	}
	for _, p := range g.Params {
		if err := collect(p); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ValidateReferences runs strict-mode reference checks on every flow:
//
//   - every {{ output.X }} must point to a group that appears earlier in the
//     same flow (step mode: in an earlier step; dag mode: in the flow's run
//     set, with the resulting data DAG being acyclic).
//   - dag mode additionally rejects cycles.
//
// It is invoked from normalizeAndValidate so a single LoadConfig surfaces
// every error.
func (c *Config) ValidateReferences() error {
	for name, flow := range c.Flows {
		members := flow.Members()
		if err := c.checkFlowRefs(name, &flow, members); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) checkFlowRefs(flowName string, f *Flow, members []string) error {
	memberSet := make(map[string]struct{}, len(members))
	for _, m := range members {
		memberSet[m] = struct{}{}
	}
	switch f.Mode {
	case ModeStep:
		return c.checkStepRefs(flowName, f, memberSet)
	case ModeDAG:
		return c.checkDAGRefs(flowName, members, memberSet)
	}
	return nil
}

func (c *Config) checkStepRefs(flowName string, f *Flow, memberSet map[string]struct{}) error {
	seen := make(map[string]struct{})
	for stepIdx, step := range f.Steps {
		for _, member := range step.Run {
			g := c.GroupByName(member)
			if g == nil {
				return fmt.Errorf("flow %q step %d: group %q is not defined", flowName, stepIdx+1, member)
			}
			refs, err := ExtractRefs(g)
			if err != nil {
				return fmt.Errorf("flow %q step %d: %w", flowName, stepIdx+1, err)
			}
			for _, ref := range refs {
				if _, ok := memberSet[ref]; !ok {
					return fmt.Errorf(
						"flow %q step %d: group %q references {{ output.%s }}, but %q is not part of this flow",
						flowName, stepIdx+1, member, ref, ref,
					)
				}
				if _, ok := seen[ref]; !ok {
					return fmt.Errorf(
						"flow %q step %d: group %q references {{ output.%s }}, but %q is not produced by an earlier step",
						flowName, stepIdx+1, member, ref, ref,
					)
				}
			}
		}
		for _, member := range step.Run {
			seen[member] = struct{}{}
		}
	}
	return nil
}

func (c *Config) checkDAGRefs(flowName string, members []string, memberSet map[string]struct{}) error {
	adj := make(map[string][]string, len(members))
	inDeg := make(map[string]int, len(members))
	for _, m := range members {
		inDeg[m] = 0
	}
	for _, m := range members {
		g := c.GroupByName(m)
		if g == nil {
			return fmt.Errorf("flow %q: group %q is not defined", flowName, m)
		}
		refs, err := ExtractRefs(g)
		if err != nil {
			return fmt.Errorf("flow %q: %w", flowName, err)
		}
		for _, ref := range refs {
			if _, ok := memberSet[ref]; !ok {
				return fmt.Errorf(
					"flow %q: group %q references {{ output.%s }}, but %q is not part of this flow",
					flowName, m, ref, ref,
				)
			}
			if ref == m {
				return fmt.Errorf("flow %q: group %q references its own output", flowName, m)
			}
			adj[ref] = append(adj[ref], m)
			inDeg[m]++
		}
	}
	return topoCheck(flowName, members, adj, inDeg)
}

// topoCheck runs Kahn's algorithm; an unvisited node after the sweep means
// the graph contains a cycle, and we surface a still-positive in-degree node
// as the anchor.
func topoCheck(flowName string, members []string, adj map[string][]string, inDeg map[string]int) error {
	queue := make([]string, 0, len(members))
	for _, m := range members {
		if inDeg[m] == 0 {
			queue = append(queue, m)
		}
	}
	visited := 0
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		visited++
		for _, succ := range adj[n] {
			inDeg[succ]--
			if inDeg[succ] == 0 {
				queue = append(queue, succ)
			}
		}
	}
	if visited == len(members) {
		return nil
	}
	var anchor string
	for _, m := range members {
		if inDeg[m] > 0 {
			anchor = m
			break
		}
	}
	return fmt.Errorf("flow %q: data-reference cycle detected involving group %q", flowName, anchor)
}
