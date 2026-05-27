// Package template renders keepup parameter/command strings.
//
// It owns the Expander abstraction the engine depends on (dependency
// inversion): the engine asks "expand this string against these outputs and
// env" without knowing the implementation. The default implementation is Go's
// text/template with the sprig function library, plus two keepup-specific
// functions:
//
//	output "name"   → the captured stdout of a prior group
//	env    "KEY"    → a value from the merged keepup environment
//
// A backward-compatibility shim rewrites the legacy "{{ output.X }}" form into
// the function form "{{ output \"X\" }}" before parsing, so configs written
// against the original substring expander keep working unchanged.
package template

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
)

// legacyRe matches the original "{{ output.NAME }}" form (optional whitespace
// and trim markers). NAME may contain letters, digits, dots, dashes, and
// underscores — matching the group-name characters the substring expander
// accepted.
var legacyRe = regexp.MustCompile(`{{-?\s*output\.([A-Za-z0-9._-]+)\s*-?}}`)

// normalize rewrites the legacy dotted form into the function-call form so a
// single Go-template grammar handles both old and new configs.
func normalize(s string) string {
	return legacyRe.ReplaceAllString(s, `{{ output "$1" }}`)
}

// Data is the context a template is rendered against.
type Data struct {
	Outputs map[string]string // group name → captured stdout
	Env     map[string]string // merged keepup environment
}

// Expander renders a templated string against Data. Implementations must be
// safe for concurrent use.
type Expander interface {
	Expand(s string, data Data) (string, error)
}

// goExpander renders with text/template + sprig.
type goExpander struct{}

// NewExpander returns the default Go-template + sprig Expander.
func NewExpander() Expander { return goExpander{} }

func (goExpander) Expand(s string, data Data) (string, error) {
	tmpl, err := template.New("param").
		Option("missingkey=zero").
		Funcs(funcMap(data)).
		Parse(normalize(s))
	if err != nil {
		return "", fmt.Errorf("parse template %q: %w", s, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, nil); err != nil {
		return "", fmt.Errorf("render template %q: %w", s, err)
	}
	return buf.String(), nil
}

// funcMap builds the function map for one render: sprig plus the keepup
// output/env helpers, which close over the supplied data. Building it per
// render keeps the Expander free of shared mutable state (concurrency-safe).
func funcMap(data Data) template.FuncMap {
	fm := sprig.TxtFuncMap()
	// output trims surrounding whitespace, matching the original substring
	// expander so existing configs render identically.
	fm["output"] = func(name string) string { return strings.TrimSpace(data.Outputs[name]) }
	fm["env"] = func(key string) string { return data.Env[key] }
	return fm
}
