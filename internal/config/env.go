package config

import "maps"

// DeclaredEnv returns the environment a command declares across the config's
// three env: layers, with command env: beating group env: beating global env:.
// The process environment is not included. This is the single definition of
// env precedence: the runner layers it over the process environment, and the
// cache fingerprints it.
func DeclaredEnv(global, group, command map[string]string) map[string]string {
	merged := make(map[string]string, len(global)+len(group)+len(command))
	for _, layer := range []map[string]string{global, group, command} {
		maps.Copy(merged, layer)
	}
	return merged
}
