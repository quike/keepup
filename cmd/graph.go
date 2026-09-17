package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/quike/keepup/internal/graph"
)

func newGraphCmd(opts *runtimeOpts, stdout io.Writer) *cobra.Command {
	var format, output string
	cmd := &cobra.Command{
		Use:   "graph [flow]",
		Short: "Emit a diagram of a flow",
		Long: "Render a flow as a Mermaid or Graphviz diagram. Step-mode waves are drawn " +
			"as labeled boxes, conditional groups dashed, and cacheable groups as cylinders.\n\n" +
			"Pipe dot through Graphviz for an image:\n" +
			"  keepup graph ci --format dot | dot -Tsvg > ci.svg",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeFlows(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.load(cmd.OutOrStdout()); err != nil {
				return err
			}
			flowName := opts.cfg.Default
			if len(args) == 1 {
				flowName = args[0]
			}
			if flowName == "" {
				return fmt.Errorf("no flow specified and no default declared")
			}
			model, err := graph.Build(opts.cfg, flowName)
			if err != nil {
				return err
			}
			w, closeFn, err := openGraphWriter(output, stdout)
			if err != nil {
				return err
			}
			defer closeFn()
			return graph.Render(w, graph.Format(format), model)
		},
	}
	cmd.Flags().StringVarP(&format, "format", "f", string(graph.FormatMermaid),
		"Diagram format: "+strings.Join(graph.Formats(), " or "))
	cmd.Flags().StringVarP(&output, "output", "o", "",
		"Write the diagram to this file ('-' or empty for stdout)")
	_ = cmd.RegisterFlagCompletionFunc("format",
		func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return graph.Formats(), cobra.ShellCompDirectiveNoFileComp
		})
	return cmd
}

// openGraphWriter resolves --output: empty or "-" means stdout, otherwise a
// file (truncated). The returned closer is a no-op for stdout.
func openGraphWriter(path string, stdout io.Writer) (io.Writer, func(), error) {
	if path == "" || path == "-" {
		return stdout, func() {}, nil
	}
	f, err := os.Create(filepath.Clean(path))
	if err != nil {
		return nil, nil, fmt.Errorf("open graph file %q: %w", path, err)
	}
	return f, func() { _ = f.Close() }, nil
}
