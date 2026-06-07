# CLAUDE.md

This repo holds **the TAP-14 specification** (root-level
`tap-version-14-specification.md` + amendments) **and** the `tap-dancer`
reference implementation (`bash/`, `go/`, `rust/`, `skills/tap14/`).

## Overview

TAP-14 writer library (Go + Rust) and Claude skill plugin. The Go and Rust
implementations verify the same TAP-14 spec compliance: version line, plan,
test points (ok/not ok), YAML diagnostics, directives (SKIP/TODO), bail out,
comments. The Rust crate also ships a tap-ndjson(7) direct producer
(`NdjsonWriter`) and a `Reporter` facade (tty→TAP text, non-tty→ndjson)
consumed by downstream health commands; the Go CLI's `format-ndjson`
transforms TAP text into the same wire format.

## Build & Test

```sh
just build          # nix build
just test           # all impls
just test-go        # Go only
just test-rust      # Rust only
just test-bats      # BATS, requires built tap-dancer CLI
```

## Code Style

- Go: `gofumpt`, package name `tap`, module `github.com/amarbel-llc/tap/go`
- Rust: `cargo fmt` + `cargo clippy`, crate name `tap-dancer`
- All langs: `nix fmt` (treefmt-nix → goimports + gofumpt + rustfmt + nixfmt + shfmt). Also gated by `nix flake check`'s `formatting` check.
