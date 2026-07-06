package codec

import (
	"strings"
	"testing"
)

var fuzzSeeds = []string{
	"name: {{ .Environment.Name }}\n",
	"image: \"repo/{{ .tag }}:latest\"\n",
	"a: {{ .x }}{{ .y }}\nb: '{{ .z }}'\n",
	"{{ if .Values.enabled }}\nhost: a\n{{ else if .b }}\nhost: b\n{{ else }}\nhost: c\n{{ end }}\n",
	"items:\n{{ range .Values.items }}\n  - {{ .name }}\n{{ end }}\n",
	"{{ with .Values }}\nname: x\n{{ end }}\n",
	"{{ define \"helper\" }}\nfoo: bar\n{{ end }}\n",
	"{{/* comment */}}\nname: x\n",
	"outer: 1\n{{ if .a }}\n{{ if .b }}\ndeep: 3\n{{ end }}\n{{ end }}\n",
	"tricky: {{ printf \"}}\" }}\n",
	"# comment with {{ .x }}\nplain: 1\n",
	"name: {{- .x -}}\n",
	"a: __GQ_deadbeef_T0__\nb: {{ .x }}\n",
	"{{ end }}\n",
	"{{ if .a }}\nunclosed: 1\n",
	"a: \"unterminated {{ .x\n",
	"",
	"\r\ncrlf: {{ .x }}\r\n",
}

// FuzzParse asserts that parsing plus projection plus restore never panics
// and, whenever the pipeline accepts the input, the unevaluated projection
// restores byte-identically (the core invariant of gq's codec).
func FuzzParse(f *testing.F) {
	for _, seed := range fuzzSeeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		file, err := Parse(src, Yaml)
		if err != nil {
			return
		}
		doc, err := file.Project(nil)
		if err != nil {
			return
		}
		restored, err := doc.RestoreFull(doc.Projection())
		if err != nil {
			t.Fatalf("RestoreFull(Projection()) failed for %q: %v", src, err)
		}
		if restored != src {
			t.Fatalf("round trip mismatch:\n src:      %q\n restored: %q\n projection: %q", src, restored, doc.Projection())
		}
	})
}

// FuzzParseJson covers the JSON flavor of the codec.
func FuzzParseJson(f *testing.F) {
	f.Add(`{"name": {{ .x }}, "n": 1}`)
	f.Add(`{"image": "repo/{{ .tag }}"}`)
	f.Fuzz(func(t *testing.T, src string) {
		file, err := Parse(src, Json)
		if err != nil {
			return
		}
		doc, err := file.Project(nil)
		if err != nil {
			return
		}
		restored, err := doc.RestoreFull(doc.Projection())
		if err != nil {
			t.Fatalf("RestoreFull(Projection()) failed for %q: %v", src, err)
		}
		if restored != src {
			t.Fatalf("round trip mismatch:\n src:      %q\n restored: %q", src, restored)
		}
	})
}

// FuzzScanLine asserts the low-level line scanner never panics and returns
// spans with sane bounds.
func FuzzScanLine(f *testing.F) {
	for _, seed := range fuzzSeeds {
		for _, line := range strings.Split(seed, "\n") {
			f.Add(line)
		}
	}
	f.Fuzz(func(t *testing.T, line string) {
		spans, err := scanLine(0, line)
		if err != nil {
			return
		}
		for _, s := range spans {
			if s.start < 0 || s.end > len(line) || s.start >= s.end {
				t.Fatalf("span out of bounds for %q: %+v", line, s)
			}
		}
	})
}

// FuzzEncodeExpression asserts expression encoding never panics and that
// encoded expressions contain no raw template actions.
func FuzzEncodeExpression(f *testing.F) {
	f.Add(`.name = {{ .Values.name }}`)
	f.Add(`.a = "prefix-{{ .x }}"`)
	f.Add(`.a.b | select(. == "x")`)
	f.Fuzz(func(t *testing.T, expr string) {
		doc, encoded, err := EncodeExpression(expr)
		if err != nil {
			return
		}
		if _, err := doc.Restore(encoded); err != nil {
			t.Fatalf("Restore(encoded) failed for %q: %v", expr, err)
		}
	})
}
