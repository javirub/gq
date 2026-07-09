# Contributing to gq

## Development

```bash
make build        # build the binary
make test         # unit tests
make acceptance   # end-to-end golden suite (regen with make update-golden)
make lint         # golangci-lint (config in .golangci.yml)
make race         # race detector
make fuzz         # fuzz the template codec
make check        # everything CI runs
```

Everything is pure Go and runs the same on Linux, macOS and Windows.

## Dependencies

gq deliberately keeps its dependency surface minimal: the only direct
dependencies are [`yqlib`](https://pkg.go.dev/github.com/mikefarah/yq/v4/pkg/yqlib)
(the embedded yq expression engine — the reason gq exists) and
[cobra](https://github.com/spf13/cobra) for the CLI. New direct dependencies
need a strong justification and should be discussed in an issue first;
prefer the standard library.

How dependencies are obtained and tracked:

- **Selection**: only well-maintained, widely used modules with compatible
  licenses (MIT/BSD/Apache-2.0).
- **Obtaining**: through Go modules (`go.mod`/`go.sum`); the Go toolchain
  verifies every module against the checksum database (`sum.golang.org`).
- **Tracking**: Dependabot checks Go modules and GitHub Actions daily and
  opens update PRs; Dependabot security updates are enabled. GitHub Actions
  are pinned to full commit SHAs.

## Testing Policy

All of the above runs automatically in CI on every pull request and on every
push to `main` (see `.github/workflows/ci.yml`): unit tests on Linux, macOS
and Windows, race detector, the acceptance/golden suite, yq-parity checks,
fuzz smoke tests and `govulncheck`. Passing checks are required to merge.

Any change that adds or modifies functionality **must add or update
automated tests** covering it — unit tests for logic, golden `.file` tests
for anything that writes output, and a fuzz seed for new codec behavior.

## Guidelines

- **yq parity is a contract**: for plain `.yaml`/`.json` inputs, gq must
  behave exactly like yq. The acceptance job compares against a pinned yq
  binary; if you change shared behavior, prove parity.
- **Inactive template branches are sacred**: any code path that writes a
  `.gotmpl` file must keep unresolved/inactive branches byte-identical.
  Golden `.file` tests enforce this — add one for new write paths.
- Fix lint findings in the code; never disable rules.
- New codec behavior should come with a fuzz seed exercising it.
