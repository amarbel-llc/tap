#! /usr/bin/env bats
# bats file_tags=test-runners-ndjson
#
# Conformance for `--format=ndjson` (with --split and --pass-out) on the
# `go-test` and `cargo-test` subcommands. We only exercise `go-test`
# here — `cargo test` is too slow to build a real fixture inside bats,
# and both subcommands share the same emitter pipeline so coverage on
# one is structurally informative for the other.

setup() {
  load "$(dirname "$BATS_TEST_FILE")/common.bash"
  setup_test_home
  export output
  tap_dancer="${TAP_DANCER_BIN:-tap-dancer}"
}

teardown() {
  teardown_test_home
}

# Build a tiny single-test Go module under $BATS_TEST_TMPDIR/$1 and
# echo its absolute path. Caller picks pass/fail with $2 = "pass" or
# "fail".
function make_go_fixture {
  local name="$1"
  local outcome="$2"
  local dir="$BATS_TEST_TMPDIR/$name"
  mkdir -p "$dir"
  cat > "$dir/go.mod" <<EOF
module example
go 1.26
EOF
  case "$outcome" in
    pass)
      cat > "$dir/x_test.go" <<'EOF'
package example
import "testing"
func TestOK(t *testing.T) {}
EOF
      ;;
    fail)
      cat > "$dir/x_test.go" <<'EOF'
package example
import "testing"
func TestFail(t *testing.T) { t.Fatal("boom") }
EOF
      ;;
    *)
      echo "make_go_fixture: unknown outcome $outcome" >&2
      return 1
      ;;
  esac
  echo "$dir"
}

function go_test_format_ndjson_passing_emits_summary { # @test
  local dir
  dir=$(make_go_fixture pass-fixture pass)
  local out_file="$BATS_TEST_TMPDIR/out.ndjson"
  run bash -c "cd '$dir' && '$tap_dancer' go-test --format=ndjson ./..."
  [ "$status" -eq 0 ]
  # Output should be NDJSON, not raw TAP. Last line is the summary.
  echo "$output" | tail -1 | jq -e '.type == "summary" and .failed == 0'
}

function go_test_format_ndjson_failing_exits_one { # @test
  local dir
  dir=$(make_go_fixture fail-fixture fail)
  run bash -c "cd '$dir' && '$tap_dancer' go-test --format=ndjson ./..."
  [ "$status" -eq 1 ]
  echo "$output" | tail -1 | jq -e '.type == "summary" and .failed >= 1'
}

function go_test_format_ndjson_split_routes_failures { # @test
  local dir
  dir=$(make_go_fixture fail-fixture fail)
  local passfile="$BATS_TEST_TMPDIR/passes.ndjson"
  local failfile="$BATS_TEST_TMPDIR/fails.ndjson"
  bash -c "cd '$dir' && '$tap_dancer' go-test --format=ndjson --split --pass-out '$passfile' ./..." > "$failfile" || true
  # The failure file must contain at least one failing test record.
  run jq -r 'select(.type == "test" and .ok == false) | .description' "$failfile"
  [ -n "$output" ]
}

function go_test_format_unknown_is_rejected { # @test
  run bash -c "'$tap_dancer' go-test --format=garbage" 2>&1
  [ "$status" -ne 0 ]
  assert_output --partial "format"
}

function go_test_default_format_is_still_tap { # @test
  local dir
  dir=$(make_go_fixture pass-fixture pass)
  run bash -c "cd '$dir' && '$tap_dancer' go-test ./..."
  [ "$status" -eq 0 ]
  # Default should remain TAP-14 — first line is the version banner.
  echo "$output" | head -1 | grep -q "^TAP version 14"
}
