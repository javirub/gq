package cli

import (
	"bufio"
	"bytes"
	"container/list"
	"fmt"
	"io"
	"os"

	"github.com/javirub/gq/internal/codec"
	"github.com/mikefarah/yq/v4/pkg/yqlib"
	"github.com/spf13/cobra"
)

func createEvaluateSequenceCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "eval [expression] [file...]",
		Aliases: []string{"e"},
		Short:   "(default) Apply the expression to each document in each file in sequence",
		Long: `Iterates over each document from each given file, applies the
expression and prints the result in sequence.`,
		Example: `
# Reads field under the given path for each file
gq e '.a.b' f1.yml f2.yml

# Creates a new yaml document
gq e -n '.a.b.c = "cat"'

# Update a file in place
gq e '.a.b = "cool"' -i file.yaml
`,
		RunE: evaluateSequence,
	}
}

func createEvaluateAllCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "eval-all [expression] [file...]",
		Aliases: []string{"ea"},
		Short:   "Loads all documents of all files and runs the expression once",
		Long: `Loads all documents of all given files at once, then applies the
expression once against the combined list of documents.`,
		Example: `
# Merge f2.yml into f1.yml (inplace)
gq ea -i 'select(fileIndex == 0) * select(fileIndex == 1)' f1.yml f2.yml
`,
		RunE: evaluateAll,
	}
}

func evaluateSequence(cmd *cobra.Command, args []string) error {
	return runEvaluate(cmd, args, false)
}

func evaluateAll(cmd *cobra.Command, args []string) error {
	return runEvaluate(cmd, args, true)
}

func runEvaluate(cmd *cobra.Command, args []string, allAtOnce bool) (cmdError error) {
	out := cmd.OutOrStdout()

	expression, args, err := initCommand(cmd, args)
	if err != nil {
		return err
	}

	// {{ ... }} in the expression itself becomes a quoted string literal
	// and is restored in the output
	rawExpression := expression
	exprDoc, expression, err := codec.EncodeExpression(expression)
	if err != nil {
		return err
	}

	if writeInplace {
		// only use colors if forced
		colorsEnabled = forceColor
		writeInPlaceHandler := yqlib.NewWriteInPlaceHandler(args[0])
		out, err = writeInPlaceHandler.CreateTempFile()
		if err != nil {
			return err
		}
		// indirection so that completedSuccessfully is read at the end of
		// execution rather than now
		defer func() {
			if cmdError == nil {
				cmdError = writeInPlaceHandler.FinishWriteInPlace(completedSuccessfully)
			}
		}()
	}

	encoder, err := configureEncoder()
	if err != nil {
		return err
	}
	decoder, err := configureDecoder(allAtOnce)
	if err != nil {
		return err
	}

	sources := make([]inputSource, len(args))
	for i, arg := range args {
		sources[i] = inputSource{name: arg}
	}

	var appendix []byte
	if frontMatter != "" {
		// gq splits front matter in memory instead of using yqlib's
		// frontMatterHandler, which leaks the original file handle and
		// needs temp files
		content, err := readInput(args[0])
		if err != nil {
			return err
		}
		var frontMatterContent []byte
		frontMatterContent, appendix = splitFrontMatter(content)
		sources[0].content = frontMatterContent
	}

	// encode templated sources into plain-parseable projections
	restoreDocs := []*codec.Doc{exprDoc}
	var blockFile *codec.File
	for i := range sources {
		content, isTemplate, err := sources[i].detectTemplate()
		if err != nil {
			return err
		}
		if content != nil {
			// keep content we already consumed (e.g. piped stdin)
			sources[i].content = content
		}
		if !isTemplate {
			continue
		}
		inputCodecFormat := codec.Yaml
		if inputFormat == "json" {
			inputCodecFormat = codec.Json
		}
		parsed, err := codec.Parse(string(content), inputCodecFormat)
		if err != nil {
			return fmt.Errorf("%s: %w", sources[i].name, err)
		}
		if parsed.HasBlocks() {
			if blockFile != nil {
				return fmt.Errorf("only one file with control-flow template blocks can be processed per invocation")
			}
			blockFile = parsed
			continue
		}
		doc, err := parsed.Project(nil)
		if err != nil {
			return fmt.Errorf("%s: %w", sources[i].name, err)
		}
		sources[i].content = []byte(doc.Projection())
		restoreDocs = append(restoreDocs, doc)
	}

	// files with control-flow blocks take a dedicated path: base projection
	// plus per-branch probes / --all enumeration
	if blockFile != nil {
		if len(sources) != 1 {
			return fmt.Errorf("a file with control-flow template blocks cannot be combined with other input files")
		}
		if allAtOnce {
			return fmt.Errorf("eval-all does not support files with control-flow template blocks")
		}
		err = evaluateBlockFile(rawExpression, processExpression(expression), blockFile, sources[0].name, exprDoc, out, encoder, decoder, appendix)
		completedSuccessfully = err == nil
		return err
	}

	// when templates are involved, evaluate into a buffer, restore the
	// placeholders and only then write to the real output
	needsRestore := len(restoreDocs) > 1 || exprDoc.HasTemplates()
	evalOut := out
	var restoreBuffer *bytes.Buffer
	if needsRestore {
		restoreBuffer = new(bytes.Buffer)
		evalOut = restoreBuffer
	}

	printer := yqlib.NewPrinter(encoder, configurePrinterWriter(evalOut))
	if frontMatter == "process" {
		printer.SetAppendix(bytes.NewReader(appendix))
	}

	switch len(args) {
	case 0:
		if nullInput {
			err = yqlib.NewStreamEvaluator().EvaluateNew(processExpression(expression), printer)
		} else {
			cmd.Println(cmd.UsageString())
			return nil
		}
	default:
		if allAtOnce {
			err = evaluateAllInMemory(processExpression(expression), sources, printer, decoder)
		} else {
			err = evaluateFilesInMemory(processExpression(expression), sources, printer, decoder)
		}
	}

	if err == nil && needsRestore {
		restored := restoreBuffer.String()
		for _, doc := range restoreDocs {
			restored, err = doc.Restore(restored)
			if err != nil {
				break
			}
		}
		if err == nil {
			_, err = io.WriteString(out, restored)
		}
	}
	completedSuccessfully = err == nil

	if err == nil && exitStatus && !printer.PrintedAnything() {
		return fmt.Errorf("no matches found")
	}

	return err
}

// inputSource is a named input with optionally pre-loaded content; when the
// content is nil it is read from the named file (or stdin for "-") at open
// time. yqlib's own EvaluateFiles never closes the files it opens (its close
// only matches *os.File but readStream returns a *bufio.Reader), which locks
// the inputs on Windows, so gq reads files itself instead.
type inputSource struct {
	name    string
	content []byte
}

func (s inputSource) open() (io.Reader, error) {
	if s.content != nil {
		return bytes.NewReader(s.content), nil
	}
	if s.name == "-" {
		return bufio.NewReader(os.Stdin), nil
	}
	content, err := os.ReadFile(s.name) // #nosec
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(content), nil
}

// detectTemplate decides whether this source must go through the template
// codec (--gotmpl / --no-gotmpl override, .gotmpl extension, or {{ in piped
// stdin) and returns the loaded content when it does.
func (s inputSource) detectTemplate() ([]byte, bool, error) {
	if noGotmpl {
		return nil, false, nil
	}
	if !forceGotmpl && !isTemplateFilename(s.name) && s.name != "-" {
		return nil, false, nil
	}
	content := s.content
	if content == nil {
		var err error
		content, err = readInput(s.name)
		if err != nil {
			return nil, false, err
		}
	}
	if s.name == "-" && !forceGotmpl && !bytes.Contains(content, []byte("{{")) {
		// plain stdin: keep the content we already consumed
		return content, false, nil
	}
	return content, true, nil
}

// readInput reads a whole file, or all of stdin for "-".
func readInput(filename string) ([]byte, error) {
	if filename == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(filename) // #nosec
}

// evaluateFilesInMemory mirrors yqlib's StreamEvaluator.EvaluateFiles.
func evaluateFilesInMemory(expression string, sources []inputSource, printer yqlib.Printer, decoder yqlib.Decoder) error {
	node, err := yqlib.ExpressionParser.ParseExpression(expression)
	if err != nil {
		return err
	}

	streamEvaluator := yqlib.NewStreamEvaluator()
	var totalProcessedDocs uint
	for _, source := range sources {
		reader, err := source.open()
		if err != nil {
			return err
		}
		processedDocs, err := streamEvaluator.Evaluate(source.name, reader, node, printer, decoder)
		if err != nil {
			return err
		}
		totalProcessedDocs += processedDocs
	}

	if totalProcessedDocs == 0 {
		return streamEvaluator.EvaluateNew(expression, printer)
	}
	return nil
}

// evaluateAllInMemory mirrors yqlib's AllAtOnceEvaluator.EvaluateFiles.
func evaluateAllInMemory(expression string, sources []inputSource, printer yqlib.Printer, decoder yqlib.Decoder) error {
	allDocuments := list.New()
	for fileIndex, source := range sources {
		reader, err := source.open()
		if err != nil {
			return err
		}
		documents, err := yqlib.ReadDocuments(reader, decoder)
		if err != nil {
			return err
		}
		for el := documents.Front(); el != nil; el = el.Next() {
			candidate := el.Value.(*yqlib.CandidateNode)
			candidate.SetFilename(source.name)
			candidate.SetFileIndex(fileIndex)
		}
		allDocuments.PushBackList(documents)
	}

	if allDocuments.Len() == 0 {
		return yqlib.NewStreamEvaluator().EvaluateNew(expression, printer)
	}

	matches, err := yqlib.NewAllAtOnceEvaluator().EvaluateCandidateNodes(expression, allDocuments)
	if err != nil {
		return err
	}
	return printer.PrintResults(matches)
}
