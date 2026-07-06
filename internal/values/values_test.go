package values

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBuildFromFilesMergeOrder(t *testing.T) {
	f1 := writeTemp(t, "base.yaml", "a: 1\nnested:\n  x: keep\n  y: base\n")
	f2 := writeTemp(t, "prod.yaml", "nested:\n  y: prod\nb: 2\n")

	data, err := Build([]string{f1, f2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if data["a"] != 1 || data["b"] != 2 {
		t.Errorf("top-level values wrong: %v", data)
	}
	nested := data["nested"].(map[string]any)
	if nested["x"] != "keep" {
		t.Errorf("deep merge must keep sibling keys: %v", nested)
	}
	if nested["y"] != "prod" {
		t.Errorf("later file must win: %v", nested)
	}
}

func TestBuildFromExpressions(t *testing.T) {
	data, err := Build(nil, []string{".enabled = true", ".test = false", `.nested.name = "app"`})
	if err != nil {
		t.Fatal(err)
	}
	if data["enabled"] != true || data["test"] != false {
		t.Errorf("boolean values wrong: %v", data)
	}
	if data["nested"].(map[string]any)["name"] != "app" {
		t.Errorf("nested assignment wrong: %v", data)
	}
}

func TestBuildExpressionsOverrideFiles(t *testing.T) {
	f := writeTemp(t, "vals.yaml", "enabled: false\n")
	data, err := Build([]string{f}, []string{".enabled = true"})
	if err != nil {
		t.Fatal(err)
	}
	if data["enabled"] != true {
		t.Errorf("--values must win over -f: %v", data)
	}
}

func TestBuildJSONValuesFile(t *testing.T) {
	f := writeTemp(t, "vals.json", `{"enabled": true}`)
	data, err := Build([]string{f}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if data["enabled"] != true {
		t.Errorf("json values file: %v", data)
	}
}

func TestBuildBadExpression(t *testing.T) {
	if _, err := Build(nil, []string{"not a valid ]] expr"}); err == nil {
		t.Error("expected error for invalid --values expression")
	}
}
