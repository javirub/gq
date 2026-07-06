package codec

import (
	"fmt"
	"strings"
)

// BlockKind classifies a whole-line control-flow construct.
type BlockKind int

const (
	BlockIf BlockKind = iota
	BlockRange
	BlockWith
	// BlockOpaque covers define/block bodies and standalone comment or
	// template-invocation lines: preserved verbatim, never active
	BlockOpaque
)

// Block is a control-flow construct made of one or more branches, delimited
// by whole-line template actions.
type Block struct {
	Kind      BlockKind
	Branches  []*Branch
	StartLine int // line of the opening action
	EndLine   int // line of {{ end }} (== StartLine for single-line opaques)

	parentBranch *Branch
}

// Branch is one arm of a block: the if body, an else-if body, the else
// body, or the single body of range/with/define blocks.
type Branch struct {
	// Keyword is the action keyword that opened the branch: "if",
	// "else if", "else", "range", "with", ...
	Keyword string
	// Cond is the pipeline text of if/else-if/range/with actions
	Cond       string
	MarkerLine int // line of the opening action of this branch
	// body line range: [BodyStart, BodyEnd)
	BodyStart, BodyEnd int
	Blocks             []*Block // nested blocks in source order

	parent *Block
}

// BranchRef addresses one branch of one block.
type BranchRef struct {
	Block *Block
	Index int
}

// File is a parsed templated document: source lines plus the top-level
// block tree.
type File struct {
	format Format
	lines  []string
	Blocks []*Block
}

// Parse builds the block tree of a templated document.
func Parse(src string, format Format) (*File, error) {
	f := &File{format: format, lines: strings.Split(src, "\n")}
	if !strings.Contains(src, "{{") {
		return f, nil
	}

	// current insertion points: nil frame parent means top level
	type frame struct {
		block *Block
	}
	var stack []frame

	appendBlock := func(b *Block) {
		if len(stack) == 0 {
			f.Blocks = append(f.Blocks, b)
		} else {
			parent := stack[len(stack)-1].block
			branch := parent.Branches[len(parent.Branches)-1]
			b.parentBranch = branch
			branch.Blocks = append(branch.Blocks, b)
		}
	}

	for idx, line := range f.lines {
		spans, err := scanLine(idx, line)
		if err != nil {
			return nil, err
		}
		var ctrl *rawSpan
		for i := range spans {
			if spans[i].kind == spanControl {
				ctrl = &spans[i]
				break
			}
		}
		if ctrl == nil {
			continue
		}

		action := line[ctrl.start:ctrl.end]
		keyword := ctrl.keyword
		if isCommentAction(action) {
			keyword = "comment"
		}

		switch keyword {
		case "if", "range", "with", "define", "block":
			kind := map[string]BlockKind{
				"if": BlockIf, "range": BlockRange, "with": BlockWith,
				"define": BlockOpaque, "block": BlockOpaque,
			}[keyword]
			b := &Block{Kind: kind, StartLine: idx}
			branch := &Branch{
				Keyword:    keyword,
				Cond:       actionPipeline(action, keyword),
				MarkerLine: idx,
				BodyStart:  idx + 1,
				parent:     b,
			}
			b.Branches = append(b.Branches, branch)
			appendBlock(b)
			stack = append(stack, frame{block: b})

		case "else":
			if len(stack) == 0 {
				return nil, fmt.Errorf("line %d: '{{ else }}' without an open block", idx+1)
			}
			b := stack[len(stack)-1].block
			b.Branches[len(b.Branches)-1].BodyEnd = idx
			branch := &Branch{
				Keyword:    "else",
				MarkerLine: idx,
				BodyStart:  idx + 1,
				parent:     b,
			}
			if pipeline := actionPipeline(action, "else"); strings.HasPrefix(pipeline, "if ") {
				branch.Keyword = "else if"
				branch.Cond = strings.TrimSpace(strings.TrimPrefix(pipeline, "if "))
			}
			b.Branches = append(b.Branches, branch)

		case "end":
			if len(stack) == 0 {
				return nil, fmt.Errorf("line %d: '{{ end }}' without an open block", idx+1)
			}
			b := stack[len(stack)-1].block
			b.Branches[len(b.Branches)-1].BodyEnd = idx
			b.EndLine = idx
			stack = stack[:len(stack)-1]

		case "template", "comment":
			// standalone single-line opaque
			b := &Block{Kind: BlockOpaque, StartLine: idx, EndLine: idx}
			b.Branches = append(b.Branches, &Branch{
				Keyword:    keyword,
				MarkerLine: idx,
				BodyStart:  idx + 1,
				BodyEnd:    idx + 1,
				parent:     b,
			})
			appendBlock(b)

		default:
			return nil, fmt.Errorf("line %d: unsupported control action %q", idx+1, action)
		}
	}

	if len(stack) > 0 {
		open := stack[len(stack)-1].block
		return nil, fmt.Errorf("line %d: unclosed '{{ %s }}' block (missing '{{ end }}')",
			open.StartLine+1, open.Branches[0].Keyword)
	}

	if format == Json && len(f.Blocks) > 0 {
		return nil, fmt.Errorf("whole-line control-flow blocks are not supported in JSON templates (JSON has no comments to anchor them); use YAML")
	}

	return f, nil
}

// actionPipeline extracts the pipeline text after the keyword:
// "{{- if .Values.enabled }}" -> ".Values.enabled".
func actionPipeline(action, keyword string) string {
	inner := strings.TrimPrefix(action, "{{")
	inner = strings.TrimSuffix(inner, "}}")
	inner = strings.TrimPrefix(inner, "-")
	inner = strings.TrimSuffix(inner, "-")
	inner = strings.TrimSpace(inner)
	inner = strings.TrimPrefix(inner, keyword)
	return strings.TrimSpace(inner)
}

// HasBlocks reports whether the file contains any control-flow blocks.
func (f *File) HasBlocks() bool {
	return len(f.Blocks) > 0
}

// HasCondBlocks reports whether any if-block exists (recursively): those are
// the blocks whose branch must be resolved or enumerated.
func (f *File) HasCondBlocks() bool {
	return len(collectCondBranches(f.Blocks)) > 0
}

// CondBranches lists every branch of every if-block, depth first.
func (f *File) CondBranches() []BranchRef {
	return collectCondBranches(f.Blocks)
}

func collectCondBranches(blocks []*Block) []BranchRef {
	var refs []BranchRef
	for _, b := range blocks {
		if b.Kind == BlockIf {
			for i := range b.Branches {
				refs = append(refs, BranchRef{Block: b, Index: i})
			}
		}
		if b.Kind != BlockOpaque {
			for _, br := range b.Branches {
				refs = append(refs, collectCondBranches(br.Blocks)...)
			}
		}
	}
	return refs
}

// TopLevelCondRefs lists the branches of if-blocks not nested inside
// another if-block (range/with bodies do not count as nesting: they are
// active by default so their if-blocks surface as excised regions).
func (f *File) TopLevelCondRefs() []BranchRef {
	return collectShallowCondBranches(f.Blocks)
}

// DirectChildCondRefs lists the branches of the if-blocks nested directly
// under the given branch, without crossing another if-block.
func DirectChildCondRefs(ref BranchRef) []BranchRef {
	return collectShallowCondBranches(ref.Block.Branches[ref.Index].Blocks)
}

func collectShallowCondBranches(blocks []*Block) []BranchRef {
	var refs []BranchRef
	for _, b := range blocks {
		switch b.Kind {
		case BlockIf:
			for i := range b.Branches {
				refs = append(refs, BranchRef{Block: b, Index: i})
			}
		case BlockRange, BlockWith:
			for _, br := range b.Branches {
				refs = append(refs, collectShallowCondBranches(br.Blocks)...)
			}
		}
	}
	return refs
}

// Describe returns the original action text and 1-based line of the branch
// opening marker, for error messages and --all annotations.
func (f *File) Describe(ref BranchRef) (action string, line int) {
	branch := ref.Block.Branches[ref.Index]
	return strings.TrimSpace(f.lines[branch.MarkerLine]), branch.MarkerLine + 1
}

// ActionLine returns the verbatim source line that opens the branch.
func (f *File) ActionLine(ref BranchRef) string {
	return f.lines[ref.Block.Branches[ref.Index].MarkerLine]
}

// Selector decides whether a branch is explicitly active. It is only
// consulted for branches that are not active by default (if-branches and
// range/with else-branches). Opaque blocks are never active.
type Selector func(ref BranchRef) bool

// ChainSelector activates the given branch plus every ancestor branch
// needed to reach it.
func ChainSelector(target BranchRef) Selector {
	active := map[*Branch]bool{}
	branch := target.Block.Branches[target.Index]
	for branch != nil {
		active[branch] = true
		if branch.parent == nil {
			break
		}
		parentBranch := branch.parent.parentBranch
		branch = parentBranch
	}
	return func(ref BranchRef) bool {
		return active[ref.Block.Branches[ref.Index]]
	}
}

// branchActive applies default activation rules plus the selector.
func branchActive(b *Block, idx int, selector Selector) bool {
	if b.Kind == BlockOpaque {
		return false
	}
	// range/with main body is always active for editing purposes; their
	// else branch (empty collection) is not
	if (b.Kind == BlockRange || b.Kind == BlockWith) && idx == 0 {
		return true
	}
	if selector == nil {
		return false
	}
	return selector(BranchRef{Block: b, Index: idx})
}

// Project builds the placeholder-encoded projection of the file where only
// default-active and selector-chosen branches are parseable content;
// everything else is excised into marker comments.
func (f *File) Project(selector Selector) (*Doc, error) {
	runID, err := newRunID(strings.Join(f.lines, "\n"))
	if err != nil {
		return nil, err
	}
	doc := &Doc{runID: runID, format: f.format}

	var out []string
	if err := f.project(&out, doc, 0, len(f.lines), f.Blocks, selector); err != nil {
		return nil, err
	}
	doc.projection = strings.Join(out, "\n")
	return doc, nil
}

func (f *File) project(out *[]string, doc *Doc, from, to int, blocks []*Block, selector Selector) error {
	cursor := from
	emitPlain := func(upto int) error {
		for ; cursor < upto; cursor++ {
			encoded, err := f.encodeInlineLine(doc, cursor)
			if err != nil {
				return err
			}
			*out = append(*out, encoded)
		}
		return nil
	}

	for _, b := range blocks {
		if err := emitPlain(b.StartLine); err != nil {
			return err
		}

		if b.Kind == BlockOpaque {
			*out = append(*out, doc.addMarker(f.lines[b.StartLine:b.EndLine+1], markerIndent(f.lines[b.StartLine])))
			cursor = b.EndLine + 1
			continue
		}

		firstToken := make([]string, len(b.Branches))
		activeIdxs := []int{}
		for idx, branch := range b.Branches {
			if branchActive(b, idx, selector) {
				// marker for the branch opening line, body stays parseable
				*out = append(*out, doc.addMarker(f.lines[branch.MarkerLine:branch.MarkerLine+1], markerIndent(f.lines[branch.MarkerLine])))
				firstToken[idx] = doc.lastMarkerToken()
				activeIdxs = append(activeIdxs, idx)
				cursor = branch.BodyStart
				if err := f.project(out, doc, branch.BodyStart, branch.BodyEnd, branch.Blocks, selector); err != nil {
					return err
				}
				cursor = branch.BodyEnd
			} else {
				// whole region (marker line + body) excised verbatim
				*out = append(*out, doc.addMarker(f.lines[branch.MarkerLine:branch.BodyEnd], markerIndent(f.lines[branch.MarkerLine])))
				firstToken[idx] = doc.lastMarkerToken()
				doc.setRegionMarker(BranchRef{Block: b, Index: idx}, len(doc.markers)-1)
				cursor = branch.BodyEnd
			}
		}
		// the {{ end }} line
		*out = append(*out, doc.addMarker(f.lines[b.EndLine:b.EndLine+1], markerIndent(f.lines[b.EndLine])))
		endToken := doc.lastMarkerToken()

		// record the marker pair delimiting each active branch body, used
		// by write-probes to detect edits landing inside a branch
		for _, idx := range activeIdxs {
			closing := endToken
			if idx+1 < len(b.Branches) {
				closing = firstToken[idx+1]
			}
			doc.setBranchRegion(BranchRef{Block: b, Index: idx}, firstToken[idx], closing)
		}
		cursor = b.EndLine + 1
	}

	return emitPlain(to)
}

// encodeInlineLine replaces inline template spans of one source line with
// placeholder tokens.
func (f *File) encodeInlineLine(doc *Doc, idx int) (string, error) {
	line := f.lines[idx]
	spans, err := scanLine(idx, line)
	if err != nil {
		return "", err
	}
	if len(spans) == 0 {
		return line, nil
	}
	var sb strings.Builder
	prev := 0
	for _, s := range spans {
		if s.kind == spanControl {
			return "", fmt.Errorf("line %d: internal error: control action not captured by block parser", idx+1)
		}
		sb.WriteString(line[prev:s.start])
		sb.WriteString(doc.addSpan(line[s.start:s.end], s.inQuotes))
		prev = s.end
	}
	sb.WriteString(line[prev:])
	return sb.String(), nil
}

func markerIndent(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}
