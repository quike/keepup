package template

import (
	"fmt"
	"text/template"
	"text/template/parse"

	"github.com/Masterminds/sprig/v3"

	"github.com/quike/keepup/internal/result"
)

// Refs returns the group names a template references via output("X"), in
// encounter order (duplicates preserved; callers de-duplicate as needed).
// Both the legacy "{{ output.X }}" form and the function form are handled.
//
// Only string-literal arguments are extractable; a dynamically-computed name
// (e.g. output (printf "g%d" 1)) cannot be resolved statically and is ignored.
func Refs(s string) ([]string, error) {
	fm := sprig.TxtFuncMap()
	// Stub the keepup functions so parsing succeeds without real data.
	fm["output"] = func(string) string { return "" }
	fm["out"] = func(string) result.RunResult { return result.RunResult{} }
	fm["env"] = func(string) string { return "" }

	t, err := template.New("ref").Funcs(fm).Parse(normalize(s))
	if err != nil {
		return nil, fmt.Errorf("parse template %q: %w", s, err)
	}
	var refs []string
	walk(t.Root, &refs)
	return refs, nil
}

func walk(n parse.Node, refs *[]string) {
	switch v := n.(type) {
	case *parse.ListNode:
		if v == nil {
			return
		}
		for _, c := range v.Nodes {
			walk(c, refs)
		}
	case *parse.ActionNode:
		walkPipe(v.Pipe, refs)
	case *parse.IfNode:
		walkBranch(&v.BranchNode, refs)
	case *parse.RangeNode:
		walkBranch(&v.BranchNode, refs)
	case *parse.WithNode:
		walkBranch(&v.BranchNode, refs)
	case *parse.TemplateNode:
		walkPipe(v.Pipe, refs)
	}
}

func walkBranch(b *parse.BranchNode, refs *[]string) {
	walkPipe(b.Pipe, refs)
	walk(b.List, refs)
	walk(b.ElseList, refs)
}

func walkPipe(p *parse.PipeNode, refs *[]string) {
	if p == nil {
		return
	}
	for _, cmd := range p.Cmds {
		walkCommand(cmd, refs)
	}
}

func walkCommand(c *parse.CommandNode, refs *[]string) {
	if len(c.Args) >= 2 {
		if id, ok := c.Args[0].(*parse.IdentifierNode); ok &&
			(id.Ident == "output" || id.Ident == "out") {
			if s, ok := c.Args[1].(*parse.StringNode); ok {
				*refs = append(*refs, s.Text)
			}
		}
	}
	// Recurse into parenthesized sub-pipelines, e.g. {{ if (output "x") }}.
	for _, a := range c.Args {
		if pipe, ok := a.(*parse.PipeNode); ok {
			walkPipe(pipe, refs)
		}
	}
}
