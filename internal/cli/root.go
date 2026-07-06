package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"

	"github.com/mikefarah/yq/v4/pkg/yqlib"
	"github.com/spf13/cobra"
)

// gqVersion is stamped at release time via
// -ldflags "-X github.com/javirub/gq/internal/cli.gqVersion=v..."
var gqVersion = "dev"

// exitCoder lets typed errors (e.g. ambiguity errors in later phases)
// control the process exit code. Default for any other error is 1.
type exitCoder interface {
	ExitCode() int
}

// Execute runs the gq CLI and returns the process exit code.
func Execute() int {
	rootCmd := New()

	// same trick as yq's main: when the first argument is an expression or
	// file rather than a subcommand, run the default eval command.
	args := os.Args[1:]
	if _, _, err := rootCmd.Find(args); err != nil && len(args) > 0 && args[0] != "__complete" && args[0] != "__completeNoDesc" {
		rootCmd.SetArgs(append([]string{"eval"}, args...))
	}

	if err := rootCmd.Execute(); err != nil {
		var coder exitCoder
		if errors.As(err, &coder) {
			return coder.ExitCode()
		}
		return 1
	}
	return 0
}

func New() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "gq",
		Short: "gq is a yq-compatible data file processor that preserves Go template syntax.",
		Long: `gq is a command-line YAML/JSON processor with yq-compatible expressions
that can also query and edit Go-templated files (.yaml.gotmpl, .json.gotmpl),
preserving the {{ ... }} template expressions intact.`,
		Example: `
# read the "stuff" node from "myfile.yml"
gq '.stuff' myfile.yml

# update myfile.yml in place
gq -i '.stuff = "foo"' myfile.yml

# set a value to a Go template expression, keeping the file a valid template
gq -i '.name = {{ .Values.name }}' values.yaml.gotmpl

# print contents of sample.json as idiomatic YAML
gq -P -oy sample.json
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if version {
				cmd.Print(versionDisplay())
				return nil
			}
			return evaluateSequence(cmd, args)
		},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SetOut(cmd.OutOrStdout())

			// https://no-color.org
			forceNoColor = forceNoColor || os.Getenv("NO_COLOR") != ""

			level := slog.LevelWarn
			if verbose {
				level = slog.LevelDebug
			}
			yqlib.GetLogger().SetLevel(level)
			opts := &slog.HandlerOptions{Level: level, AddSource: verbose}
			yqlib.GetLogger().SetSlogger(slog.New(slog.NewTextHandler(os.Stderr, opts)))

			yqlib.InitExpressionParser()
			return nil
		},
	}

	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose mode")

	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output-format", "o", "auto", "[auto|a|yaml|y|json|j] output format type.")
	rootCmd.PersistentFlags().StringVarP(&inputFormat, "input-format", "p", "auto", "[auto|a|yaml|y|json|j] parse format for input.")

	rootCmd.PersistentFlags().BoolVarP(&nullInput, "null-input", "n", false, "Don't read input, simply evaluate the expression given. Useful for creating docs from scratch.")
	rootCmd.PersistentFlags().BoolVarP(&noDocSeparators, "no-doc", "N", false, "Don't print document separators (---)")

	rootCmd.PersistentFlags().IntVarP(&indent, "indent", "I", 2, "sets indent level for output")
	rootCmd.Flags().BoolVarP(&version, "version", "V", false, "Print version information and quit")
	rootCmd.PersistentFlags().BoolVarP(&writeInplace, "inplace", "i", false, "update the file in place of first file given.")

	rootCmd.PersistentFlags().BoolVarP(&unwrapScalarFlag, "unwrapScalar", "r", true, "unwrap scalar, print the value with no quotes, colors or comments. Defaults to true for yaml")
	rootCmd.PersistentFlags().Lookup("unwrapScalar").NoOptDefVal = "true"

	rootCmd.PersistentFlags().BoolVarP(&prettyPrint, "prettyPrint", "P", false, "pretty print, shorthand for '... style = \"\"'")
	rootCmd.PersistentFlags().BoolVarP(&exitStatus, "exit-status", "e", false, "set exit status if there are no matches or null or false is returned")

	rootCmd.PersistentFlags().BoolVarP(&forceColor, "colors", "C", false, "force print with colors")
	rootCmd.PersistentFlags().BoolVarP(&forceNoColor, "no-colors", "M", false, "force print with no colors")

	// yq uses -f for --front-matter; gq deliberately reserves -f for
	// --values-file (helm convention, added in a later phase), so
	// front-matter is long-form only.
	rootCmd.PersistentFlags().StringVar(&frontMatter, "front-matter", "", "(extract|process) first input as yaml front-matter. Extract will pull out the yaml content, process will run the expression against the yaml content, leaving the remaining data intact")

	rootCmd.PersistentFlags().StringVar(&forceExpression, "expression", "", "forcibly set the expression argument. Useful when gq argument detection thinks your expression is a file.")
	rootCmd.PersistentFlags().StringVar(&expressionFile, "from-file", "", "Load expression from specified file.")

	rootCmd.PersistentFlags().BoolVar(&allBranches, "all", false, "operate on every branch of {{ if }}/{{ else }} template blocks")
	rootCmd.PersistentFlags().StringArrayVar(&valuesExprs, "values", nil, "yq assignment applied to the data context used to resolve {{ if }} conditions, e.g. --values '.enabled = true' (repeatable)")
	// deliberate divergence from yq, where -f is --front-matter: gq follows
	// the helm/helmfile convention for values files
	rootCmd.PersistentFlags().StringArrayVarP(&valuesFiles, "values-file", "f", nil, "yaml/json file loaded into the data context (helm-style, repeatable, deep-merged in order)")
	rootCmd.PersistentFlags().BoolVar(&forceGotmpl, "gotmpl", false, "force Go-template mode regardless of file extension")
	rootCmd.PersistentFlags().BoolVar(&noGotmpl, "no-gotmpl", false, "disable Go-template mode regardless of file extension")
	rootCmd.MarkFlagsMutuallyExclusive("gotmpl", "no-gotmpl")

	rootCmd.AddCommand(
		createEvaluateSequenceCommand(),
		createEvaluateAllCommand(),
		createRenderCommand(),
	)
	return rootCmd
}

func versionDisplay() string {
	return fmt.Sprintf("gq (https://github.com/javirub/gq) version %s\nyq expression engine (yqlib) %s\n", gqVersion, yqlibVersion())
}

func yqlibVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == "github.com/mikefarah/yq/v4" {
				return dep.Version
			}
		}
	}
	return "unknown"
}
