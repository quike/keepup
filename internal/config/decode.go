package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"go.yaml.in/yaml/v3"
)

// decodeStrict decodes the first YAML document in data into out, rejecting
// any mapping key that has no matching struct field. The resulting error
// names the key and its line (e.g. "line 9: field skipif not found in type
// config.Group"), so a typo fails loudly instead of silently disabling a
// feature. An empty document decodes to the zero value.
//
// KnownFields only governs the reflective struct decoder. The custom
// UnmarshalYAML methods (RunEntry, CommandSpec) walk their mapping keys by
// hand and reject unknown ones themselves; they only call node.Decode for
// scalars, lists and maps, which have no fields to be strict about.
func decodeStrict(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
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
