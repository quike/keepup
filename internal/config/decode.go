package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"

	"go.yaml.in/yaml/v4"
)

// decodeStrict decodes the first YAML document in data into out, rejecting
// any mapping key that has no matching struct field. The resulting error
// names the key, its line and its place in the schema (e.g. `line 9: unknown
// key "skipif" in group`), so a typo fails loudly instead of silently
// disabling a feature. An empty document decodes to the zero value.
//
// KnownFields only governs the reflective struct decoder. The custom
// UnmarshalYAML methods (RunEntry, CommandSpec) walk their mapping keys by
// hand and reject unknown ones themselves; they only call node.Decode for
// scalars, lists and maps, which have no fields to be strict about.
func decodeStrict(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return humanizeDecodeError(err)
	}
	return nil
}

// unknownFieldRE matches the yaml library's unknown-key message, e.g.
// "field skipif not found in type config.Group".
var unknownFieldRE = regexp.MustCompile(`^field (.+) not found in type (\S+)$`)

// humanizeDecodeError rewrites unknown-key entries of a *yaml.LoadErrors so
// they name the key and its place in the schema instead of a Go type:
// `line 9: unknown key "skipif" in group`. Every other entry, and every other
// error, passes through unchanged.
func humanizeDecodeError(err error) error {
	var le *yaml.LoadErrors
	if !errors.As(err, &le) {
		return err
	}
	out := make([]*yaml.LoadError, len(le.Errors))
	for i, e := range le.Errors {
		out[i] = humanizeUnknownField(e)
	}
	return &yaml.LoadErrors{Errors: out}
}

func humanizeUnknownField(e *yaml.LoadError) *yaml.LoadError {
	m := unknownFieldRE.FindStringSubmatch(e.Message)
	if m == nil {
		return e
	}
	key, typeName := m[1], m[2]
	for t, where := range schemaLocations {
		if t.String() == typeName {
			out := *e
			out.Message = fmt.Sprintf("unknown key %q %s", key, where)
			return &out
		}
	}
	return e
}

// checkDeclaredVersion leniently reads only the top-level version: and
// reports a schema-version error when it is set to anything but
// SchemaVersion. NewConfig consults it when strict decoding fails, so a v1
// file is told to migrate rather than shown every v1-only key as unknown.
// It returns nil when the version is current, absent, or unreadable.
func checkDeclaredVersion(data []byte) error {
	var probe struct {
		Version int `yaml:"version"`
	}
	// Unreadable YAML leaves Version at 0: the strict decode error is then
	// the better report.
	_ = yaml.Unmarshal(data, &probe)
	if probe.Version == 0 || probe.Version == SchemaVersion {
		return nil
	}
	return unsupportedVersionError(probe.Version)
}

func unsupportedVersionError(v int) error {
	return fmt.Errorf(
		"unsupported schema version %d: this binary only supports version %d",
		v, SchemaVersion,
	)
}
