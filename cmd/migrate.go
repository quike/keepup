package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/quike/keepup/internal/migrate"
)

func newMigrateCmd(stdout io.Writer) *cobra.Command {
	var (
		outPath  string
		flowName string
	)
	cmd := &cobra.Command{
		Use:   "migrate <path>",
		Short: "Convert a legacy v1 config file to the v2 schema",
		Long: "Read a v1 keepup file and emit the v2 equivalent. Groups, env, and " +
			"settings are preserved; the single v1 'execution' block becomes one " +
			"step-mode flow. Output goes to stdout unless --output is given.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			data, err := os.ReadFile(filepath.Clean(args[0]))
			if err != nil {
				return fmt.Errorf("read %q: %w", args[0], err)
			}
			out, err := migrate.Migrate(data, migrate.Options{FlowName: flowName})
			if err != nil {
				return err
			}
			if outPath == "" {
				_, err = stdout.Write(out)
				return err
			}
			// outPath is a user-chosen destination for a CLI write; that is the
			// command's purpose.
			if err := os.WriteFile(filepath.Clean(outPath), out, 0o600); err != nil { //nolint:gosec // user-specified output path
				return fmt.Errorf("write %q: %w", outPath, err)
			}
			fmt.Fprintf(stdout, "wrote %s\n", outPath)
			return nil
		},
	}
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "Write the v2 config to this file instead of stdout")
	cmd.Flags().StringVar(&flowName, "flow", migrate.DefaultFlowName, "Name for the flow synthesized from the v1 execution block")
	return cmd
}
