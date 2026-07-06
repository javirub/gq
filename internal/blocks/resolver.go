// Package blocks resolves which branch of each {{ if }} block is active by
// evaluating the conditions with Go text/template against a user-supplied
// data context.
package blocks

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/javirub/gq/internal/codec"
)

// Unresolved describes a branch whose condition could not be evaluated.
type Unresolved struct {
	Ref codec.BranchRef
	// Detail explains why, e.g. `key '.enabled' was not provided`
	Detail string
}

// Resolution is the outcome of resolving a file's if-blocks against a data
// context.
type Resolution struct {
	// active branch index per fully resolved if-block (-1: no branch active)
	active map[*codec.Block]int
	// Unresolved lists, in document order, every branch of every reachable
	// if-block whose condition chain could not be evaluated
	Unresolved []Unresolved
}

// Selector returns the codec selector activating the resolved branches.
func (r *Resolution) Selector() codec.Selector {
	return func(ref codec.BranchRef) bool {
		idx, ok := r.active[ref.Block]
		return ok && idx == ref.Index
	}
}

// FullyResolved reports whether every reachable if-block was resolved.
func (r *Resolution) FullyResolved() bool {
	return len(r.Unresolved) == 0
}

// ProbeSelector activates the resolved branches plus the given unresolved
// branch (and the ancestors needed to reach it).
func (r *Resolution) ProbeSelector(ref codec.BranchRef) codec.Selector {
	resolved := r.Selector()
	chain := codec.ChainSelector(ref)
	return func(q codec.BranchRef) bool {
		return resolved(q) || chain(q)
	}
}

// Resolve evaluates the conditions of every reachable if-block in the file.
// Blocks nested inside branches that resolved inactive are dead code and
// are skipped entirely.
func Resolve(f *codec.File, data map[string]any) *Resolution {
	res := &Resolution{active: map[*codec.Block]int{}}
	resolveBlocks(f.Blocks, data, res)
	return res
}

func resolveBlocks(blocksList []*codec.Block, data map[string]any, res *Resolution) {
	for _, b := range blocksList {
		switch b.Kind {
		case codec.BlockIf:
			resolveIfBlock(b, data, res)
		case codec.BlockRange, codec.BlockWith:
			// main body is always active for editing purposes; conditions
			// inside rebind '.' so nested ifs there stay unresolved unless
			// they evaluate against the same root (documented limitation) —
			// they are still walked so probes and --all can reach them
			for _, branch := range b.Branches {
				resolveBlocks(branch.Blocks, data, res)
			}
		case codec.BlockOpaque:
			// never active, nothing to resolve
		}
	}
}

func resolveIfBlock(b *codec.Block, data map[string]any, res *Resolution) {
	activeIdx := -1
	var failDetail string

	for idx, branch := range b.Branches {
		if branch.Keyword == "else" {
			activeIdx = idx
			break
		}
		ok, err := EvalCondition(branch.Cond, data)
		if err != nil {
			failDetail = DescribeEvalError(err)
			activeIdx = -2 // unknown
			break
		}
		if ok {
			activeIdx = idx
			break
		}
	}

	if activeIdx == -2 {
		for idx := range b.Branches {
			res.Unresolved = append(res.Unresolved, Unresolved{
				Ref:    codec.BranchRef{Block: b, Index: idx},
				Detail: failDetail,
			})
		}
		// nested blocks of an unresolved block stay unresolved too: walk
		// them so probes can reach nested content
		for _, branch := range b.Branches {
			resolveBlocks(branch.Blocks, data, res)
		}
		return
	}

	res.active[b] = activeIdx
	// only walk the branch that is actually active; inactive branches are
	// dead code under this data context
	if activeIdx >= 0 {
		resolveBlocks(b.Branches[activeIdx].Blocks, data, res)
	}
}

// EvalCondition evaluates one if/else-if pipeline against the data context
// with exact text/template truthiness. Missing keys are reported as errors
// rather than treated as false.
func EvalCondition(cond string, data map[string]any) (bool, error) {
	tmpl, err := template.New("cond").
		Funcs(sprig.TxtFuncMap()).
		Option("missingkey=error").
		Parse("{{ if " + cond + " }}1{{ else }}0{{ end }}")
	if err != nil {
		return false, fmt.Errorf("invalid condition: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return false, err
	}
	return buf.String() == "1", nil
}

var (
	// `executing "cond" at <.Values.enabled>: map has no entry for key "Values"`
	atExprRe     = regexp.MustCompile(`at <([^>]+)>`)
	missingKeyRe = regexp.MustCompile(`map has no entry for key "([^"]+)"`)
	nilPointerRe = regexp.MustCompile(`nil pointer evaluating [^.]*(\.[\w.]+)`)
)

// DescribeEvalError turns a text/template execution error into a compact,
// user-facing explanation naming the missing key when possible.
func DescribeEvalError(err error) string {
	msg := err.Error()
	missing := ""
	if m := missingKeyRe.FindStringSubmatch(msg); m != nil {
		missing = "." + m[1]
	} else if m := nilPointerRe.FindStringSubmatch(msg); m != nil {
		missing = m[1]
	}
	if missing != "" {
		// the `at <...>` part carries the full path being evaluated, which
		// is more useful than the first missing segment alone
		if m := atExprRe.FindStringSubmatch(msg); m != nil && strings.HasPrefix(m[1], ".") {
			missing = m[1]
		}
		return fmt.Sprintf("key '%s' was not provided", missing)
	}
	// strip the "template: cond:1:2: executing ..." prefix for readability
	if idx := strings.LastIndex(msg, ": "); idx >= 0 {
		return msg[idx+2:]
	}
	return msg
}
