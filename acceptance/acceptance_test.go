// Package acceptance runs the compiled gq binary end-to-end against golden
// expectations. Unlike yq's bash acceptance suite this one is pure Go, so it
// runs on Linux, macOS and Windows alike.
//
// Regenerate the golden files with:
//
//	go test ./acceptance -update
package acceptance

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

var gqBin string

func TestMain(m *testing.M) {
	flag.Parse()

	dir, err := os.MkdirTemp("", "gq-acceptance")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	gqBin = filepath.Join(dir, "gq")
	if os.PathSeparator == '\\' {
		gqBin += ".exe"
	}
	build := exec.Command("go", "build", "-o", gqBin, "github.com/javirub/gq/cmd/gq")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building gq: %v\n%s", err, out)
		os.Exit(1)
	}

	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type testCase struct {
	name  string
	args  []string
	stdin string
	// files written into the working directory before the run; args refer
	// to them by name
	files map[string]string
	// expected exit code
	wantExit int
	// substrings that must appear in stderr
	errContains []string
	// file whose content after the run is golden-compared (for -i and
	// --output-file cases)
	checkFile string
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

const gotmplInline = `# environment values
name: {{ .Environment.Name }}
image: "repo/{{ .Values.tag }}:stable"
replicas: 3
`

const gotmplBlocks = `replicas: 3
{{ if .Values.enabled }}
host: enabled.example.com
mode: on
{{ else }}
host: disabled.example.com
{{ end }}
tail: end
`

const gotmplNested = `top: 1
{{ if .outer }}
label: o
{{ if .inner }}
label: i
{{ end }}
{{ end }}
`

const renderTmpl = `name: {{ .Values.name }}
{{ if .Values.enabled }}
host: {{ .Values.host | default "fallback.example.com" }}
{{ else }}
host: none
{{ end }}
`

const valuesFile = `Values:
  enabled: true
  tag: v9
  name: app
`

var acceptanceCases = []testCase{
	{
		name:  "read-scalar",
		args:  []string{".name", "f.yaml"},
		files: map[string]string{"f.yaml": plainYaml},
	},
	{
		name:  "identity-keeps-comments",
		args:  []string{".", "f.yaml"},
		files: map[string]string{"f.yaml": plainYaml},
	},
	{
		name:  "output-json",
		args:  []string{"-o", "json", ".image", "f.yaml"},
		files: map[string]string{"f.yaml": plainYaml},
	},
	{
		name:  "json-auto-detect",
		args:  []string{".a.b[1]", "f.json"},
		files: map[string]string{"f.json": `{"a": {"b": [1, 2]}}`},
	},
	{
		name:  "json-to-yaml-pretty",
		args:  []string{"-P", "-oy", ".", "f.json"},
		files: map[string]string{"f.json": `{"a": {"b": [1, 2]}, "on": true}`},
	},
	{
		name:  "indent-four",
		args:  []string{"-I", "4", ".image", "f.yaml"},
		files: map[string]string{"f.yaml": plainYaml},
	},
	{
		name:  "stdin-pipe",
		args:  []string{".a"},
		stdin: "a: from-stdin\n",
	},
	{
		name: "null-input",
		args: []string{"-n", `.a.b = "cat"`},
	},
	{
		name:  "unwrap-scalar-off",
		args:  []string{"-r=false", ".image.tag", "f.yaml"},
		files: map[string]string{"f.yaml": plainYaml},
	},
	{
		name:  "multi-doc-no-separators",
		args:  []string{"-N", ".a", "f.yaml"},
		files: map[string]string{"f.yaml": "a: 1\n---\na: 2\n"},
	},
	{
		name:        "exit-status-no-match",
		args:        []string{"-e", ".missing", "f.yaml"},
		files:       map[string]string{"f.yaml": plainYaml},
		wantExit:    1,
		errContains: []string{"no matches found"},
	},
	{
		name: "eval-all-merge",
		args: []string{"eval-all", "select(fileIndex == 0) * select(fileIndex == 1)", "f1.yaml", "f2.yaml"},
		files: map[string]string{
			"f1.yaml": "a: 1\nb: orig\n",
			"f2.yaml": "b: 2\n",
		},
	},
	{
		name:  "front-matter-extract",
		args:  []string{"--front-matter", "extract", ".title", "post.md"},
		files: map[string]string{"post.md": "---\ntitle: hello\n---\n# body\ntext\n"},
	},
	{
		name:  "front-matter-process",
		args:  []string{"--front-matter", "process", `.title = "bye"`, "post.md"},
		files: map[string]string{"post.md": "---\ntitle: hello\n---\n# body\ntext\n"},
	},
	{
		name:      "inplace-plain",
		args:      []string{"-i", ".replicas = 5", "f.yaml"},
		files:     map[string]string{"f.yaml": plainYaml},
		checkFile: "f.yaml",
	},
	{
		name:  "gotmpl-identity",
		args:  []string{".", "values.yaml.gotmpl"},
		files: map[string]string{"values.yaml.gotmpl": gotmplInline},
	},
	{
		name:  "gotmpl-read-template-value",
		args:  []string{".name", "values.yaml.gotmpl"},
		files: map[string]string{"values.yaml.gotmpl": gotmplInline},
	},
	{
		name:      "gotmpl-edit-preserves-templates",
		args:      []string{"-i", ".replicas = 5", "values.yaml.gotmpl"},
		files:     map[string]string{"values.yaml.gotmpl": gotmplInline},
		checkFile: "values.yaml.gotmpl",
	},
	{
		name:      "gotmpl-template-in-expression",
		args:      []string{"-i", ".env = {{ .Environment.Name }}", "values.yaml.gotmpl"},
		files:     map[string]string{"values.yaml.gotmpl": gotmplInline},
		checkFile: "values.yaml.gotmpl",
	},
	{
		name:  "json-gotmpl-inline",
		args:  []string{".", "cfg.json.gotmpl"},
		files: map[string]string{"cfg.json.gotmpl": "{\"name\": {{ .x }}, \"n\": 1}\n"},
	},
	{
		name:     "blocks-ambiguous-read",
		args:     []string{".host", "values.yaml.gotmpl"},
		files:    map[string]string{"values.yaml.gotmpl": gotmplBlocks},
		wantExit: 2,
		errContains: []string{
			"cannot resolve '.host'",
			"{{ if .Values.enabled }}",
			"'.Values.enabled' was not provided",
		},
	},
	{
		name:  "blocks-values-read",
		args:  []string{"--values", ".Values.enabled = true", ".host", "values.yaml.gotmpl"},
		files: map[string]string{"values.yaml.gotmpl": gotmplBlocks},
	},
	{
		name: "blocks-values-file-read",
		args: []string{"-f", "vals.yaml", ".host", "values.yaml.gotmpl"},
		files: map[string]string{
			"values.yaml.gotmpl": gotmplBlocks,
			"vals.yaml":          valuesFile,
		},
	},
	{
		name:      "blocks-write-active-branch",
		args:      []string{"-i", "--values", ".Values.enabled = true", `.host = "new.example.com"`, "values.yaml.gotmpl"},
		files:     map[string]string{"values.yaml.gotmpl": gotmplBlocks},
		checkFile: "values.yaml.gotmpl",
	},
	{
		name:        "blocks-write-probe-blocked",
		args:        []string{"-i", `.host = "sneaky"`, "values.yaml.gotmpl"},
		files:       map[string]string{"values.yaml.gotmpl": gotmplBlocks},
		wantExit:    2,
		checkFile:   "values.yaml.gotmpl", // must be untouched
		errContains: []string{"cannot resolve"},
	},
	{
		name:      "blocks-write-outside",
		args:      []string{"-i", ".replicas = 7", "values.yaml.gotmpl"},
		files:     map[string]string{"values.yaml.gotmpl": gotmplBlocks},
		checkFile: "values.yaml.gotmpl",
	},
	{
		name:  "blocks-all-read",
		args:  []string{"--all", ".host", "values.yaml.gotmpl"},
		files: map[string]string{"values.yaml.gotmpl": gotmplBlocks},
	},
	{
		name:      "blocks-all-write",
		args:      []string{"-i", "--all", `.host = "everywhere"`, "values.yaml.gotmpl"},
		files:     map[string]string{"values.yaml.gotmpl": gotmplBlocks},
		checkFile: "values.yaml.gotmpl",
	},
	{
		name:      "blocks-all-write-nested",
		args:      []string{"-i", "--all", `.label = "x"`, "values.yaml.gotmpl"},
		files:     map[string]string{"values.yaml.gotmpl": gotmplNested},
		checkFile: "values.yaml.gotmpl",
	},
	{
		name: "render-stdout",
		args: []string{"render", "-f", "vals.yaml", "values.yaml.gotmpl"},
		files: map[string]string{
			"values.yaml.gotmpl": renderTmpl,
			"vals.yaml":          valuesFile,
		},
	},
	{
		name: "render-output-file",
		args: []string{"render", "-f", "vals.yaml", "--output-file", "out.yaml", "values.yaml.gotmpl"},
		files: map[string]string{
			"values.yaml.gotmpl": renderTmpl,
			"vals.yaml":          valuesFile,
		},
		checkFile: "out.yaml",
	},
	{
		name:        "render-missing-values",
		args:        []string{"render", "values.yaml.gotmpl"},
		files:       map[string]string{"values.yaml.gotmpl": renderTmpl},
		wantExit:    2,
		errContains: []string{"cannot render"},
	},
}

func TestAcceptance(t *testing.T) {
	for _, tc := range acceptanceCases {
		t.Run(tc.name, func(t *testing.T) {
			workDir := t.TempDir()
			for name, content := range tc.files {
				if err := os.WriteFile(filepath.Join(workDir, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			cmd := exec.Command(gqBin, tc.args...)
			cmd.Dir = workDir
			if tc.stdin != "" {
				cmd.Stdin = strings.NewReader(tc.stdin)
			}
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			exitCode := 0
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else if err != nil {
				t.Fatalf("running gq: %v", err)
			}

			if exitCode != tc.wantExit {
				t.Errorf("exit code = %d, want %d\nstderr: %s", exitCode, tc.wantExit, stderr.String())
			}
			for _, want := range tc.errContains {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr should contain %q:\n%s", want, stderr.String())
				}
			}

			compareGolden(t, tc.name+".out", stdout.String())
			if tc.checkFile != "" {
				content, err := os.ReadFile(filepath.Join(workDir, tc.checkFile))
				if err != nil {
					t.Fatal(err)
				}
				compareGolden(t, tc.name+".file", string(content))
			}
		})
	}
}

// compareGolden compares got with the golden file, regenerating it when the
// -update flag is set. Golden files are stored with unix line endings.
func compareGolden(t *testing.T, goldenName, got string) {
	t.Helper()
	// Base neutralizes any path traversal in the golden name
	path := filepath.Join("testdata", "golden", filepath.Base(goldenName))
	got = strings.ReplaceAll(got, "\r\n", "\n")

	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden file %s (run 'go test ./acceptance -update'): %v", path, err)
	}
	if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Errorf("mismatch with %s:\n got:  %q\n want: %q", path, got, want)
	}
}
