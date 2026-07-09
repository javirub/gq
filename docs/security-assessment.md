# Security Assessment

This is a self-assessment of the most likely and most impactful security
problems that could occur in gq, and how the project mitigates them. See
[architecture.md](architecture.md) for the system model and
[SECURITY.md](../SECURITY.md) for how to report vulnerabilities.

## Attack Surface

gq is a local command-line tool with no network access, no privileged
operations, and no execution of user-provided code. Its entire attack
surface is the parsing of untrusted text: input documents (YAML/JSON,
possibly containing Go-template syntax), values files, and the expression
string.

## Threats and Mitigations

### 1. Malformed or adversarial input crashes or hangs the parser

**Likelihood: high. Impact: low–medium** (denial of service of the local
process; potential for memory-safety issues is limited by Go).

- The template codec (`internal/codec`) is continuously fuzzed; CI runs
  four fuzz targets (`FuzzParse`, `FuzzParseJson`, `FuzzScanLine`,
  `FuzzEncodeExpression`) on every change, and new codec behavior must ship
  with a fuzz seed.
- CI runs the full test suite with the race detector.
- YAML/JSON parsing is delegated to the widely used, fuzzed `yqlib`/go-yaml
  stack.

### 2. Template injection — input causes template code execution

**Likelihood: low. Impact: high** if it existed.

gq **never executes** Go templates. `{{ ... }}` expressions are treated as
opaque byte sequences and preserved verbatim; only whole-line control-flow
blocks are structurally matched, and branch selection merely picks which
existing lines remain. There is no `text/template` execution of input data.

### 3. Unintended file access or modification

**Likelihood: low. Impact: medium.**

gq only reads files named on the command line and only writes when the user
passes `-i` (or shell redirection). It runs with the invoking user's
permissions, spawns no subprocesses, and opens no network connections, so it
cannot escalate beyond what the user could already do.

### 4. Supply-chain compromise of dependencies or releases

**Likelihood: low. Impact: high.**

- Only two direct dependencies (`yqlib`, `cobra`); Go modules verify all
  modules against `sum.golang.org`.
- GitHub Actions are pinned to full commit SHAs; Dependabot updates daily.
- Release artifacts are built by GoReleaser in CI from the tagged source;
  a SHA-256 checksum manifest is signed keyless with Sigstore cosign.
- OpenSSF Scorecard and CodeQL run continuously on the repository.

### 5. Silent output corruption (integrity)

**Likelihood: medium. Impact: medium** — for a tool that edits deployment
manifests, corrupting an untouched part of a file is the most damaging
realistic failure.

- yq parity is enforced in CI against a pinned yq binary for plain
  YAML/JSON.
- Golden-file acceptance tests enforce that unmodified template branches
  stay byte-identical after edits.

## Review Cadence

This assessment is revisited when the architecture changes (new input
formats, new execution paths, new dependencies) and at least once per major
release.
