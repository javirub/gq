# Architecture

This document describes the design of gq: the actors involved, the actions
they can perform, and how data flows through the system.

## Actors

gq has a single actor: the **local user** who runs the `gq` binary. There are
no other actors — gq has no network access, no daemon or server mode, no
plugins, and no privilege boundaries. It runs entirely with the invoking
user's permissions.

## Actions

The user can perform two actions through the command-line interface:

- **Query**: evaluate a yq/jq-style expression against one or more input
  documents and print the result to stdout.
- **Edit**: apply an expression that modifies the document, writing the
  result to stdout or back to the input file (`-i`).

Inputs are read from files given as arguments or from stdin. Values files
(`--values`, `-f`) supply data for resolving Go-template conditional blocks.

## Data Flow

```
input file / stdin
      │
      ▼
codec (internal/codec) ─── detects and protects {{ ... }} template
      │                    expressions so the document parses as YAML/JSON
      ▼
blocks (internal/blocks) ── resolves whole-line control-flow blocks
      │                     ({{ if }} / {{ else }} / {{ end }}) using
      │                     --values data, keeping inactive branches intact
      ▼
yqlib (embedded yq engine) ─ evaluates the user's expression
      │
      ▼
render (internal/render) ── re-encodes the result, restoring template
      │                     expressions byte-for-byte
      ▼
stdout / input file (-i)
```

The CLI layer (`internal/cli`, `cmd/gq`) parses flags with cobra and wires
the pipeline together. For plain `.yaml`/`.json` inputs the codec and blocks
stages are pass-through and gq behaves exactly like yq (this parity is
enforced by CI against a pinned yq binary).

## External Interfaces

The released software has exactly one external interface: the `gq`
command-line interface (flags, expression language, exit codes), documented
in the [README](../README.md). gq reads and writes only the files the user
names on the command line (plus stdin/stdout). It opens no sockets, spawns
no processes, and reads no configuration files.

## Trust Boundaries

All input files, values files, and expressions are treated as untrusted
text. Template expressions in input files are **never executed** — gq
preserves them as opaque byte sequences; only whole-line control-flow
blocks are structurally resolved against user-supplied values. See the
[security assessment](security-assessment.md) for the threat analysis.
