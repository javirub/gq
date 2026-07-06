package cli

import (
	"fmt"
	"os"

	"github.com/javirub/gq/internal/blocks"
	"github.com/javirub/gq/internal/render"
	"github.com/javirub/gq/internal/values"
	"github.com/spf13/cobra"
)

// renderError maps template-data problems (missing keys) to exit code 2,
// like the resolve errors of the eval commands.
type renderError struct {
	file   string
	detail string
}

func (e *renderError) Error() string {
	return fmt.Sprintf(`cannot render %s: %s
  hint: add --values '<key> = <value>' or -f <values-file>`, e.file, e.detail)
}

func (e *renderError) ExitCode() int { return 2 }

func createRenderCommand() *cobra.Command {
	var outputFile string

	cmd := &cobra.Command{
		Use:   "render [file]",
		Short: "Execute the Go template with the given values and print plain YAML/JSON",
		Long: `Fully executes the Go template in the given file (or stdin) using the data
context built from --values and -f/--values-file, producing plain YAML/JSON.
Template functions are text/template built-ins plus sprig, and missing keys
are errors (exit code 2).`,
		Example: `
# render with the environment values file
gq render -f env/prod.yaml values.yaml.gotmpl

# render to a file with inline values
gq render --values '.Values.enabled = true' --output-file out.yaml values.yaml.gotmpl`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			name := "-"
			if len(args) > 0 {
				name = args[0]
			}
			content, err := readInput(name)
			if err != nil {
				return err
			}

			data, err := values.Build(valuesFiles, valuesExprs)
			if err != nil {
				return err
			}

			rendered, err := render.Render(string(content), data)
			if err != nil {
				return &renderError{file: name, detail: blocks.DescribeEvalError(err)}
			}

			if outputFile != "" {
				return os.WriteFile(outputFile, []byte(rendered), 0o600)
			}
			_, err = cmd.OutOrStdout().Write([]byte(rendered))
			return err
		},
	}

	// long-form only: -o keeps yq's output-format meaning on the root
	cmd.Flags().StringVar(&outputFile, "output-file", "", "write the rendered output to this file instead of stdout")
	return cmd
}
