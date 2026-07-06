# gq

[![CI](https://github.com/javirub/gq/actions/workflows/ci.yml/badge.svg)](https://github.com/javirub/gq/actions/workflows/ci.yml)
[![CodeQL](https://github.com/javirub/gq/actions/workflows/codeql.yml/badge.svg)](https://github.com/javirub/gq/actions/workflows/codeql.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/javirub/gq/badge)](https://scorecard.dev/viewer/?uri=github.com/javirub/gq)
[![codecov](https://codecov.io/gh/javirub/gq/branch/main/graph/badge.svg)](https://codecov.io/gh/javirub/gq)
[![Go Report Card](https://goreportcard.com/badge/github.com/javirub/gq)](https://goreportcard.com/report/github.com/javirub/gq)
[![Release](https://img.shields.io/github/v/release/javirub/gq)](https://github.com/javirub/gq/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**gq** is a yq-compatible command-line YAML/JSON processor that also understands
**Go-templated** files (`.yaml.gotmpl`, `.json.gotmpl`, helm/helmfile style):
it can query and edit them **while preserving the `{{ ... }}` template
expressions byte-for-byte**, something plain [yq](https://github.com/mikefarah/yq)
/ [jq](https://github.com/jqlang/jq) cannot do because `name: {{ .x }}` is not
valid YAML.

gq embeds yq's expression engine ([`yqlib`](https://pkg.go.dev/github.com/mikefarah/yq/v4/pkg/yqlib)),
so the full yq/jq-style expression language works out of the box.

```bash
# values.yaml.gotmpl stays a valid template after the edit
gq -i '.name = {{ .Values.name }}' values.yaml.gotmpl

# read a templated value verbatim
gq '.image' values.yaml.gotmpl        # -> repo/{{ .Values.tag }}:stable
```

## Conditional blocks and `--values`

Helmfile-style files contain whole-line control flow:

```yaml
replicas: 3
{{ if .Values.ingress.enabled }}
host: {{ .Values.host }}
{{ else }}
host: none
{{ end }}
```

A key like `.host` exists in *both* branches, so gq refuses to guess. You
resolve the conditions by providing data, exactly like helm:

```bash
# provide values inline (repeatable) or from a file (deep-merged in order)
gq --values '.Values.ingress.enabled = true' '.host' values.yaml.gotmpl
gq -f env/prod.yaml '.host' values.yaml.gotmpl

# writes only touch the active branch; inactive branches stay byte-identical
gq -i --values '.Values.ingress.enabled = true' '.host = "prod.example.com"' values.yaml.gotmpl
```

Without enough values, gq fails with **exit code 2** and names the missing key:

```
cannot resolve '.host': it matches content inside a conditional block

    values.yaml.gotmpl:2   {{ if .Values.ingress.enabled }}

  the condition could not be evaluated: key '.Values.ingress.enabled' was not provided
  hint: add --values '<key> = <value>' (or -f <values-file>), or use --all to target every branch
```

`--all` bypasses resolution: reads print every distinct match annotated with
its branch condition; writes are applied to **every** branch.

```bash
gq --all '.host' values.yaml.gotmpl
gq -i --all '.host = "everywhere"' values.yaml.gotmpl
```

## Rendering

`gq render` fully executes the template (text/template + [sprig](https://masterminds.github.io/sprig/),
helm-like semantics) and emits plain YAML/JSON:

```bash
gq render -f env/prod.yaml values.yaml.gotmpl
gq render --values '.Values.enabled = true' --output-file out.yaml values.yaml.gotmpl
```

Keys missing from the data context are an error (exit code 2), not a silent
`<no value>`.

## yq compatibility

For plain `.yaml`/`.json` files gq behaves like yq (same expression language,
same output, comments preserved). Flags mirrored 1:1 from yq v4.53.3:

| Flag | Behaviour |
|---|---|
| `-i, --inplace` | edit the first given file in place (atomic) |
| `-o, --output-format` | `auto\|yaml\|y\|json\|j` (default `auto`) |
| `-p, --input-format` | `auto\|yaml\|y\|json\|j` (default `auto`, detects `.gotmpl`) |
| `-I, --indent` | output indent level (default 2) |
| `-P, --prettyPrint` | pretty print |
| `-e, --exit-status` | exit 1 when no matches / null / false |
| `-n, --null-input` | create documents from scratch |
| `-r, --unwrapScalar` | print scalar values without quotes (default true) |
| `-N, --no-doc` | no `---` separators |
| `-C / -M` | force colors / no colors |
| `--from-file`, `--expression` | expression from file / forced expression |
| `--front-matter extract\|process` | long-form only (see below) |
| `eval` / `eval-all` | same subcommand semantics as yq |

gq-specific flags: `--values <yq-expr>` (repeatable), `-f/--values-file <file>`
(repeatable, deep merge), `--all`, `--gotmpl` / `--no-gotmpl` (override the
`.gotmpl`-extension autodetection; piped stdin autodetects on `{{`).

**Deliberate divergence:** in yq `-f` means `--front-matter`; gq assigns `-f`
to `--values-file` following the helm/helmfile convention (gq's target
audience). Front matter is still fully supported via the long flag.

**Exit codes:** `0` ok · `1` generic/parse error (like yq) · `2` unresolved
conditional / missing values (gq-specific, CI-friendly).

## How it works

- Inline templates (`name: {{ .x }}`) are swapped for collision-free
  placeholder tokens so the document parses as plain YAML/JSON, then restored
  verbatim in the output (quoting style preserved).
- Whole-line control flow (`{{ if }}`, `{{ else }}`, `{{ range }}`, `{{ with }}`,
  `{{ end }}`) is excised into standalone marker comments; only the *active*
  branch is ever parsed, so duplicate keys across branches never collide and
  inactive branches are byte-identical by construction.
- `{{ if }}` conditions are evaluated with Go text/template (+ sprig) against
  the `--values`/`-f` data, with exact template truthiness.
- A safety valve verifies every marker survives evaluation: an expression
  that would swallow template blocks (e.g. a broad `del(...)`) aborts before
  touching the file.

## Limitations (v1)

- Templates in key position (`name-{{ .env }}: x`) are preserved but not
  addressable by expressions.
- Template actions spanning multiple lines mid-value are rejected.
- Inline control flow in value position (`foo: {{ if .x }}a{{ else }}b{{ end }}`)
  is preserved verbatim, not branch-resolved.
- `{{ define }}` / `{{ template }}` / `{{ block }}` are preserved as opaque
  regions; `render` executes them natively.
- An `{{ if }}` nested inside `{{ range }}`/`{{ with }}` (where `.` is
  rebound) cannot be resolved with `--values`; use `--all`.
- Whole-line control flow inside `.json.gotmpl` is not supported (JSON has no
  comments to anchor the markers); inline templates in JSON work.
- Only one file with control-flow blocks per invocation; `eval-all` does not
  support block files.
- Parsed (active) regions get yq-style normalization on write, exactly like
  `yq -i`; only inactive branches are guaranteed byte-identical.
- Template functions are text/template + sprig; helmfile's `readFile`/`exec`/
  state values are not available.
- yq formats other than YAML/JSON (xml, csv, toml, props, lua, ini, hcl...)
  and `-s/--split-exp`, `-0`, `--security-*` are not supported.

## Install

Download a signed binary from the [releases page](https://github.com/javirub/gq/releases),
or install with Go:

```bash
go install github.com/javirub/gq/cmd/gq@latest
```

Release checksums are signed keyless with [cosign](https://github.com/sigstore/cosign):

```bash
cosign verify-blob --bundle checksums.txt.sig --certificate checksums.txt.pem checksums.txt
```

## GitHub Action

gq is also available as a GitHub Action (same interface as yq's):

```yaml
- name: Bump image tag in templated values
  uses: javirub/gq@v1
  with:
    cmd: -i --values '.Values.ingress.enabled = true' '.image.tag = "${{ github.sha }}"' values.yaml.gotmpl

- name: Read a value
  id: name
  uses: javirub/gq@v1
  with:
    cmd: "'.name' values.yaml.gotmpl"
# -> steps.name.outputs.result
```

## Development

```bash
make build        # build the binary
make check        # lint + unit + race + acceptance suite
make update-golden # regenerate acceptance goldens
```

See [CONTRIBUTING.md](CONTRIBUTING.md). Testing goes beyond yq's own suite:
a portable golden acceptance suite (runs on Linux/macOS/Windows), native Go
fuzzing of the template codec, race detector, coverage, and a CI job that
verifies behavioral parity against a pinned real yq binary.
