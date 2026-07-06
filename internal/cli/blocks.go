package cli

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/javirub/gq/internal/blocks"
	"github.com/javirub/gq/internal/codec"
	"github.com/javirub/gq/internal/values"
	"github.com/mikefarah/yq/v4/pkg/yqlib"
)

// resolveError is returned when an expression matches content inside a
// conditional template block that gq cannot resolve to a single branch.
// It maps to exit code 2 so CI pipelines can tell it apart.
type resolveError struct {
	expression string
	file       string
	action     string
	line       int
	detail     string
}

func (e *resolveError) Error() string {
	detail := e.detail
	if detail == "" {
		detail = "no values were provided"
	}
	return fmt.Sprintf(`cannot resolve '%s': it matches content inside a conditional block

    %s:%d   %s

  the condition could not be evaluated: %s
  hint: add --values '<key> = <value>' (or -f <values-file>), or use --all to target every branch`,
		e.expression, e.file, e.line, e.action, detail)
}

func (e *resolveError) ExitCode() int { return 2 }

// evaluateBlockFile evaluates the expression against a single templated
// file containing control-flow blocks. The if-conditions are resolved with
// the --values/-f data context; expressions that touch unresolved branches
// produce a resolveError, and --all evaluates every branch annotating
// results with their branch condition.
func evaluateBlockFile(rawExpression, expression string, f *codec.File, name string, exprDoc *codec.Doc, out io.Writer, encoder yqlib.Encoder, decoder yqlib.Decoder, appendix []byte) error {
	if allBranches {
		return evaluateAllBranches(expression, f, name, exprDoc, out, encoder, decoder, appendix)
	}

	data, err := values.Build(valuesFiles, valuesExprs)
	if err != nil {
		return err
	}
	resolution := blocks.Resolve(f, data)

	baseDoc, err := f.Project(resolution.Selector())
	if err != nil {
		return err
	}
	baseRaw, err := evalDoc(expression, baseDoc, name, encoder, decoder)
	if err != nil {
		return err
	}
	base, err := restoreOutput(baseDoc, exprDoc, baseRaw, writeInplace)
	if err != nil {
		return err
	}

	if !resolution.FullyResolved() {
		if err := probeUnresolved(rawExpression, expression, f, name, exprDoc, encoder, decoder, resolution, base); err != nil {
			return err
		}
	}

	return writeResult(out, base, appendix)
}

// probeUnresolved evaluates the expression once per unresolved branch to
// detect matches (reads) or edits (writes) landing inside content that gq
// could not resolve to a single branch.
func probeUnresolved(rawExpression, expression string, f *codec.File, name string, exprDoc *codec.Doc, encoder yqlib.Encoder, decoder yqlib.Decoder, resolution *blocks.Resolution, base string) error {
	baseNull := isNullish(base)

	for _, unres := range resolution.Unresolved {
		probeDoc, err := f.Project(resolution.ProbeSelector(unres.Ref))
		if err != nil {
			return err
		}
		probeRaw, err := evalDoc(expression, probeDoc, name, encoder, decoder)
		if err != nil {
			// a probe that fails to evaluate must not fail the whole run:
			// the branch content may not even be valid in combination
			continue
		}

		action, line := f.Describe(unres.Ref)
		newResolveError := func() *resolveError {
			return &resolveError{
				expression: rawExpression,
				file:       name,
				action:     action,
				line:       line,
				detail:     unres.Detail,
			}
		}

		// write-probe: did the branch body region change? compare against
		// an identity evaluation of the same projection so that yq's
		// cosmetic normalization does not count as a change
		if open, closing, ok := probeDoc.BranchRegionMarkers(unres.Ref); ok {
			if editedRegion, found := extractRegion(probeRaw, open, closing); found {
				identityRaw, err := evalDoc(".", probeDoc, name, encoder, decoder)
				if err == nil {
					if normalizedRegion, found := extractRegion(identityRaw, open, closing); found && normalizedRegion != editedRegion {
						return newResolveError()
					}
				}
			}
		}

		// read-probe: the expression found nothing in resolved content but
		// matches inside this branch
		if baseNull {
			probeOut, err := restoreOutput(probeDoc, exprDoc, probeRaw, false)
			if err == nil && !isNullish(probeOut) && probeOut != base {
				return newResolveError()
			}
		}
	}
	return nil
}

// evaluateAllBranches enumerates every branch of every conditional block,
// bypassing condition resolution. Writes are assembled from one evaluation
// pass per branch; reads print every distinct match annotated with its
// branch condition.
func evaluateAllBranches(expression string, f *codec.File, name string, exprDoc *codec.Doc, out io.Writer, encoder yqlib.Encoder, decoder yqlib.Decoder, appendix []byte) error {
	if len(f.CondBranches()) > 0 {
		assembled, isWrite, err := assembleAllBranchesWrite(expression, f, name, exprDoc, encoder, decoder)
		if err != nil {
			return err
		}
		if isWrite {
			return writeResult(out, assembled, appendix)
		}
	}

	base, err := evalProjection(expression, f, nil, name, exprDoc, encoder, decoder)
	if err != nil {
		return err
	}

	var sb strings.Builder
	if !isNullish(base) {
		sb.WriteString(base)
	}
	for _, ref := range f.CondBranches() {
		branchOut, err := evalProjection(expression, f, codec.ChainSelector(ref), name, exprDoc, encoder, decoder)
		if err != nil {
			return err
		}
		if branchOut != base && !isNullish(branchOut) {
			action, line := f.Describe(ref)
			fmt.Fprintf(&sb, "# branch: %s (%s:%d)\n", action, name, line)
			sb.WriteString(branchOut)
		}
	}
	return writeResult(out, sb.String(), appendix)
}

// assembleAllBranchesWrite runs the expression once per conditional branch
// and splices each branch's edited body back into a single document. The
// skeleton pass (first branch active) provides the non-block content.
// Returns isWrite=false when the expression output is not a document (a
// read), letting the caller fall back to annotated read output.
func assembleAllBranchesWrite(expression string, f *codec.File, name string, exprDoc *codec.Doc, encoder yqlib.Encoder, decoder yqlib.Decoder) (string, bool, error) {
	topRefs := f.TopLevelCondRefs()
	skeletonRef := topRefs[0]

	skeletonDoc, err := f.Project(codec.ChainSelector(skeletonRef))
	if err != nil {
		return "", false, err
	}
	skeletonRaw, err := evalDoc(expression, skeletonDoc, name, encoder, decoder)
	if err != nil {
		return "", false, err
	}

	// a write outputs the whole document, so the skeleton's active branch
	// region must be present; otherwise this is a read
	open, closing, _ := skeletonDoc.BranchRegionMarkers(skeletonRef)
	if _, found := extractRegion(skeletonRaw, open, closing); !found {
		return "", false, nil
	}

	// build returns the edited region text (action line + edited body) for
	// a branch, recursively splicing the branches nested inside it
	var build func(ref codec.BranchRef) (string, error)
	build = func(ref codec.BranchRef) (string, error) {
		passDoc, err := f.Project(codec.ChainSelector(ref))
		if err != nil {
			return "", err
		}
		passRaw, err := evalDoc(expression, passDoc, name, encoder, decoder)
		if err != nil {
			return "", err
		}
		passOpen, passClose, _ := passDoc.BranchRegionMarkers(ref)
		body, found := extractRegion(passRaw, passOpen, passClose)
		if !found {
			action, line := f.Describe(ref)
			return "", fmt.Errorf("--all write: the expression did not produce a document when %s (%s:%d) was active", action, name, line)
		}
		for _, child := range codec.DirectChildCondRefs(ref) {
			childText, err := build(child)
			if err != nil {
				return "", err
			}
			passDoc.ReplaceBranchRegion(child, childText)
		}
		restoredBody, err := restoreOutput(passDoc, exprDoc, body, false)
		if err != nil {
			return "", err
		}
		if restoredBody == "" {
			return f.ActionLine(ref), nil
		}
		return f.ActionLine(ref) + "\n" + restoredBody, nil
	}

	for _, ref := range topRefs {
		if ref == skeletonRef {
			// its edited body is already inline in the skeleton; only its
			// nested blocks need splicing
			for _, child := range codec.DirectChildCondRefs(ref) {
				childText, err := build(child)
				if err != nil {
					return "", false, err
				}
				skeletonDoc.ReplaceBranchRegion(child, childText)
			}
			continue
		}
		text, err := build(ref)
		if err != nil {
			return "", false, err
		}
		skeletonDoc.ReplaceBranchRegion(ref, text)
	}

	assembled, err := restoreOutput(skeletonDoc, exprDoc, skeletonRaw, true)
	if err != nil {
		return "", false, err
	}
	return assembled, true, nil
}

func writeResult(out io.Writer, result string, appendix []byte) error {
	if _, err := io.WriteString(out, result); err != nil {
		return err
	}
	if frontMatter == "process" && len(appendix) > 0 {
		if _, err := out.Write(appendix); err != nil {
			return err
		}
	}
	completedSuccessfully = true

	if exitStatus && isNullish(result) {
		return fmt.Errorf("no matches found")
	}
	return nil
}

// evalDoc evaluates the expression against a projection and returns the raw
// (unrestored) output.
func evalDoc(expression string, doc *codec.Doc, name string, encoder yqlib.Encoder, decoder yqlib.Decoder) (string, error) {
	node, err := yqlib.ExpressionParser.ParseExpression(expression)
	if err != nil {
		return "", err
	}
	buf := new(bytes.Buffer)
	printer := yqlib.NewPrinter(encoder, yqlib.NewSinglePrinterWriter(buf))
	if _, err := yqlib.NewStreamEvaluator().Evaluate(name, strings.NewReader(doc.Projection()), node, printer, decoder); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// restoreOutput restores document and expression placeholders; full
// (strict) restore is used for in-place edits.
func restoreOutput(doc, exprDoc *codec.Doc, raw string, full bool) (string, error) {
	restore := doc.Restore
	if full {
		restore = doc.RestoreFull
	}
	restored, err := restore(raw)
	if err != nil {
		return "", err
	}
	return exprDoc.Restore(restored)
}

// evalProjection evaluates the expression against one projection of the
// file and returns the fully restored output text.
func evalProjection(expression string, f *codec.File, selector codec.Selector, name string, exprDoc *codec.Doc, encoder yqlib.Encoder, decoder yqlib.Decoder) (string, error) {
	doc, err := f.Project(selector)
	if err != nil {
		return "", err
	}
	raw, err := evalDoc(expression, doc, name, encoder, decoder)
	if err != nil {
		return "", err
	}
	return restoreOutput(doc, exprDoc, raw, writeInplace)
}

// extractRegion returns the text between the two marker comment lines.
func extractRegion(text, open, closing string) (string, bool) {
	lines := strings.Split(text, "\n")
	start := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if start < 0 {
			if trimmed == "# "+open {
				start = i + 1
			}
			continue
		}
		if trimmed == "# "+closing {
			return strings.Join(lines[start:i], "\n"), true
		}
	}
	return "", false
}

// isNullish reports whether an evaluation result carries no matches.
func isNullish(s string) bool {
	t := strings.TrimSpace(s)
	return t == "" || t == "null" || t == "~"
}
