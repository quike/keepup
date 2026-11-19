package config

import (
	"fmt"
	"maps"
	"slices"

	"github.com/quike/keepup/internal/template"
)

// ExtractRefs returns every group name referenced by a group's commands via
// the template output() function (or the legacy "{{ output.X }}" form),
// across every entry in CommandList(). Duplicates are preserved by position.
// An error is returned when any template string is malformed, surfacing the
// problem at config-load time.
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
	for _, cs := range g.CommandList() {
		if err := collect(cs.Command); err != nil {
			return nil, err
		}
		for _, p := range cs.Params {
			if err := collect(p); err != nil {
				return nil, err
			}
		}
		if err := collect(cs.Dir); err != nil {
			return nil, err
		}
		// Sorted: map order is random, and these refs become dag edges and
		// graph output, which must not shuffle between runs.
		for _, k := range slices.Sorted(maps.Keys(cs.Env)) {
			if err := collect(cs.Env[k]); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// selfRefHint explains the intra-group limitation for multi-command groups: a
// commands: entry cannot consume an earlier entry's output, which is the
// usual intent behind a self-reference.
func selfRefHint(g *Group) string {
	if g == nil || len(g.CommandList()) <= 1 {
		return ""
	}
	return " (commands in a group cannot consume each other's output; split into separate groups or pipe within a single shell entry)"
}

// ValidateReferences runs strict-mode reference checks on every flow:
//
//   - every {{ output.X }} must point to a group that appears earlier in the
//     same flow (step mode: in an earlier step; dag mode: in the flow's run
//     set, with the resulting data DAG being acyclic — including references
//     inside `when:` predicates, which are treated as graph edges and
//     cycle-checked).
//   - every after: name on a dag run entry must be another member of the
//     same flow.
//   - dag mode additionally rejects cycles, counting data references and
//     after: ordering edges together.
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
		return c.checkDAGRefs(flowName, f, members, memberSet)
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
				if ref == member {
					return fmt.Errorf(
						"flow %q step %d: group %q references its own output%s",
						flowName, stepIdx+1, member, selfRefHint(g),
					)
				}
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
		if err := c.checkWhenRefs(flowName, stepIdx, step.When, memberSet, seen); err != nil {
			return err
		}
		for _, member := range step.Run {
			seen[member] = struct{}{}
		}
	}
	return nil
}

// checkWhenRefs validates that a step's `when` predicate only references groups
// produced by earlier steps (the same rule as param/command references).
func (c *Config) checkWhenRefs(flowName string, stepIdx int, when string, memberSet, seen map[string]struct{}) error {
	if when == "" {
		return nil
	}
	refs, err := template.Refs(when)
	if err != nil {
		return fmt.Errorf("flow %q step %d: when: %w", flowName, stepIdx+1, err)
	}
	for _, ref := range refs {
		if _, ok := memberSet[ref]; !ok {
			return fmt.Errorf("flow %q step %d: when references {{ output.%s }}, but %q is not part of this flow",
				flowName, stepIdx+1, ref, ref)
		}
		if _, ok := seen[ref]; !ok {
			return fmt.Errorf("flow %q step %d: when references {{ output.%s }}, but %q is not produced by an earlier step",
				flowName, stepIdx+1, ref, ref)
		}
	}
	return nil
}

// edgeSource says where a dag edge was declared, so a rejection can name the
// construct the user actually wrote.
type edgeSource int

const (
	fromCommand edgeSource = iota // {{ output.X }} in a command, param, dir or env
	fromWhen                      // {{ output.X }} in a run entry's when:
	fromAfter                     // a name in a run entry's after:
)

// dagEdges accumulates the edges of one dag flow for the cycle check. Data and
// ordering edges are stored alike: both constrain scheduling, so a cycle mixing
// them is still a cycle.
type dagEdges struct {
	flow      string
	memberSet map[string]struct{}
	defined   func(name string) bool // whether a group exists anywhere in the config
	adj       map[string][]string
	inDeg     map[string]int
}

func newDAGEdges(flow string, members []string, memberSet map[string]struct{}, defined func(string) bool) *dagEdges {
	d := &dagEdges{
		flow:      flow,
		memberSet: memberSet,
		defined:   defined,
		adj:       make(map[string][]string, len(members)),
		inDeg:     make(map[string]int, len(members)),
	}
	for _, m := range members {
		d.inDeg[m] = 0
	}
	return d
}

// add records from -> to after checking that from is a flow member and not to
// itself. selfHint extends the self-reference message for data edges.
func (d *dagEdges) add(from, to string, src edgeSource, selfHint string) error {
	if _, ok := d.memberSet[from]; !ok {
		return d.notInFlow(from, to, src)
	}
	if from == to {
		if src == fromAfter {
			return fmt.Errorf("flow %q: group %q cannot list itself in after", d.flow, to)
		}
		return fmt.Errorf("flow %q: group %q references its own output%s", d.flow, to, selfHint)
	}
	d.adj[from] = append(d.adj[from], to)
	d.inDeg[to]++
	return nil
}

// notInFlow phrases the rejection of an edge from a non-member after the
// construct that declared it.
func (d *dagEdges) notInFlow(from, to string, src edgeSource) error {
	switch src {
	case fromAfter:
		if !d.defined(from) {
			return fmt.Errorf("flow %q: group %q: after references %q, but no group %q is defined",
				d.flow, to, from, from)
		}
		return fmt.Errorf("flow %q: group %q: after references %q, but %q is not part of this flow",
			d.flow, to, from, from)
	case fromWhen:
		return fmt.Errorf("flow %q: group %q: when references {{ output.%s }}, but %q is not part of this flow",
			d.flow, to, from, from)
	case fromCommand:
		return fmt.Errorf("flow %q: group %q references {{ output.%s }}, but %q is not part of this flow",
			d.flow, to, from, from)
	}
	// Unreachable while every edgeSource has a case above; Go still needs a
	// terminating statement after a switch without a default.
	return fmt.Errorf("flow %q: group %q: unknown edge source %d", d.flow, to, src)
}

func (c *Config) checkDAGRefs(flowName string, f *Flow, members []string, memberSet map[string]struct{}) error {
	isDefined := func(name string) bool { return c.GroupByName(name) != nil }
	edges := newDAGEdges(flowName, members, memberSet, isDefined)
	// seen catches the same group listed twice in run: — both bare strings and
	// {group, when} maps. With per-entry `when:` predicates, last-wins would
	// silently swallow a predicate; reject it at load.
	seen := make(map[string]struct{}, len(f.Run))
	for i := range f.Run {
		m := f.Run[i].Group
		if _, dup := seen[m]; dup {
			return fmt.Errorf(
				"flow %q: group %q is listed more than once in run: (duplicate dag entries are not allowed)",
				flowName, m,
			)
		}
		seen[m] = struct{}{}
		if err := c.addRunEntryEdges(edges, &f.Run[i]); err != nil {
			return err
		}
	}
	return topoCheck(flowName, members, edges.adj, edges.inDeg)
}

// addRunEntryEdges records every edge into one run entry: data references in
// its group's commands and its when:, and ordering edges from its after:.
func (c *Config) addRunEntryEdges(edges *dagEdges, entry *RunEntry) error {
	m := entry.Group
	g := c.GroupByName(m)
	if g == nil {
		return fmt.Errorf("flow %q: group %q is not defined", edges.flow, m)
	}
	refs, err := ExtractRefs(g)
	if err != nil {
		return fmt.Errorf("flow %q: %w", edges.flow, err)
	}
	hint := selfRefHint(g)
	for _, ref := range refs {
		if err := edges.add(ref, m, fromCommand, hint); err != nil {
			return err
		}
	}
	// when: references create edges exactly like command/param refs.
	if entry.When != "" {
		whenRefs, werr := template.Refs(entry.When)
		if werr != nil {
			return fmt.Errorf("flow %q: group %q: when: %w", edges.flow, m, werr)
		}
		for _, ref := range whenRefs {
			if err := edges.add(ref, m, fromWhen, hint); err != nil {
				return err
			}
		}
	}
	for _, pred := range entry.After {
		if err := edges.add(pred, m, fromAfter, ""); err != nil {
			return err
		}
	}
	return nil
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
	return fmt.Errorf("flow %q: dependency cycle detected involving group %q (data references and after: edges)", flowName, anchor)
}
