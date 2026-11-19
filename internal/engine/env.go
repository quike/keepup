package engine

import (
	"maps"
	"strings"
)

// overlay returns the process environment with the declared env on top:
// declared values beat process values. The precedence among the config's env:
// layers is config.DeclaredEnv's job; this only adds the process layer below
// it. Neither input is mutated.
func overlay(process, declared map[string]string) map[string]string {
	merged := make(map[string]string, len(process)+len(declared))
	maps.Copy(merged, process)
	maps.Copy(merged, declared)
	return merged
}

// parseEnviron converts KEY=VALUE pairs (the os.Environ form) into a map.
// Entries without "=" are dropped.
func parseEnviron(environ []string) map[string]string {
	env := make(map[string]string, len(environ))
	for _, raw := range environ {
		if k, v, ok := strings.Cut(raw, "="); ok {
			env[k] = v
		}
	}
	return env
}

// formatEnviron flattens env into KEY=VALUE pairs suitable for exec.Cmd.Env.
func formatEnviron(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}
