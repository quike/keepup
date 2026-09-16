package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/quike/keepup/internal/config"
)

// completeFlows builds the completion function for a flow-name argument. It
// must not use runtimeOpts.load, which mutates CLI state and under --verbose
// dumps the config to stdout — into the candidate list.
func completeFlows(opts *runtimeOpts) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		cfg := configForCompletion(opts)
		if cfg == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var candidates []string
		for _, name := range sortedFlowNames(cfg) {
			if strings.HasPrefix(name, toComplete) {
				candidates = append(candidates, name+flowCompletionSuffix(cfg, name))
			}
		}
		return candidates, cobra.ShellCompDirectiveNoFileComp
	}
}

// configForCompletion loads the config the pending command would use: --config
// when already on the line, otherwise the default path. It returns nil on any
// failure, since completion has no channel to report an error on.
func configForCompletion(opts *runtimeOpts) *config.Config {
	path := opts.configFile
	if path == "" {
		resolved, err := defaultConfigPath()
		if err != nil {
			return nil
		}
		path = resolved
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		return nil
	}
	return cfg
}

// flowCompletionSuffix renders the tab-separated description that zsh and fish
// show beside a candidate, marking the default flow.
func flowCompletionSuffix(cfg *config.Config, name string) string {
	desc := cfg.Flows[name].Description
	if cfg.Default == name {
		desc = strings.TrimSpace(defaultFlowMarker + " " + desc)
	}
	if desc == "" {
		return ""
	}
	return "\t" + desc
}
