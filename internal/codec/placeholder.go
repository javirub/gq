package codec

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// Format is the underlying data format of a templated file.
type Format int

const (
	Yaml Format = iota
	Json
)

// span is an inline template action replaced by a placeholder token.
type span struct {
	token string
	text  string // original action text, verbatim
	// the original action sits inside a quoted scalar: surrounding quotes
	// belong to the document and must be kept on restore
	inQuotes bool
	// synthetic: the token was wrapped in quotes that do not exist in the
	// original (bare template in json, or expression string literal); those
	// quotes are stripped on restore
	synthetic bool
}

// marker is an excised source region (control-flow lines and inactive
// branch bodies) replaced by a standalone comment line in the projection.
type marker struct {
	token string
	text  string // original region text, verbatim (may span lines)
}

// Doc is a templated document encoded into plain parseable YAML/JSON plus a
// side table of template spans and excised regions.
type Doc struct {
	runID         string
	format        Format
	projection    string
	spans         []span
	markers       []marker
	branchRegions map[BranchRef][2]string
	regionMarkers map[BranchRef]int
}

func (d *Doc) lastMarkerToken() string {
	return d.markers[len(d.markers)-1].token
}

func (d *Doc) setBranchRegion(ref BranchRef, open, closing string) {
	if d.branchRegions == nil {
		d.branchRegions = map[BranchRef][2]string{}
	}
	d.branchRegions[ref] = [2]string{open, closing}
}

// BranchRegionMarkers returns the marker tokens delimiting the body of a
// branch that was active in this projection.
func (d *Doc) BranchRegionMarkers(ref BranchRef) (open, closing string, ok bool) {
	pair, ok := d.branchRegions[ref]
	return pair[0], pair[1], ok
}

func (d *Doc) setRegionMarker(ref BranchRef, idx int) {
	if d.regionMarkers == nil {
		d.regionMarkers = map[BranchRef]int{}
	}
	d.regionMarkers[ref] = idx
}

// ReplaceBranchRegion swaps the text that will be spliced back for an
// inactive branch region, used by --all writes to inject the edited body
// produced by another evaluation pass.
func (d *Doc) ReplaceBranchRegion(ref BranchRef, text string) bool {
	idx, ok := d.regionMarkers[ref]
	if !ok {
		return false
	}
	d.markers[idx].text = text
	return true
}

// addMarker registers an excised region and returns the standalone comment
// line that stands in for it in the projection.
func (d *Doc) addMarker(lines []string, indent string) string {
	token := fmt.Sprintf("__GQ_%s_B%d__", d.runID, len(d.markers))
	d.markers = append(d.markers, marker{token: token, text: strings.Join(lines, "\n")})
	return indent + "# " + token
}

// newRunID returns a short random id, guaranteed not to collide with any
// text already present in the given sources.
func newRunID(sources ...string) (string, error) {
	for range 10 {
		buf := make([]byte, 4)
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		id := hex.EncodeToString(buf)
		collision := false
		for _, src := range sources {
			if strings.Contains(src, "__GQ_"+id) {
				collision = true
				break
			}
		}
		if !collision {
			return id, nil
		}
	}
	return "", fmt.Errorf("could not generate a collision-free placeholder id")
}

// Encode parses a templated document and builds its default projection:
// inline templates become placeholder tokens, control-flow blocks are
// excised into marker comments (only default-active branches stay
// parseable).
func Encode(src string, format Format) (*Doc, error) {
	f, err := Parse(src, format)
	if err != nil {
		return nil, err
	}
	return f.Project(nil)
}

// addSpan registers an inline span and returns its replacement text.
func (d *Doc) addSpan(text string, inQuotes bool) string {
	token := fmt.Sprintf("__GQ_%s_T%d__", d.runID, len(d.spans))
	replacement := token
	synthetic := false
	if d.format == Json && !inQuotes {
		// a bare template is not valid JSON: wrap in synthetic quotes
		replacement = `"` + token + `"`
		synthetic = true
	}
	d.spans = append(d.spans, span{token: token, text: text, inQuotes: inQuotes, synthetic: synthetic})
	return replacement
}

// Projection returns the placeholder-encoded text, valid plain YAML/JSON.
func (d *Doc) Projection() string {
	return d.projection
}

// EncodeExpression replaces {{ ... }} actions inside a gq/yq expression with
// quoted string-literal placeholders, so the expression parses with yqlib
// and the resulting values restore to the original template text.
// The returned Doc must also take part in restoring the final output.
func EncodeExpression(expr string) (*Doc, string, error) {
	runID, err := newRunID(expr)
	if err != nil {
		return nil, "", err
	}
	doc := &Doc{runID: runID, format: Yaml}

	if !strings.Contains(expr, "{{") {
		return doc, expr, nil
	}

	var sb strings.Builder
	prev := 0
	// expressions may span lines; scan line by line but rebuild as one text
	offset := 0
	for idx, line := range strings.Split(expr, "\n") {
		spans, err := scanLine(idx, line)
		if err != nil {
			return nil, "", err
		}
		for _, s := range spans {
			// in an expression every template becomes a string literal,
			// regardless of position
			token := fmt.Sprintf("__GQ_%s_E%d__", doc.runID, len(doc.spans))
			doc.spans = append(doc.spans, span{
				token:     token,
				text:      line[s.start:s.end],
				inQuotes:  s.inQuotes,
				synthetic: !s.inQuotes,
			})
			sb.WriteString(expr[prev : offset+s.start])
			if s.inQuotes {
				sb.WriteString(token)
			} else {
				sb.WriteString(`"` + token + `"`)
			}
			prev = offset + s.end
		}
		offset += len(line) + 1
	}
	sb.WriteString(expr[prev:])
	return doc, sb.String(), nil
}

// Restore replaces every placeholder token in the evaluated output with its
// original template text and splices excised regions back where their
// marker comments stand. Tokens wrapped in synthetic quotes are unwrapped
// when the token is the whole quoted scalar. Missing markers are accepted:
// reads extract sub-values that legitimately carry few or no markers.
func (d *Doc) Restore(output string) (string, error) {
	return d.restore(output, false)
}

// RestoreFull is Restore for outputs that must be the complete document
// (e.g. in-place edits): every excised region must reappear exactly once. A
// lost or duplicated marker (e.g. a del(...) that swallowed an adjacent
// comment) aborts with an error rather than corrupt the file.
func (d *Doc) RestoreFull(output string) (string, error) {
	return d.restore(output, true)
}

func (d *Doc) restore(output string, strict bool) (string, error) {
	if len(d.markers) > 0 {
		byLine := make(map[string]int, len(d.markers))
		for i, m := range d.markers {
			byLine["# "+m.token] = i
		}
		seen := make([]int, len(d.markers))
		lines := strings.Split(output, "\n")
		out := make([]string, 0, len(lines))
		for _, line := range lines {
			if i, ok := byLine[strings.TrimSpace(line)]; ok {
				seen[i]++
				out = append(out, d.markers[i].text)
				continue
			}
			out = append(out, line)
		}
		for i, m := range d.markers {
			if seen[i] > 1 || (strict && seen[i] != 1) {
				firstLine := strings.TrimSpace(strings.SplitN(m.text, "\n", 2)[0])
				return "", fmt.Errorf("the expression removed or duplicated content adjacent to a template block (%q); cannot safely reassemble the file", firstLine)
			}
		}
		output = strings.Join(out, "\n")
	}

	for _, s := range d.spans {
		if !strings.Contains(output, s.token) {
			// the expression deleted this value: nothing to restore
			continue
		}
		if s.synthetic || (d.format == Yaml && !s.inQuotes) {
			// strip quotes the original did not have when the token is the
			// entire quoted scalar
			output = stripQuotedToken(output, s.token, s.text)
		}
		output = strings.ReplaceAll(output, s.token, s.text)
	}
	if idx := strings.Index(output, "__GQ_"+d.runID); idx >= 0 {
		return "", fmt.Errorf("internal error: unrestored placeholder remains in output at byte %d", idx)
	}
	return output, nil
}

// stripQuotedToken replaces "token"/'token' with the bare original text,
// but only when the opening quote genuinely opens a string at that point.
// It walks each line with the same quote-state machine as the scanner, so
// quotes that belong to neighboring scalars (e.g. the closing quote of a
// previous string) are never mistaken for synthetic quotes.
func stripQuotedToken(output, token, text string) string {
	lines := strings.Split(output, "\n")
	for li, line := range lines {
		if !strings.Contains(line, token) {
			continue
		}
		var out strings.Builder
		inSingle, inDouble := false, false
		for i := 0; i < len(line); {
			c := line[i]
			if !inSingle && !inDouble && (c == '\'' || c == '"') {
				quoted := string(c) + token + string(c)
				if strings.HasPrefix(line[i:], quoted) {
					out.WriteString(text)
					i += len(quoted)
					continue
				}
			}
			switch {
			case inSingle:
				if c == '\'' {
					if i+1 < len(line) && line[i+1] == '\'' {
						out.WriteByte(c)
						i++
					} else {
						inSingle = false
					}
				}
			case inDouble:
				if c == '\\' && i+1 < len(line) {
					out.WriteByte(c)
					i++
				} else if c == '"' {
					inDouble = false
				}
			case c == '\'':
				inSingle = true
			case c == '"':
				inDouble = true
			}
			out.WriteByte(line[i])
			i++
		}
		lines[li] = out.String()
	}
	return strings.Join(lines, "\n")
}

// HasTemplates reports whether the document contains any template spans.
func (d *Doc) HasTemplates() bool {
	return len(d.spans) > 0
}
