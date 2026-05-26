package cmd

import (
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"

	"github.com/quike/keepup/internal/config"
)

func newGraphCmd(opts *runtimeOpts, stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "graph [flow]",
		Short: "Emit a Mermaid diagram of the data DAG for a flow",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.load(cmd.OutOrStdout()); err != nil {
				return err
			}
			flowName := opts.cfg.Default
			if len(args) == 1 {
				flowName = args[0]
			}
			if flowName == "" {
				return fmt.Errorf("no flow specified and no default declared")
			}
			flow, ok := opts.cfg.Flows[flowName]
			if !ok {
				return fmt.Errorf("flow %q not found", flowName)
			}
			return emitMermaid(stdout, flowName, opts.cfg, &flow)
		},
	}
}

// emitMermaid writes a Mermaid graph TD definition. Nodes are groups in the
// flow; edges go from referenced group → referencing group (data direction).
// Step boundaries are not drawn — step mode and dag mode produce the same
// data-flow picture, which is the semantically meaningful one.
func emitMermaid(out io.Writer, flowName string, cfg *config.Config, flow *config.Flow) error {
	members := flow.Members()
	memberSet := make(map[string]struct{}, len(members))
	for _, m := range members {
		memberSet[m] = struct{}{}
	}

	if _, err := fmt.Fprintf(out, "%%%% flow: %s (mode: %s)\ngraph TD\n", flowName, flow.Mode); err != nil {
		return err
	}

	// Declare nodes in declaration order so output is stable.
	for _, m := range members {
		g := cfg.GroupByName(m)
		label := m
		if g != nil && g.Description != "" {
			label = fmt.Sprintf("%s<br/>%s", m, g.Description)
		}
		if _, err := fmt.Fprintf(out, "  %s[%q]\n", nodeID(m), label); err != nil {
			return err
		}
	}

	// Collect edges, sorted for determinism.
	type edge struct{ from, to string }
	edges := make([]edge, 0)
	for _, m := range members {
		g := cfg.GroupByName(m)
		seen := make(map[string]struct{})
		for _, ref := range config.ExtractRefs(g) {
			if _, in := memberSet[ref]; !in {
				continue
			}
			if _, dup := seen[ref]; dup {
				continue
			}
			seen[ref] = struct{}{}
			edges = append(edges, edge{from: ref, to: m})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].from != edges[j].from {
			return edges[i].from < edges[j].from
		}
		return edges[i].to < edges[j].to
	})
	for _, e := range edges {
		if _, err := fmt.Fprintf(out, "  %s --> %s\n", nodeID(e.from), nodeID(e.to)); err != nil {
			return err
		}
	}
	return nil
}

// nodeID turns a group name into a Mermaid-safe identifier. Hyphens are
// replaced with underscores; other characters are preserved.
func nodeID(name string) string {
	b := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			b = append(b, c)
		default:
			b = append(b, '_')
		}
	}
	return string(b)
}
