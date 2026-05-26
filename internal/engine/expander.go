package engine

import (
	"fmt"
	"strings"
)

// Expander substitutes "{{ output.<name> }}" placeholders in templated strings.
type Expander interface {
	Expand(template string, outputs map[string]string) string
}

// TemplateExpander is the default placeholder substitution.
type TemplateExpander struct{}

// Expand replaces all "{{ output.<key> }}" occurrences in template with the
// trimmed value for that key, ignoring placeholders without a matching entry.
func (TemplateExpander) Expand(template string, outputs map[string]string) string {
	for k, v := range outputs {
		placeholder := fmt.Sprintf("{{ output.%s }}", k)
		template = strings.ReplaceAll(template, placeholder, strings.TrimSpace(v))
	}
	return template
}
