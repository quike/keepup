package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/quike/keepup/internal/config"
)

// starterConfig is the v2 scaffold written by `keepup init`. It is validated
// against the parser in a test, so it always loads.
const starterConfig = `version: 2

settings:
  logging:
    level: info
    pretty: true

groups:
  - name: hello
    description: "Print a greeting"
    command: echo
    params: ["hello from keepup"]

  - name: world
    description: "Build on the previous group's output"
    command: echo
    params: ['{{ output "hello" }} — world']

default: greet

flows:
  greet:
    description: "Say hello, then consume its output"
    mode: step
    steps:
      - run: [hello]
      - run: [world]
`

func newInitCmd(stdout io.Writer) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Write a starter keepup.yml to get going quickly",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			path := "keepup.yml"
			if len(args) == 1 {
				path = args[0]
			}
			if _, err := os.Stat(path); err == nil && !force {
				return fmt.Errorf("%s already exists; pass --force to overwrite", path)
			}
			if dir := filepath.Dir(path); dir != "." && dir != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return fmt.Errorf("create %s: %w", dir, err)
				}
			}
			if err := os.WriteFile(filepath.Clean(path), []byte(starterConfig), 0o600); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
			fmt.Fprintf(stdout, "wrote %s — run `keepup run` to try it\n", path)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite an existing file")
	return cmd
}

// starterIsValid is a guard used by tests to ensure the scaffold always parses.
func starterIsValid() error {
	_, err := config.NewConfig([]byte(starterConfig))
	return err
}
