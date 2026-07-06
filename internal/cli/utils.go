package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mikefarah/yq/v4/pkg/yqlib"
	"github.com/spf13/cobra"
)

// initCommand mirrors yq's argument and format handling so gq behaves
// identically for the supported subset (yaml/json).
func initCommand(cmd *cobra.Command, args []string) (string, []string, error) {
	cmd.SilenceUsage = true

	// derived state is recomputed on every run so the command can be
	// executed multiple times in-process (e.g. from tests)
	colorsEnabled = false
	unwrapScalar = false
	completedSuccessfully = false

	setupColors()

	expression, args, err := processArgs(args)
	if err != nil {
		return "", nil, err
	}

	if err := validateCommandFlags(args); err != nil {
		return "", nil, err
	}

	if err := configureFormats(args); err != nil {
		return "", nil, err
	}

	configureUnwrapScalar(cmd)

	return expression, args, nil
}

func setupColors() {
	fileInfo, _ := os.Stdout.Stat()

	if forceColor || (!forceNoColor && fileInfo != nil && (fileInfo.Mode()&os.ModeCharDevice) != 0) {
		colorsEnabled = true
	}
}

func validateCommandFlags(args []string) error {
	if writeInplace && (len(args) == 0 || args[0] == "-") {
		return fmt.Errorf("write in place flag only applicable when giving an expression and at least one file")
	}

	if frontMatter != "" && len(args) == 0 {
		return fmt.Errorf("front matter flag only applicable when giving an expression and at least one file")
	}

	if nullInput && len(args) > 0 {
		return fmt.Errorf("cannot pass files in when using null-input flag")
	}

	return nil
}

// normalizeFormat maps the yq format aliases gq supports to their formal
// name, and rejects the yq formats gq does not support.
func normalizeFormat(format string) (string, error) {
	switch format {
	case "", "auto", "a":
		return "auto", nil
	case "yaml", "y", "yml":
		return "yaml", nil
	case "json", "j":
		return "json", nil
	default:
		return "", fmt.Errorf("format '%s' is not supported by gq (supported formats: yaml, json)", format)
	}
}

func isAutomaticOutputFormat() bool {
	return outputFormat == "" || outputFormat == "auto" || outputFormat == "a"
}

// stripGotmpl removes a trailing .gotmpl so format detection sees the
// underlying data format (values.yaml.gotmpl -> values.yaml).
func stripGotmpl(filename string) string {
	return strings.TrimSuffix(filename, ".gotmpl")
}

func isTemplateFilename(filename string) bool {
	return strings.HasSuffix(filename, ".gotmpl")
}

func configureFormats(args []string) error {
	inputFilename := ""
	if len(args) > 0 {
		inputFilename = args[0]
	}

	if err := configureInputFormat(inputFilename); err != nil {
		return err
	}

	normalizedOut, err := normalizeFormat(outputFormat)
	if err != nil {
		return err
	}
	if normalizedOut == "auto" {
		// only reachable with no input filename and explicit -p: same
		// backwards-compatible default as yq.
		normalizedOut = "yaml"
	}
	outputFormat = normalizedOut

	if outputFormat == "yaml" {
		unwrapScalar = true
	}

	yqlib.GetLogger().Debugf("Using input format %v", inputFormat)
	yqlib.GetLogger().Debugf("Using output format %v", outputFormat)

	return nil
}

func configureInputFormat(inputFilename string) error {
	normalizedIn, err := normalizeFormat(inputFormat)
	if err != nil {
		return err
	}

	if normalizedIn == "auto" {
		detected := yqlib.FormatStringFromFilename(stripGotmpl(inputFilename))
		normalizedDetected, err := normalizeFormat(detected)
		if err != nil {
			// the extension maps to a format yq knows but gq does not
			// support (e.g. .xml): fail loudly rather than misparse.
			if _, yqErr := yqlib.FormatFromString(detected); yqErr == nil {
				return fmt.Errorf("input file looks like '%s' which is not supported by gq (supported formats: yaml, json)", detected)
			}
			// unknown extension: default to yaml, like yq
			yqlib.GetLogger().Debugf("Unknown file format extension '%v', defaulting to yaml", detected)
			normalizedDetected = "yaml"
		}
		inputFormat = normalizedDetected
		if isAutomaticOutputFormat() {
			outputFormat = inputFormat
		}
	} else {
		inputFormat = normalizedIn
		if isAutomaticOutputFormat() {
			// same backwards-compatible behavior as yq: explicit -p with
			// automatic output produces yaml.
			outputFormat = "yaml"
		}
	}
	return nil
}

func configureUnwrapScalar(cmd *cobra.Command) {
	if cmd.Flags().Changed("unwrapScalar") {
		unwrapScalar = unwrapScalarFlag
	}
}

func configureDecoder(evaluateTogether bool) (yqlib.Decoder, error) {
	format, err := yqlib.FormatFromString(inputFormat)
	if err != nil {
		return nil, err
	}
	yqlib.ConfiguredYamlPreferences.EvaluateTogether = evaluateTogether

	if format.DecoderFactory == nil {
		return nil, fmt.Errorf("no support for %s input format", inputFormat)
	}
	decoder := format.DecoderFactory()
	if decoder == nil {
		return nil, fmt.Errorf("no support for %s input format", inputFormat)
	}
	return decoder, nil
}

func configureEncoder() (yqlib.Encoder, error) {
	format, err := yqlib.FormatFromString(outputFormat)
	if err != nil {
		return nil, err
	}

	yqlib.ConfiguredYamlPreferences.Indent = indent
	yqlib.ConfiguredJSONPreferences.Indent = indent

	yqlib.ConfiguredYamlPreferences.UnwrapScalar = unwrapScalar
	yqlib.ConfiguredJSONPreferences.UnwrapScalar = unwrapScalar

	yqlib.ConfiguredYamlPreferences.ColorsEnabled = colorsEnabled
	yqlib.ConfiguredJSONPreferences.ColorsEnabled = colorsEnabled

	yqlib.ConfiguredYamlPreferences.PrintDocSeparators = !noDocSeparators

	encoder := format.EncoderFactory()
	if encoder == nil {
		return nil, fmt.Errorf("no support for %s output format", outputFormat)
	}
	return encoder, nil
}

func configurePrinterWriter(out io.Writer) yqlib.PrinterWriter {
	return yqlib.NewSinglePrinterWriter(out)
}

// maybeFile mirrors yq's heuristic for detecting whether an argument is a
// file rather than an expression.
func maybeFile(str string) bool {
	stat, err := os.Stat(str) // #nosec
	return err == nil && !stat.IsDir()
}

func processStdInArgs(args []string) []string {
	stat, err := os.Stdin.Stat()
	if err != nil {
		yqlib.GetLogger().Debugf("error getting stdin: %v", err)
	}
	pipingStdin := stat != nil && (stat.Mode()&os.ModeCharDevice) == 0

	// if we've been given a file, don't automatically read from stdin
	if nullInput || !pipingStdin || len(args) > 1 || (len(args) > 0 && maybeFile(args[0])) {
		return args
	}

	for _, arg := range args {
		if arg == "-" {
			return args
		}
	}

	// we're piping from stdin, but there's no '-' arg: add one to the end
	return append(args, "-")
}

func processArgs(originalArgs []string) (string, []string, error) {
	expression := forceExpression
	args := processStdInArgs(originalArgs)
	maybeFirstArgIsAFile := len(args) > 0 && maybeFile(args[0])

	if expressionFile == "" && maybeFirstArgIsAFile && strings.HasSuffix(args[0], ".yq") {
		yqlib.GetLogger().Debugf("Assuming arg %v is an expression file", args[0])
		expressionFile = args[0]
		args = args[1:]
	}

	if expressionFile != "" {
		expressionBytes, err := os.ReadFile(expressionFile)
		if err != nil {
			return "", nil, err
		}
		// replace \r\n (windows) with unix line endings
		expression = strings.ReplaceAll(string(expressionBytes), "\r\n", "\n")
	}

	if expression == "" && len(args) > 0 && args[0] != "-" && !maybeFile(args[0]) {
		yqlib.GetLogger().Debugf("assuming expression is '%v'", args[0])
		expression = args[0]
		args = args[1:]
	}
	return expression, args, nil
}

// processExpression applies -P exactly like yq does.
func processExpression(expression string) string {
	if prettyPrint && expression == "" {
		return yqlib.PrettyPrintExp
	} else if prettyPrint {
		return fmt.Sprintf("%v | %v", expression, yqlib.PrettyPrintExp)
	}
	return expression
}
