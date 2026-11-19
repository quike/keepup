package config

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// docsSkipMarker, on the line directly above a ```yaml fence, opts that block
// out of TestDocsYAMLExamples. Use it only for schema sketches with
// placeholders (`{ ... }`, `[...]`) or snippets that are not one document.
const docsSkipMarker = "<!-- docs-example: skip -->"

type docExample struct {
	where string
	body  []byte
}

// TestDocsYAMLExamples strictly decodes every YAML example in the user docs so
// a renamed or misspelled key in the documentation fails CI. Complete
// documents (with version:) go through NewConfig; fragments are decoded into
// the schema type their shape implies.
func TestDocsYAMLExamples(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("../../docs/*.md")
	require.NoError(t, err)
	files = append(files, "../../README.md")

	var examples []docExample
	for _, f := range files {
		examples = append(examples, extractYAMLExamples(t, f)...)
	}
	require.NotEmpty(t, examples, "no YAML examples found; did the docs move?")

	for _, ex := range examples {
		t.Run(ex.where, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, decodeDocExample(ex.body))
		})
	}
}

// decodeDocExample picks the target type from the fragment's shape: a list is
// a groups: list, a bare commands: block belongs to a group, a document with
// version: is a full config, and any other mapping is a top-level excerpt.
func decodeDocExample(body []byte) error {
	var root yaml.Node
	if err := yaml.Unmarshal(body, &root); err != nil {
		return err
	}
	if len(root.Content) == 0 {
		return nil
	}
	top := root.Content[0]
	switch {
	case top.Kind == yaml.SequenceNode:
		var groups []Group
		return decodeStrict(body, &groups)
	case hasKey(top, "version"):
		_, err := NewConfig(body)
		return err
	case hasKey(top, "commands"):
		var g Group
		return decodeStrict(body, &g)
	default:
		var c Config
		return decodeStrict(body, &c)
	}
}

func hasKey(n *yaml.Node, key string) bool {
	if n.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return true
		}
	}
	return false
}

func extractYAMLExamples(t *testing.T, path string) []docExample {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)

	var (
		out      []docExample
		body     bytes.Buffer
		inBlock  bool
		skip     bool
		start    int
		prevLine string
	)
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		switch {
		case !inBlock && (trimmed == "```yaml" || trimmed == "```yml"):
			inBlock, start, skip = true, n, strings.TrimSpace(prevLine) == docsSkipMarker
			body.Reset()
		case inBlock && trimmed == "```":
			inBlock = false
			if !skip {
				where := filepath.Base(path) + ":" + strconv.Itoa(start)
				out = append(out, docExample{where: where, body: bytes.Clone(body.Bytes())})
			}
		case inBlock:
			body.WriteString(line)
			body.WriteByte('\n')
		}
		prevLine = line
	}
	require.NoError(t, sc.Err())
	return out
}
