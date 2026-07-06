package blocks

import (
	"strings"
	"testing"

	"github.com/javirub/gq/internal/codec"
)

func TestEvalCondition(t *testing.T) {
	data := map[string]any{
		"enabled": true,
		"count":   0,
		"name":    "prod",
		"nested":  map[string]any{"on": false},
	}

	tests := []struct {
		cond    string
		want    bool
		wantErr bool
	}{
		{".enabled", true, false},
		{".count", false, false},         // zero is falsy
		{".nested.on", false, false},     //
		{`eq .name "prod"`, true, false}, //
		{`and .enabled (eq .name "prod")`, true, false},
		{".missing", false, true},        // missing key -> unknown
		{".nested.missing", false, true}, //
		{"not .enabled", false, false},   //
	}
	for _, tt := range tests {
		got, err := EvalCondition(tt.cond, data)
		if tt.wantErr {
			if err == nil {
				t.Errorf("EvalCondition(%q): expected error", tt.cond)
			}
			continue
		}
		if err != nil {
			t.Errorf("EvalCondition(%q): %v", tt.cond, err)
			continue
		}
		if got != tt.want {
			t.Errorf("EvalCondition(%q) = %v, want %v", tt.cond, got, tt.want)
		}
	}
}

func TestMissingKeyDetail(t *testing.T) {
	_, err := EvalCondition(".enabled", map[string]any{})
	if err == nil {
		t.Fatal("expected error")
	}
	detail := DescribeEvalError(err)
	if !strings.Contains(detail, "'.enabled'") {
		t.Errorf("detail should name the missing key: %q", detail)
	}
}

const resolverSrc = `replicas: 1
{{ if .enabled }}
host: on
{{ else if .fallback }}
host: fb
{{ else }}
host: off
{{ end }}
`

func mustParse(t *testing.T, src string) *codec.File {
	t.Helper()
	f, err := codec.Parse(src, codec.Yaml)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func projected(t *testing.T, f *codec.File, sel codec.Selector) string {
	t.Helper()
	doc, err := f.Project(sel)
	if err != nil {
		t.Fatal(err)
	}
	return doc.Projection()
}

func TestResolveFirstBranch(t *testing.T) {
	f := mustParse(t, resolverSrc)
	res := Resolve(f, map[string]any{"enabled": true})
	if !res.FullyResolved() {
		t.Fatalf("expected fully resolved, unresolved: %v", res.Unresolved)
	}
	p := projected(t, f, res.Selector())
	if !strings.Contains(p, "host: on") || strings.Contains(p, "host: fb") || strings.Contains(p, "host: off") {
		t.Errorf("wrong active branch: %q", p)
	}
}

func TestResolveElseIf(t *testing.T) {
	f := mustParse(t, resolverSrc)
	res := Resolve(f, map[string]any{"enabled": false, "fallback": true})
	if !res.FullyResolved() {
		t.Fatalf("unresolved: %v", res.Unresolved)
	}
	p := projected(t, f, res.Selector())
	if !strings.Contains(p, "host: fb") || strings.Contains(p, "host: on") {
		t.Errorf("wrong active branch: %q", p)
	}
}

func TestResolveElse(t *testing.T) {
	f := mustParse(t, resolverSrc)
	res := Resolve(f, map[string]any{"enabled": false, "fallback": false})
	if !res.FullyResolved() {
		t.Fatalf("unresolved: %v", res.Unresolved)
	}
	p := projected(t, f, res.Selector())
	if !strings.Contains(p, "host: off") {
		t.Errorf("wrong active branch: %q", p)
	}
}

func TestResolveUnknown(t *testing.T) {
	f := mustParse(t, resolverSrc)
	res := Resolve(f, map[string]any{})
	if res.FullyResolved() {
		t.Fatal("expected unresolved block")
	}
	// all three branches of the block are unresolved
	if len(res.Unresolved) != 3 {
		t.Errorf("expected 3 unresolved branches, got %d", len(res.Unresolved))
	}
	if !strings.Contains(res.Unresolved[0].Detail, "'.enabled'") {
		t.Errorf("detail should name missing key: %q", res.Unresolved[0].Detail)
	}
}

func TestResolveSecondCondUnknown(t *testing.T) {
	f := mustParse(t, resolverSrc)
	// first cond false, second unknown: block cannot be resolved
	res := Resolve(f, map[string]any{"enabled": false})
	if res.FullyResolved() {
		t.Fatal("expected unresolved block")
	}
	if !strings.Contains(res.Unresolved[0].Detail, "'.fallback'") {
		t.Errorf("detail should name the unknown second condition: %q", res.Unresolved[0].Detail)
	}
}

func TestResolveNestedSkipsDeadCode(t *testing.T) {
	src := `{{ if .outer }}
{{ if .inner }}
a: 1
{{ end }}
{{ end }}
`
	f := mustParse(t, src)
	// outer resolved inactive: the inner block is dead code, its missing
	// condition must not surface
	res := Resolve(f, map[string]any{"outer": false})
	if !res.FullyResolved() {
		t.Errorf("dead nested block must not report unresolved: %v", res.Unresolved)
	}

	// outer active: inner is reachable and unresolved
	res = Resolve(f, map[string]any{"outer": true})
	if res.FullyResolved() {
		t.Error("reachable nested block with missing key must be unresolved")
	}
}

func TestResolveIfNoBranchActive(t *testing.T) {
	src := "{{ if .a }}\nx: 1\n{{ end }}\nkeep: 1\n"
	f := mustParse(t, src)
	res := Resolve(f, map[string]any{"a": false})
	if !res.FullyResolved() {
		t.Fatalf("unresolved: %v", res.Unresolved)
	}
	p := projected(t, f, res.Selector())
	if strings.Contains(p, "x: 1") {
		t.Errorf("no branch should be active: %q", p)
	}
	if !strings.Contains(p, "keep: 1") {
		t.Errorf("content outside blocks must stay: %q", p)
	}
}
