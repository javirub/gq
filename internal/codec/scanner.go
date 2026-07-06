package codec

import (
	"fmt"
	"strings"
)

// spanKind classifies a {{ ... }} action found in the source.
type spanKind int

const (
	// spanValue is a template in value position (inline), replaced by a
	// placeholder token
	spanValue spanKind = iota
	// spanControl is a whole-line control-flow action (if/else/end/range/
	// with/define/block/template or a comment action), handled by the block
	// codec
	spanControl
)

// rawSpan is a template action located in the source text.
type rawSpan struct {
	kind spanKind
	line int // 0-based line index
	// byte offsets within the line
	start, end int
	// whether the span sits inside a single/double-quoted scalar
	inQuotes bool
	// first word of the action, e.g. "if", "else", "end", ".Values.name"
	keyword string
}

// controlKeywords are the text/template actions that drive control flow when
// they occupy a whole line. "else" covers both "else" and "else if".
var controlKeywords = map[string]bool{
	"if": true, "else": true, "end": true, "range": true,
	"with": true, "define": true, "block": true, "template": true,
}

// scanLine finds every {{ ... }} span in one line of source text.
// It is quote-aware on two levels: spans inside YAML single/double-quoted
// scalars are flagged (so restore keeps them in place), and `}}` inside Go
// string literals within the action does not terminate the span.
// Spans inside YAML comments are ignored: they are already valid YAML.
func scanLine(lineIdx int, line string) ([]rawSpan, error) {
	var spans []rawSpan

	var inSingle, inDouble bool
	i := 0
	for i < len(line) {
		c := line[i]
		switch {
		case inSingle:
			// YAML single-quoted: '' is an escaped quote
			if c == '\'' {
				if i+1 < len(line) && line[i+1] == '\'' {
					i++
				} else {
					inSingle = false
				}
			}
		case inDouble:
			switch c {
			case '\\':
				i++
			case '"':
				inDouble = false
			}
		case c == '\'':
			inSingle = true
		case c == '"':
			inDouble = true
		case c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			// comment until end of line: {{ }} inside it is valid YAML
			return spans, nil
		}

		if strings.HasPrefix(line[i:], "{{") {
			end, err := findActionEnd(line, i)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineIdx+1, err)
			}
			action := line[i:end]
			spans = append(spans, rawSpan{
				kind:     classifySpan(line, i, end, action),
				line:     lineIdx,
				start:    i,
				end:      end,
				inQuotes: inSingle || inDouble,
				keyword:  actionKeyword(action),
			})
			i = end
			continue
		}
		i++
	}
	return spans, nil
}

// findActionEnd returns the byte offset just past the closing "}}" of the
// action starting at offset start, skipping Go string literals inside it.
func findActionEnd(line string, start int) (int, error) {
	i := start + 2 // skip "{{"
	for i < len(line) {
		switch line[i] {
		case '"':
			for i++; i < len(line); i++ {
				if line[i] == '\\' {
					i++
				} else if line[i] == '"' {
					break
				}
			}
			if i >= len(line) {
				return 0, fmt.Errorf("unterminated string inside template action %q", line[start:])
			}
		case '`':
			i++
			for i < len(line) && line[i] != '`' {
				i++
			}
			if i >= len(line) {
				return 0, fmt.Errorf("unterminated raw string inside template action %q", line[start:])
			}
		case '\'':
			for i++; i < len(line); i++ {
				if line[i] == '\\' {
					i++
				} else if line[i] == '\'' {
					break
				}
			}
			if i >= len(line) {
				return 0, fmt.Errorf("unterminated rune literal inside template action %q", line[start:])
			}
		case '}':
			if i+1 < len(line) && line[i+1] == '}' {
				return i + 2, nil
			}
		}
		i++
	}
	return 0, fmt.Errorf("template action %q does not close on the same line (multi-line actions are not supported)", line[start:])
}

// classifySpan decides whether a span drives control flow: it must occupy
// the whole line (only whitespace around it) and start with a control
// keyword or be a pure comment action.
func classifySpan(line string, start, end int, action string) spanKind {
	if strings.TrimSpace(line[:start]) != "" || strings.TrimSpace(line[end:]) != "" {
		return spanValue
	}
	if isCommentAction(action) {
		return spanControl
	}
	if controlKeywords[actionKeyword(action)] {
		return spanControl
	}
	return spanValue
}

// actionKeyword returns the first word inside {{ ... }}, with trim markers
// removed: "{{- if .x }}" -> "if".
func actionKeyword(action string) string {
	inner := strings.TrimPrefix(action, "{{")
	inner = strings.TrimSuffix(inner, "}}")
	inner = strings.TrimPrefix(inner, "-")
	inner = strings.TrimSuffix(inner, "-")
	fields := strings.Fields(inner)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// isCommentAction reports whether the action is {{/* ... */}} (possibly with
// trim markers).
func isCommentAction(action string) bool {
	inner := strings.TrimPrefix(action, "{{")
	inner = strings.TrimPrefix(inner, "-")
	inner = strings.TrimSpace(inner)
	return strings.HasPrefix(inner, "/*")
}
