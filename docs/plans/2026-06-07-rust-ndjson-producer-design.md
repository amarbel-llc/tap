# Design: tap-ndjson(7) producer support in the Rust crate

- **Date:** 2026-06-07
- **Status:** approved
- **Consumers:** `piggy health` and `ssh-agent-mux health` (in-flight in
  their own sessions) — both need tty→TAP-14 text / non-tty→ndjson from
  the same check sequence, via a git dep on this repo's `rust/` crate.

## Problem

The Rust `tap-dancer` crate writes TAP-14 text only. The tap-ndjson(7)
wire format exists solely in the Go side, and only as a *transformer*
(`tap-dancer format-ndjson`: TAP text in → ndjson out). Rust consumers
that want ndjson when stdout is not a tty would each have to hand-roll
an emitter, and two hand-rolled emitters will drift from the spec.

## Decisions (settled with user)

1. **API shape: enum facade.** A `Reporter` enum wrapping
   `TapWriter | NdjsonWriter` with the shared flat-check method subset.
   One downstream code path, no generics in consumer signatures.
2. **Diagnostic values: `serde_json::Value`.** ndjson passes values
   through natively (ints stay ints per the spec's DIAGNOSTIC PARSING
   rules); the TAP side renders strings via the existing YAML field
   writer and numbers/bools bare (matching today's `exitcode`).
3. **`line` on direct-producer records: `0`.** The spec defines `line`
   as a position in the source TAP stream; a direct producer has no
   source stream. `0` means "no source line" and gets a clarifying
   paragraph in `doc/tap-ndjson.7.scd`. (Rejected: record ordinal —
   plausible-looking but a lie consumers can't distinguish from real
   source positions.)
4. **Delivery: merge + release tag.** After merge, cut `v0.1.12` so
   both downstreams pin a tag, per eng-versioning(7).

## Module layout & dependencies

`rust/src/lib.rs` is already ~2,400 lines; new code goes in modules:

- `rust/src/ndjson.rs` — serde record structs + `NdjsonWriter`
- `rust/src/reporter.rs` — the `Reporter` enum facade
- `lib.rs` gains `mod ndjson; mod reporter;` + re-exports

New deps: `serde` (derive) + `serde_json`. The existing `TapWriter` API
is untouched — purely additive.

## `NdjsonWriter` — direct producer

Holds `&mut dyn Write` (same idiom as `TapWriter`), internal counters,
and emits one spec-conforming record per call:

- `plan_ahead(n)` → `{"type":"plan","count":n}`; error if any test
  record was already emitted (spec: plan must be first).
- `ok(desc)` / `not_ok(desc)` / `skip(desc, reason)` /
  `todo(desc, reason)` / `not_ok_diag(desc, &[(&str, Value)])` → one
  complete `test` record each. All spec fields always present:
  `directive` object for skip/todo else null, `diagnostic` object or
  null, `output` null, `subtest` null, `line` 0.
- `bail_out(message)` → `bailout` record (`line` 0).
- `comment(_)` → no-op (the spec has no comment record and forbids
  inventing record types).
- `finish()` → the mandatory trailing `summary` record: `passed` /
  `failed` / `skipped` / `todo` / `total` counts (skip/todo counted
  regardless of `ok`, per spec), `plan_count` (from `plan_ahead`, else
  0), `bailed`, `valid: true`, `diagnostics: []` — a direct producer
  has no parse diagnostics by construction. Errors on double-finish;
  any record after finish errors.
- `count()` / `has_failures()` for parity with `TapWriter`.

## `Reporter` facade

```rust
pub enum Reporter<'a> { Tap(TapWriter<'a>), Ndjson(NdjsonWriter<'a>) }
```

- `Reporter::auto(w, is_tty)` — tty →
  `TapWriterBuilder::auto(w).build()` (color/locale defaults as today),
  else `NdjsonWriter::new(w)`. The caller supplies
  `std::io::stdout().is_terminal()`; the crate never sniffs fds itself,
  keeping `&mut dyn Write` testable.
- Shared methods: `plan_ahead`, `ok`, `not_ok`,
  `not_ok_diag(&[(&str, Value)])`, `skip`, `todo`, `comment`,
  `bail_out`, `finish`, `count`, `has_failures`.
- `finish()` on the Tap side = trailing `plan()` (idempotent, as
  today).

Deliberately **not** in the facade (YAGNI — both health commands are
flat): subtests, output blocks, spinner / tty-build-last-line. Those
stay TAP-only via direct `TapWriter` use.

## Spec amendment

`doc/tap-ndjson.7.scd` gains a short clarification on the `line` field
(test and bailout records): producers not derived from a TAP source
stream MUST emit `0`, meaning "no source line". Backwards-compatible
under the page's COMPATIBILITY rules (no type change, no field
removal).

## Error handling

Everything returns `io::Result<…>` like the existing API.
Spec-ordering violations (plan after tests, test after finish, double
finish) return `io::Error` of kind `InvalidInput` — explicit, cheap, no
panics.

## Testing

Unit tests in the new modules, same style as existing ones:

- per-record golden assertions lifted from the manpage examples
- full-document ordering test (plan → tests → bailout → summary)
- summary-count semantics (skip/todo counted regardless of `ok`)
- facade dispatch test (same check sequence through both variants)
- ordering-violation errors

No new bats lane: `format_ndjson.bats` targets the transformer binary;
the producer library is exercised at the unit level. `just test-rust`
is the gate (and the merge hook runs the full suite).

## Rollback

Purely additive — no existing infrastructure replaced, no
dual-architecture period needed. Rollback = revert the commit;
downstreams simply don't take the dep. The wire format itself is
already normative and stable (tap-ndjson(7) COMPATIBILITY rules).

## Tuning levers

- **Facade method subset** — current: the flat-check set above. Signal
  to widen: a downstream health command needs subtests or output blocks
  in non-tty mode.
- **`comment()` as no-op in ndjson** — current: dropped. Signal: a
  consumer wants progress/annotation records; that would be a spec
  revision (new record type), not a crate-only change.

## Delivery

Implement → `merge-this-session` (hook runs the full suite) → release
`v0.1.12` via the repo's release flow → notify both sessions (spinclass
chat) with the tag so they pin their git deps.
