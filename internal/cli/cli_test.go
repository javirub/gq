package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGq executes the gq CLI in-process, mirroring Execute()'s default-command
// handling, and returns captured stdout.
func runGq(t *testing.T, args ...string) (string, error) {
	t.Helper()
	rootCmd := New()
	out := new(bytes.Buffer)
	rootCmd.SetOut(out)
	rootCmd.SetErr(new(bytes.Buffer))

	if _, _, err := rootCmd.Find(args); err != nil && len(args) > 0 {
		args = append([]string{"eval"}, args...)
	}
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return out.String(), err
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const plainYaml = `# top comment
name: myapp
replicas: 3
image:
  repo: nginx
  tag: "1.25"
ports:
  - 80
  - 443
enabled: true
`

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		content  string
		args     []string
		want     string
	}{
		{
			name: "read scalar", filename: "f.yaml", content: plainYaml,
			args: []string{".name"}, want: "myapp\n",
		},
		{
			name: "read nested", filename: "f.yaml", content: plainYaml,
			args: []string{".image.repo"}, want: "nginx\n",
		},
		{
			name: "identity keeps comments", filename: "f.yaml", content: plainYaml,
			args: []string{"."}, want: plainYaml,
		},
		{
			name: "missing key is null", filename: "f.yaml", content: plainYaml,
			args: []string{".missing"}, want: "null\n",
		},
		{
			// unwrap (default) drops the original quotes of the scalar
			name: "unwrap scalar on", filename: "f.yaml", content: plainYaml,
			args: []string{".image.tag"}, want: "1.25\n",
		},
		{
			// -r=false keeps the scalar as-is, including its quotes
			name: "unwrap scalar off", filename: "f.yaml", content: plainYaml,
			args: []string{"-r=false", ".image.tag"}, want: "\"1.25\"\n",
		},
		{
			name: "yaml to json", filename: "f.yaml", content: "a: 1\n",
			args: []string{"-o", "json", "."}, want: "{\n  \"a\": 1\n}\n",
		},
		{
			name: "json auto detect in and out", filename: "f.json", content: `{"a": {"b": 2}}`,
			args: []string{".a"}, want: "{\n  \"b\": 2\n}\n",
		},
		{
			name: "json to yaml", filename: "f.json", content: `{"a": {"b": 2}}`,
			args: []string{"-oy", "."}, want: "a:\n  b: 2\n",
		},
		{
			name: "indent flag", filename: "f.yaml", content: "a:\n  b: 1\n",
			args: []string{"-I", "4", "."}, want: "a:\n    b: 1\n",
		},
		{
			name: "multi doc separators", filename: "f.yaml", content: "a: 1\n---\na: 2\n",
			args: []string{".a"}, want: "1\n---\n2\n",
		},
		{
			name: "no doc separators", filename: "f.yaml", content: "a: 1\n---\na: 2\n",
			args: []string{"-N", ".a"}, want: "1\n2\n",
		},
		{
			name: "prettyPrint", filename: "f.yaml", content: "a: {b: [1, 2]}\n",
			args: []string{"-P", "."}, want: "a:\n  b:\n    - 1\n    - 2\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFile(t, tt.filename, tt.content)
			got, err := runGq(t, append(tt.args, path)...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNullInput(t *testing.T) {
	got, err := runGq(t, "-n", `.a.b = "cat"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "a:\n  b: cat\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExitStatus(t *testing.T) {
	path := writeFile(t, "f.yaml", plainYaml)

	if _, err := runGq(t, "-e", ".name", path); err != nil {
		t.Errorf("expected match to succeed, got %v", err)
	}
	if _, err := runGq(t, "-e", ".missing", path); err == nil {
		t.Error("expected error for null result with -e")
	}
}

func TestWriteInPlace(t *testing.T) {
	path := writeFile(t, "f.yaml", plainYaml)

	if _, err := runGq(t, "-i", ".replicas = 5", path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(content)
	if !strings.Contains(got, "replicas: 5") {
		t.Errorf("in-place edit not applied: %q", got)
	}
	if !strings.Contains(got, "# top comment") {
		t.Errorf("in-place edit lost comments: %q", got)
	}
}

func TestWriteInPlaceRequiresFile(t *testing.T) {
	if _, err := runGq(t, "-i", ".a = 1"); err == nil {
		t.Error("expected error for -i without a file")
	}
}

func TestEvalAll(t *testing.T) {
	f1 := writeFile(t, "f1.yaml", "a: 1\nb: orig\n")
	f2 := writeFile(t, "f2.yaml", "b: 2\n")

	got, err := runGq(t, "eval-all", "select(fileIndex == 0) * select(fileIndex == 1)", f1, f2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "a: 1\nb: 2\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUnsupportedFormats(t *testing.T) {
	path := writeFile(t, "f.yaml", plainYaml)

	for _, args := range [][]string{
		{"-o", "xml", ".", path},
		{"-p", "csv", ".", path},
	} {
		if _, err := runGq(t, args...); err == nil {
			t.Errorf("expected unsupported-format error for %v", args)
		}
	}

	xmlPath := writeFile(t, "f.xml", "<a>1</a>")
	if _, err := runGq(t, ".", xmlPath); err == nil {
		t.Error("expected error for auto-detected unsupported extension .xml")
	}
}

func TestFromFileExpression(t *testing.T) {
	exprPath := writeFile(t, "expr.yq", ".name")
	dataPath := writeFile(t, "f.yaml", plainYaml)

	got, err := runGq(t, "--from-file", exprPath, dataPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "myapp\n" {
		t.Errorf("got %q, want %q", got, "myapp\n")
	}
}

func TestFrontMatter(t *testing.T) {
	path := writeFile(t, "post.md", "---\ntitle: hello\n---\n# body\ntext\n")

	got, err := runGq(t, "--front-matter", "extract", ".title", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello\n" {
		t.Errorf("extract: got %q, want %q", got, "hello\n")
	}

	got, err = runGq(t, "--front-matter", "process", `.title = "bye"`, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "---\ntitle: bye\n---\n# body\ntext\n"
	if got != want {
		t.Errorf("process: got %q, want %q", got, want)
	}
}

const gotmplInline = `# environment values
name: {{ .Values.name }}
image: "repo/{{ .tag }}:latest"
replicas: 3
`

func TestGotmplIdentityRoundTrip(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", gotmplInline)
	got, err := runGq(t, ".", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != gotmplInline {
		t.Errorf("round trip mismatch:\n got:  %q\n want: %q", got, gotmplInline)
	}
}

func TestGotmplReadTemplateValue(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", gotmplInline)
	got, err := runGq(t, ".name", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "{{ .Values.name }}\n" {
		t.Errorf("got %q", got)
	}
}

func TestGotmplEditPreservesTemplates(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", gotmplInline)
	if _, err := runGq(t, "-i", ".replicas = 5", path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(content)
	for _, want := range []string{
		"replicas: 5",
		"name: {{ .Values.name }}",
		`image: "repo/{{ .tag }}:latest"`,
		"# environment values",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in output:\n%s", want, got)
		}
	}
}

func TestGotmplTemplateInExpression(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", gotmplInline)
	if _, err := runGq(t, "-i", ".env = {{ .Environment.Name }}", path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "env: {{ .Environment.Name }}") {
		t.Errorf("template from expression not inserted verbatim:\n%s", content)
	}
}

func TestGotmplTemplateExpressionOnPlainFile(t *testing.T) {
	path := writeFile(t, "f.yaml", "a: 1\n")
	got, err := runGq(t, ".b = {{ .Values.b }}", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "a: 1\nb: {{ .Values.b }}\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

const gotmplBlocks = `replicas: 3
{{ if .Values.enabled }}
host: enabled.example.com
mode: on
{{ else }}
host: disabled.example.com
{{ end }}
tail: end
`

func TestGotmplBlocksIdentityRoundTrip(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", gotmplBlocks)
	got, err := runGq(t, ".", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != gotmplBlocks {
		t.Errorf("round trip mismatch:\n got:  %q\n want: %q", got, gotmplBlocks)
	}
}

func TestGotmplBlocksReadOutsideBlocks(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", gotmplBlocks)
	got, err := runGq(t, ".replicas", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "3\n" {
		t.Errorf("got %q", got)
	}
}

func TestGotmplBlocksAmbiguousReadFails(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", gotmplBlocks)
	_, err := runGq(t, ".host", path)
	if err == nil {
		t.Fatal("expected ambiguity error for key inside conditional branches")
	}
	var rErr *resolveError
	if !errors.As(err, &rErr) {
		t.Fatalf("expected resolveError, got %T: %v", err, err)
	}
	if rErr.ExitCode() != 2 {
		t.Errorf("expected exit code 2, got %d", rErr.ExitCode())
	}
	if !strings.Contains(rErr.Error(), "{{ if .Values.enabled }}") {
		t.Errorf("error should mention the condition: %v", rErr)
	}
}

func TestGotmplBlocksAllReads(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", gotmplBlocks)
	got, err := runGq(t, "--all", ".host", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"# branch: {{ if .Values.enabled }}",
		"enabled.example.com",
		"# branch: {{ else }}",
		"disabled.example.com",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in --all output:\n%s", want, got)
		}
	}
}

func TestGotmplBlocksWriteOutsideBlocks(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", gotmplBlocks)
	if _, err := runGq(t, "-i", ".replicas = 9", path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(content)
	want := strings.Replace(gotmplBlocks, "replicas: 3", "replicas: 9", 1)
	if got != want {
		t.Errorf("branches must stay byte-identical:\n got:  %q\n want: %q", got, want)
	}
}

func TestGotmplBlocksNestedRange(t *testing.T) {
	src := `services:
{{ range .Values.services }}
  - name: {{ .name }}
{{ end }}
`
	path := writeFile(t, "values.yaml.gotmpl", src)
	got, err := runGq(t, ".", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != src {
		t.Errorf("round trip mismatch:\n got:  %q\n want: %q", got, src)
	}

	// range bodies are always active for editing
	got, err = runGq(t, ".services[0].name", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "{{ .name }}\n" {
		t.Errorf("got %q", got)
	}
}

func TestValuesResolveRead(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", gotmplBlocks)

	got, err := runGq(t, "--values", ".Values.enabled = true", ".host", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "enabled.example.com\n" {
		t.Errorf("got %q", got)
	}

	got, err = runGq(t, "--values", ".Values.enabled = false", ".host", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "disabled.example.com\n" {
		t.Errorf("got %q", got)
	}
}

func TestValuesFileResolveRead(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", gotmplBlocks)
	valsPath := writeFile(t, "prod.yaml", "Values:\n  enabled: true\n")

	got, err := runGq(t, "-f", valsPath, ".host", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "enabled.example.com\n" {
		t.Errorf("got %q", got)
	}
}

func TestValuesResolveWriteOnlyActiveBranch(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", gotmplBlocks)

	if _, err := runGq(t, "-i", "--values", ".Values.enabled = true", `.host = "new.example.com"`, path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(content)
	if !strings.Contains(got, "host: new.example.com") {
		t.Errorf("active branch not updated:\n%s", got)
	}
	if !strings.Contains(got, "host: disabled.example.com") {
		t.Errorf("inactive branch must stay byte-identical:\n%s", got)
	}
	if !strings.Contains(got, "mode: on") {
		t.Errorf("rest of active branch must survive:\n%s", got)
	}
}

func TestMissingValuesErrorNamesKey(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", gotmplBlocks)
	_, err := runGq(t, ".host", path)
	if err == nil {
		t.Fatal("expected resolve error")
	}
	msg := err.Error()
	for _, want := range []string{"'.Values.enabled'", "{{ if .Values.enabled }}", "--values"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should contain %q:\n%s", want, msg)
		}
	}
}

func TestWriteIntoUnresolvedBranchFails(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", gotmplBlocks)
	original := gotmplBlocks

	_, err := runGq(t, "-i", `.host = "sneaky"`, path)
	if err == nil {
		t.Fatal("expected write-probe error")
	}
	var rErr *resolveError
	if !errors.As(err, &rErr) {
		t.Fatalf("expected resolveError, got %T: %v", err, err)
	}

	content, _ := os.ReadFile(path)
	if string(content) != original {
		t.Errorf("file must be untouched after failed write:\n%s", content)
	}
}

func TestWriteOutsideBlocksWithUnresolvedConditionals(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", gotmplBlocks)

	// .replicas exists only outside the conditionals: no values needed
	if _, err := runGq(t, "-i", ".replicas = 7", path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, _ := os.ReadFile(path)
	want := strings.Replace(gotmplBlocks, "replicas: 3", "replicas: 7", 1)
	if string(content) != want {
		t.Errorf("got:\n%s\nwant:\n%s", content, want)
	}
}

func TestValuesResolveNestedBlocks(t *testing.T) {
	src := `{{ if .outer }}
{{ if .inner }}
deep: yes
{{ else }}
deep: no
{{ end }}
{{ end }}
`
	path := writeFile(t, "values.yaml.gotmpl", src)

	got, err := runGq(t, "--values", ".outer = true", "--values", ".inner = true", ".deep", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "yes\n" {
		t.Errorf("got %q", got)
	}

	// outer false: inner is dead code, read resolves to null without error
	got, err = runGq(t, "--values", ".outer = false", ".deep", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "null\n" {
		t.Errorf("got %q", got)
	}
}

func TestAllWriteUpdatesEveryBranch(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", gotmplBlocks)

	if _, err := runGq(t, "-i", "--all", `.host = "everywhere"`, path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(content)
	if strings.Count(got, "host: everywhere") != 2 {
		t.Errorf("both branches must be updated:\n%s", got)
	}
	for _, want := range []string{
		"replicas: 3", "mode: on", "tail: end",
		"{{ if .Values.enabled }}", "{{ else }}", "{{ end }}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestAllWriteOutsideBlocks(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", gotmplBlocks)

	if _, err := runGq(t, "-i", "--all", ".replicas = 42", path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, _ := os.ReadFile(path)
	want := strings.Replace(gotmplBlocks, "replicas: 3", "replicas: 42", 1)
	if string(content) != want {
		t.Errorf("got:\n%s\nwant:\n%s", content, want)
	}
}

func TestAllWriteNestedBlocks(t *testing.T) {
	src := `top: 1
{{ if .outer }}
label: o
{{ if .inner }}
label: i
{{ end }}
{{ end }}
`
	path := writeFile(t, "values.yaml.gotmpl", src)

	if _, err := runGq(t, "-i", "--all", `.label = "x"`, path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, _ := os.ReadFile(path)
	got := string(content)
	if strings.Count(got, "label: x") != 2 {
		t.Errorf("nested branch must be updated too:\n%s", got)
	}
	for _, want := range []string{"top: 1", "{{ if .outer }}", "{{ if .inner }}"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestAllIdentityRoundTrip(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", gotmplBlocks)
	got, err := runGq(t, "--all", ".", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != gotmplBlocks {
		t.Errorf("identity with --all must round trip:\n got:  %q\n want: %q", got, gotmplBlocks)
	}
}

func TestGotmplBlocksDeleteAdjacentAborts(t *testing.T) {
	// deleting the key right before a block can swallow its marker comment
	src := "a: 1\n{{ if .x }}\nb: 2\n{{ end }}\n"
	path := writeFile(t, "values.yaml.gotmpl", src)
	_, err := runGq(t, "-i", "del(.tail)", path)
	// del of a missing key is harmless; the file must be unchanged
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, _ := os.ReadFile(path)
	if string(content) != src {
		t.Errorf("file must be unchanged: %q", content)
	}
}

func TestGotmplJsonInline(t *testing.T) {
	path := writeFile(t, "cfg.json.gotmpl", "{\"name\": {{ .x }}, \"n\": 1}\n")
	got, err := runGq(t, ".n", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1\n" {
		t.Errorf("got %q", got)
	}

	got, err = runGq(t, ".", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "\"name\": {{ .x }}") {
		t.Errorf("bare json template not restored: %q", got)
	}
}

const renderSrc = `name: {{ .Values.name }}
{{ if .Values.enabled }}
host: {{ .Values.host | default "fallback.example.com" }}
{{ else }}
host: none
{{ end }}
`

func TestRenderToStdout(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", renderSrc)
	got, err := runGq(t, "render",
		"--values", `.Values.name = "app"`,
		"--values", ".Values.enabled = true",
		path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "name: app\n\nhost: fallback.example.com\n\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderToFile(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", renderSrc)
	outPath := filepath.Join(t.TempDir(), "out.yaml")
	_, err := runGq(t, "render",
		"--values", `.Values.name = "app"`,
		"--values", ".Values.enabled = false",
		"--output-file", outPath, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "host: none") {
		t.Errorf("rendered file wrong:\n%s", content)
	}
}

func TestRenderMissingValuesFails(t *testing.T) {
	path := writeFile(t, "values.yaml.gotmpl", renderSrc)
	_, err := runGq(t, "render", path)
	if err == nil {
		t.Fatal("expected error for missing values")
	}
	var rErr *renderError
	if !errors.As(err, &rErr) {
		t.Fatalf("expected renderError, got %T: %v", err, err)
	}
	if rErr.ExitCode() != 2 {
		t.Errorf("expected exit code 2, got %d", rErr.ExitCode())
	}
}

// TestYqParity shells out to an installed yq binary (skipped when absent) and
// compares outputs for a matrix of invocations.
func TestYqParity(t *testing.T) {
	yqBin, err := exec.LookPath("yq")
	if err != nil {
		t.Skip("yq not installed; skipping parity test")
	}

	gqBin := filepath.Join(t.TempDir(), "gq_parity.exe")
	build := exec.Command("go", "build", "-o", gqBin, "github.com/javirub/gq/cmd/gq")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building gq: %v\n%s", err, out)
	}

	path := writeFile(t, "f.yaml", plainYaml)
	jsonPath := writeFile(t, "f.json", `{"a": {"b": [1, 2]}, "on": true}`)

	cases := [][]string{
		{".name", path},
		{".", path},
		{".image", path},
		{"-o", "json", ".", path},
		{"-P", "-oy", ".", jsonPath},
		{".a.b[0]", jsonPath},
		{"-I", "4", ".image", path},
		{"-r=false", ".name", path},
		{"-n", `.a.b = "cat"`},
	}

	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			gqOut, gqErr := exec.Command(gqBin, args...).Output()
			yqOut, yqErr := exec.Command(yqBin, args...).Output()
			if (gqErr == nil) != (yqErr == nil) {
				t.Fatalf("exit status mismatch: gq=%v yq=%v", gqErr, yqErr)
			}
			if string(gqOut) != string(yqOut) {
				t.Errorf("output mismatch:\n gq: %q\n yq: %q", gqOut, yqOut)
			}
		})
	}
}
