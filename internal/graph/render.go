package graph

import (
	"fmt"
	"io"
	"strings"

	"github.com/quike/keepup/internal/config"
)

// Format selects the diagram syntax.
type Format string

const (
	// FormatMermaid renders natively in GitHub markdown, which is where these
	// diagrams usually end up.
	FormatMermaid Format = "mermaid"
	// FormatDot renders via Graphviz: `keepup graph ci -f dot | dot -Tsvg`.
	FormatDot Format = "dot"
)

// Formats lists the supported values, for flag help and validation.
func Formats() []string { return []string{string(FormatMermaid), string(FormatDot)} }

// Render writes the model in the requested format.
func Render(w io.Writer, f Format, m *Model) error {
	switch f {
	case FormatMermaid:
		return renderMermaid(w, m)
	case FormatDot:
		return renderDot(w, m)
	default:
		return fmt.Errorf("unknown graph format %q (expected one of: %s)", f, strings.Join(Formats(), ", "))
	}
}

func renderMermaid(w io.Writer, m *Model) error {
	b := &errWriter{w: w}
	b.printf("%%%% flow: %s (mode: %s)\ngraph TD\n", m.Flow, m.Mode)

	if m.Mode == config.ModeStep {
		for _, wave := range m.Waves {
			b.printf("  subgraph wave%d[%q]\n", wave.Index, waveLabel(wave))
			for _, g := range wave.Groups {
				b.printf("    %s\n", mermaidNode(m.node(g)))
			}
			b.printf("  end\n")
		}
		for i := 1; i < len(m.Waves); i++ {
			b.printf("  wave%d -.-> wave%d\n", m.Waves[i-1].Index, m.Waves[i].Index)
		}
	} else {
		for i := range m.Nodes {
			b.printf("  %s\n", mermaidNode(&m.Nodes[i]))
		}
	}

	for _, e := range m.Edges {
		b.printf("  %s --> %s\n", ident(e.From), ident(e.To))
	}

	if conditional := m.conditionalNodes(); len(conditional) > 0 {
		b.printf("  classDef conditional stroke-dasharray: 5 5\n")
		b.printf("  class %s conditional\n", strings.Join(conditional, ","))
	}
	return b.err
}

// mermaidNode renders a group as a cylinder when it declares cache:, a
// rectangle otherwise; the dashed conditional style is applied separately by
// classDef so the two cues never compete for the same syntax.
func mermaidNode(n *Node) string {
	label := n.Name
	if n.Description != "" {
		label = n.Name + "<br/>" + n.Description
	}
	if n.Cacheable {
		return fmt.Sprintf("%s[(%q)]", ident(n.Name), label)
	}
	return fmt.Sprintf("%s[%q]", ident(n.Name), label)
}

func renderDot(w io.Writer, m *Model) error {
	b := &errWriter{w: w}
	b.printf("// flow: %s (mode: %s)\ndigraph %q {\n", m.Flow, m.Mode, m.Flow)
	b.printf("  rankdir=TB;\n  node [shape=box];\n")

	if m.Mode == config.ModeStep {
		// Graphviz places unconnected clusters side by side, which would read
		// as parallelism. compound=true allows the cluster-to-cluster barrier
		// edges below to impose the real order.
		b.printf("  compound=true;\n")
		for _, wave := range m.Waves {
			b.printf("  subgraph cluster_wave%d {\n", wave.Index)
			b.printf("    label=%q;\n", waveLabel(wave))
			if wave.Conditional {
				b.printf("    style=dashed;\n")
			}
			for _, g := range wave.Groups {
				b.printf("    %s\n", dotNode(m.node(g)))
			}
			b.printf("  }\n")
		}
		b.printf("%s", dotBarriers(m.Waves))
	} else {
		for i := range m.Nodes {
			b.printf("  %s\n", dotNode(&m.Nodes[i]))
		}
	}

	for _, e := range m.Edges {
		b.printf("  %q -> %q;\n", e.From, e.To)
	}
	b.printf("}\n")
	return b.err
}

// dotBarriers joins consecutive waves. A compound edge needs real nodes as its
// endpoints even though ltail/lhead make it render cluster-to-cluster, so it
// anchors on the first group of each wave.
func dotBarriers(waves []Wave) string {
	var b strings.Builder
	for i := 1; i < len(waves); i++ {
		prev, cur := waves[i-1], waves[i]
		if len(prev.Groups) == 0 || len(cur.Groups) == 0 {
			continue
		}
		fmt.Fprintf(&b, "  %q -> %q [ltail=cluster_wave%d, lhead=cluster_wave%d, style=dashed];\n",
			prev.Groups[0], cur.Groups[0], prev.Index, cur.Index)
	}
	return b.String()
}

func dotNode(n *Node) string {
	attrs := []string{fmt.Sprintf("label=%q", dotLabel(n))}
	if n.Cacheable {
		attrs = append(attrs, "shape=cylinder")
	}
	if n.Conditional {
		attrs = append(attrs, "style=dashed")
	}
	return fmt.Sprintf("%q [%s];", n.Name, strings.Join(attrs, ", "))
}

func dotLabel(n *Node) string {
	if n.Description == "" {
		return n.Name
	}
	return n.Name + "\n" + n.Description
}

func waveLabel(w Wave) string {
	if w.Conditional {
		return fmt.Sprintf("step %d (conditional)", w.Index)
	}
	return fmt.Sprintf("step %d", w.Index)
}

func (m *Model) node(name string) *Node {
	for i := range m.Nodes {
		if m.Nodes[i].Name == name {
			return &m.Nodes[i]
		}
	}
	return &Node{Name: name}
}

func (m *Model) conditionalNodes() []string {
	var out []string
	for _, n := range m.Nodes {
		if n.Conditional {
			out = append(out, ident(n.Name))
		}
	}
	return out
}

// mermaidKeywords are parsed as syntax when they begin a line, so a group
// named "end" would close its subgraph instead of declaring a node.
var mermaidKeywords = map[string]struct{}{
	"end": {}, "graph": {}, "subgraph": {}, "class": {},
	"classdef": {}, "style": {}, "click": {}, "linkstyle": {},
}

// ident turns a group name into a Mermaid identifier, keeping the real name
// for the label. Dot needs no equivalent: it quotes node names.
func ident(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if _, reserved := mermaidKeywords[strings.ToLower(out)]; reserved {
		return out + "_"
	}
	return out
}

// errWriter keeps the first write error so renderers can stay linear instead
// of checking every Fprintf.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) printf(format string, args ...any) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprintf(e.w, format, args...)
}
