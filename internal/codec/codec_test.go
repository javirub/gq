package codec

import (
	"strings"
	"testing"
)

func TestEncodeRestoreRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		format Format
		// templates legitimately left in the projection (e.g. comments)
		templatesInProjection bool
	}{
		{"bare value", "name: {{ .Environment.Name }}\n", Yaml, false},
		{"piped value", "replicas: {{ .Values.replicas | default 3 }}\n", Yaml, false},
		{"double quoted whole", "name: \"{{ .x }}\"\n", Yaml, false},
		{"single quoted whole", "name: '{{ .x }}'\n", Yaml, false},
		{"embedded in string", "image: \"repo/{{ .tag }}:latest\"\n", Yaml, false},
		{"adjacent spans", "combo: {{ .a }}{{ .b }}\n", Yaml, false},
		{"trim markers", "name: {{- .x -}}\n", Yaml, false},
		{"two per line", "a: {{ .x }} and {{ .y }}\n", Yaml, false},
		{"comment untouched", "# a comment with {{ .x }}\nname: y\n", Yaml, true},
		{"no templates", "name: plain\n", Yaml, false},
		{"go string with braces", "tricky: {{ printf \"}}\" }}\n", Yaml, false},
		{"inline non-control action", "key:\n  {{ .wholeLineValue }}\n", Yaml, false},
		{"json bare value", "{\"name\": {{ .x }}}\n", Json, false},
		{"json inside string", "{\"image\": \"repo/{{ .tag }}\"}\n", Json, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := Encode(tt.src, tt.format)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if !tt.templatesInProjection && strings.Contains(doc.Projection(), "{{") {
				t.Errorf("projection still contains templates: %q", doc.Projection())
			}
			restored, err := doc.Restore(doc.Projection())
			if err != nil {
				t.Fatalf("Restore: %v", err)
			}
			if restored != tt.src {
				t.Errorf("round trip mismatch:\n got:  %q\n want: %q", restored, tt.src)
			}
		})
	}
}

func TestBlocksRoundTrip(t *testing.T) {
	for _, src := range []string{
		"{{ if .Values.enabled }}\nname: a\n{{ end }}\n",
		"{{- range .Values.items }}\n- x\n{{- end }}\n",
		"{{/* a comment action */}}\nname: x\n",
		"{{ with .Values }}\nname: x\n{{ end }}\n",
		"{{ if .a }}\nname: a\n{{ else if .b }}\nname: b\n{{ else }}\nname: c\n{{ end }}\nplain: 1\n",
		"{{ define \"helper\" }}\nfoo: bar\n{{ end }}\nname: x\n",
		"outer: 1\n{{ if .a }}\ninner: 2\n{{ if .b }}\ndeep: 3\n{{ end }}\n{{ end }}\n",
	} {
		doc, err := Encode(src, Yaml)
		if err != nil {
			t.Fatalf("Encode(%q): %v", src, err)
		}
		if strings.Contains(doc.Projection(), "{{") {
			t.Errorf("projection still contains templates: %q", doc.Projection())
		}
		restored, err := doc.Restore(doc.Projection())
		if err != nil {
			t.Fatalf("Restore(%q): %v", src, err)
		}
		if restored != src {
			t.Errorf("round trip mismatch:\n got:  %q\n want: %q", restored, src)
		}
	}
}

func TestParseTree(t *testing.T) {
	src := `name: base
{{ if .Values.enabled }}
host: a
{{ else if .Values.other }}
host: b
{{ else }}
host: c
{{ end }}
{{ range .Values.items }}
- item
{{ end }}
`
	f, err := Parse(src, Yaml)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Blocks) != 2 {
		t.Fatalf("expected 2 top-level blocks, got %d", len(f.Blocks))
	}
	ifBlock := f.Blocks[0]
	if ifBlock.Kind != BlockIf || len(ifBlock.Branches) != 3 {
		t.Fatalf("expected if-block with 3 branches, got kind %v with %d", ifBlock.Kind, len(ifBlock.Branches))
	}
	if ifBlock.Branches[0].Cond != ".Values.enabled" {
		t.Errorf("cond: %q", ifBlock.Branches[0].Cond)
	}
	if ifBlock.Branches[1].Keyword != "else if" || ifBlock.Branches[1].Cond != ".Values.other" {
		t.Errorf("else-if branch: %+v", ifBlock.Branches[1])
	}
	if ifBlock.Branches[2].Keyword != "else" {
		t.Errorf("else branch: %+v", ifBlock.Branches[2])
	}
	if f.Blocks[1].Kind != BlockRange {
		t.Errorf("second block should be range")
	}
	if got := len(f.CondBranches()); got != 3 {
		t.Errorf("expected 3 cond branches, got %d", got)
	}
}

func TestProjectBranchSelection(t *testing.T) {
	src := `{{ if .a }}
name: yes-branch
{{ else }}
name: no-branch
{{ end }}
`
	f, err := Parse(src, Yaml)
	if err != nil {
		t.Fatal(err)
	}

	base, err := f.Project(nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(base.Projection(), "name:") {
		t.Errorf("base projection must excise all if branches: %q", base.Projection())
	}

	refs := f.CondBranches()
	first, err := f.Project(ChainSelector(refs[0]))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.Projection(), "name: yes-branch") ||
		strings.Contains(first.Projection(), "no-branch") {
		t.Errorf("if-branch projection wrong: %q", first.Projection())
	}

	second, err := f.Project(ChainSelector(refs[1]))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(second.Projection(), "name: no-branch") ||
		strings.Contains(second.Projection(), "yes-branch") {
		t.Errorf("else-branch projection wrong: %q", second.Projection())
	}
}

func TestProjectRangeBodyActive(t *testing.T) {
	src := "items:\n{{ range .Values.items }}\n  - {{ .name }}\n{{ end }}\n"
	doc, err := Encode(src, Yaml)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Projection(), "  - __GQ_") {
		t.Errorf("range body must stay parseable with inline tokens: %q", doc.Projection())
	}
	restored, err := doc.Restore(doc.Projection())
	if err != nil {
		t.Fatal(err)
	}
	if restored != src {
		t.Errorf("round trip mismatch: %q", restored)
	}
}

func TestRestoreEditedProjection(t *testing.T) {
	src := `replicas: 1
{{ if .a }}
name: a
{{ end }}
`
	doc, err := Encode(src, Yaml)
	if err != nil {
		t.Fatal(err)
	}
	// simulate a yq edit of the parseable part
	edited := strings.Replace(doc.Projection(), "replicas: 1", "replicas: 5", 1)
	restored, err := doc.Restore(edited)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(src, "replicas: 1", "replicas: 5", 1)
	if restored != want {
		t.Errorf("got %q, want %q", restored, want)
	}
}

func TestRestoreLostMarkerFails(t *testing.T) {
	src := "replicas: 1\n{{ if .a }}\nname: a\n{{ end }}\n"
	doc, err := Encode(src, Yaml)
	if err != nil {
		t.Fatal(err)
	}
	// simulate an expression that deleted a marker comment
	lines := strings.Split(doc.Projection(), "\n")
	var kept []string
	for _, l := range lines {
		if strings.Contains(l, "_B0__") {
			continue
		}
		kept = append(kept, l)
	}
	// lenient restore tolerates it (reads extract sub-values)...
	if _, err := doc.Restore(strings.Join(kept, "\n")); err != nil {
		t.Errorf("lenient restore should tolerate missing markers: %v", err)
	}
	// ...but a full-document restore (in-place edits) must abort
	if _, err := doc.RestoreFull(strings.Join(kept, "\n")); err == nil {
		t.Error("expected error for lost marker in full restore")
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse("{{ if .a }}\nname: x\n", Yaml); err == nil {
		t.Error("expected error for unclosed block")
	}
	if _, err := Parse("{{ end }}\n", Yaml); err == nil {
		t.Error("expected error for stray end")
	}
	if _, err := Parse("{{ else }}\n", Yaml); err == nil {
		t.Error("expected error for stray else")
	}
	if _, err := Parse("{\"a\": 1}\n{{ if .x }}\n{{ end }}\n", Json); err == nil {
		t.Error("expected error for blocks in json")
	}
}

func TestEncodeRejectsMultilineAction(t *testing.T) {
	if _, err := Encode("name: {{ .a\n| default 3 }}\n", Yaml); err == nil {
		t.Error("expected error for action spanning multiple lines")
	}
}

func TestEncodeExpression(t *testing.T) {
	doc, encoded, err := EncodeExpression(".name = {{ .Values.name }}")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encoded, "{{") {
		t.Errorf("encoded expression still contains template: %q", encoded)
	}
	if !strings.Contains(encoded, `= "__GQ_`) {
		t.Errorf("template should become a quoted string literal: %q", encoded)
	}

	// simulate what yq would output for that assignment (plain scalar)
	token := doc.spans[0].token
	restored, err := doc.Restore("name: " + token + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if restored != "name: {{ .Values.name }}\n" {
		t.Errorf("got %q", restored)
	}

	// json output keeps the value quoted; synthetic quotes are stripped so
	// the template stays a template
	restored, err = doc.Restore("{\n  \"name\": \"" + token + "\"\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(restored, `"name": {{ .Values.name }}`) {
		t.Errorf("got %q", restored)
	}
}

func TestEncodeExpressionWithoutTemplates(t *testing.T) {
	_, encoded, err := EncodeExpression(".a.b = 3")
	if err != nil {
		t.Fatal(err)
	}
	if encoded != ".a.b = 3" {
		t.Errorf("expression without templates must be unchanged, got %q", encoded)
	}
}

func TestRestoreLeftoverTokenFails(t *testing.T) {
	doc, err := Encode("name: {{ .x }}\n", Yaml)
	if err != nil {
		t.Fatal(err)
	}
	// an output with a token the doc does not know about (same runID,
	// different index) must abort
	if _, err := doc.Restore("bad: __GQ_" + doc.runID + "_T99__\n"); err == nil {
		t.Error("expected error for unrestored placeholder")
	}
}

func TestPlaceholderCollisionAvoided(t *testing.T) {
	// input that already contains something looking like our tokens
	src := "a: __GQ_deadbeef_T0__\nb: {{ .x }}\n"
	doc, err := Encode(src, Yaml)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := doc.Restore(doc.Projection())
	if err != nil {
		t.Fatal(err)
	}
	if restored != src {
		t.Errorf("round trip mismatch: %q", restored)
	}
}

func TestQuotedTemplateKeepsQuotes(t *testing.T) {
	doc, err := Encode("name: \"{{ .x }}\"\n", Yaml)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := doc.Restore(doc.Projection())
	if err != nil {
		t.Fatal(err)
	}
	if restored != "name: \"{{ .x }}\"\n" {
		t.Errorf("quotes must be preserved, got %q", restored)
	}
}
