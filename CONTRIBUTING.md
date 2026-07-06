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

## Guidelines

- **yq parity is a contract**: for plain `.yaml`/`.json` inputs, gq must
  behave exactly like yq. The acceptance job compares against a pinned yq
  binary; if you change shared behavior, prove parity.
- **Inactive template branches are sacred**: any code path that writes a
  `.gotmpl` file must keep unresolved/inactive branches byte-identical.
  Golden `.file` tests enforce this — add one for new write paths.
- Fix lint findings in the code; never disable rules.
- New codec behavior should come with a fuzz seed exercising it.
