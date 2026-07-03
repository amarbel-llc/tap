#! /usr/bin/env bats
# bats file_tags=format-ndjson

setup() {
  load "$(dirname "$BATS_TEST_FILE")/common.bash"
  setup_test_home
  export output
  tap_dancer="${TAP_DANCER_BIN:-tap-dancer}"
}

teardown() {
  teardown_test_home
}

function format_ndjson_emits_one_record_per_test { # @test
  local input=$'TAP version 14\n1..2\nok 1 - a\nnot ok 2 - b\n'
  run bash -c "printf '%s' '$input' | $tap_dancer format-ndjson"
  # leading plan + 2 test records + 1 summary = 4 lines
  local count=$(echo "$output" | wc -l)
  [ "$count" -eq 4 ]
  # First line is the plan, last line is the summary
  echo "$output" | head -1 | jq -e '.type == "plan"'
  echo "$output" | tail -1 | jq -e '.type == "summary"'
}

function format_ndjson_exit_1_on_failures { # @test
  run bash -c "printf 'TAP version 14\n1..1\nnot ok 1 - fail\n' | $tap_dancer format-ndjson"
  [ "$status" -eq 1 ]
}

function format_ndjson_exit_0_on_all_pass { # @test
  run bash -c "printf 'TAP version 14\n1..1\nok 1 - pass\n' | $tap_dancer format-ndjson"
  [ "$status" -eq 0 ]
}

function format_ndjson_split_routes_by_verdict { # @test
  local passfile="$BATS_TEST_TMPDIR/pass.ndjson"
  local failfile="$BATS_TEST_TMPDIR/fail.ndjson"
  printf 'TAP version 14\n1..2\nok 1 - a\nnot ok 2 - b\n' |
    "$tap_dancer" format-ndjson --split --pass-out "$passfile" >"$failfile" || true

  # Failure stream: plan + 1 test (n=2) + summary
  run jq -s 'length' "$failfile"
  assert_output "3"
  run jq -r 'select(.type == "test") | .n' "$failfile"
  assert_output "2"

  # Pass stream: plan + 1 test (n=1) + summary
  run jq -s 'length' "$passfile"
  assert_output "3"
  run jq -r 'select(.type == "test") | .n' "$passfile"
  assert_output "1"
}

function format_ndjson_split_routes_todo_to_passes { # @test
  local passfile="$BATS_TEST_TMPDIR/pass.ndjson"
  local failfile="$BATS_TEST_TMPDIR/fail.ndjson"
  local input=$'TAP version 14\n1..3\nok 1 - pass\nnot ok 2 - real failure\nnot ok 3 - try harder # TODO not yet implemented\n'
  printf '%s' "$input" |
    "$tap_dancer" format-ndjson --split --pass-out "$passfile" >"$failfile" || true

  # Failure stream: only the genuine failure (n=2) + summary
  run jq -r 'select(.type == "test") | .n' "$failfile"
  assert_output "2"

  # Pass stream: pass (n=1) and TODO (n=3) + summary
  run jq -rs '[.[] | select(.type == "test") | .n] | sort | @csv' "$passfile"
  assert_output "1,3"
}

function format_ndjson_split_without_pass_out_drops_passes { # @test
  run bash -c "printf 'TAP version 14\n1..2\nok 1 - a\nnot ok 2 - b\n' | $tap_dancer format-ndjson --split"
  # plan + the failing record + summary (passes dropped without --pass-out)
  local count=$(echo "$output" | wc -l)
  [ "$count" -eq 3 ]
  # Plan leads the stream; the sole test record is the failure
  echo "$output" | head -1 | jq -e '.type == "plan"'
  test "$(echo "$output" | jq -r 'select(.type == "test") | .ok')" = "false"
}

function format_ndjson_pass_out_without_split_fails { # @test
  run bash -c "printf 'TAP version 14\n1..1\nok 1 - a\n' | $tap_dancer format-ndjson --pass-out /tmp/x.ndjson"
  [ "$status" -eq 2 ]
  assert_output --partial "--pass-out requires --split"
}

function format_ndjson_attaches_yaml_diagnostic { # @test
  local input
  input=$'TAP version 14\n1..1\nnot ok 1 - fail\n  ---\n  message: broken\n  severity: fail\n  ...\n'
  local fails="$BATS_TEST_TMPDIR/fails.ndjson"
  printf '%s' "$input" | "$tap_dancer" format-ndjson >"$fails" || true
  run jq -r 'select(.type == "test") | .diagnostic.message' "$fails"
  assert_output "broken"
}

function format_ndjson_embeds_subtest { # @test
  local input
  input=$'TAP version 14\n1..1\n    # Subtest: child\n    ok 1 - inner pass\n    not ok 2 - inner fail\n    1..2\nnot ok 1 - child\n'
  local fails="$BATS_TEST_TMPDIR/fails.ndjson"
  printf '%s' "$input" | "$tap_dancer" format-ndjson >"$fails" || true
  run jq -r 'select(.type == "test") | .subtest | length' "$fails"
  assert_output "2"
}

function format_ndjson_attaches_output_block { # @test
  local input
  input=$'TAP version 14\n# Output: 1 - build\n    compiling main.rs\n    linking binary\nok 1 - build\n1..1\n'
  local out_file="$BATS_TEST_TMPDIR/out.ndjson"
  printf '%s' "$input" | "$tap_dancer" format-ndjson >"$out_file"
  run jq -r 'select(.type == "test") | .output' "$out_file"
  assert_output --partial "compiling main.rs"
  assert_output --partial "linking binary"
}

function format_ndjson_emits_bailout_record { # @test
  local input=$'TAP version 14\n1..3\nok 1 - first\nBail out! disk full\n'
  local out_file="$BATS_TEST_TMPDIR/out.ndjson"
  printf '%s' "$input" | "$tap_dancer" format-ndjson >"$out_file" || true
  run jq -r 'select(.type == "bailout") | .message' "$out_file"
  assert_output --partial "disk full"
}

function format_ndjson_summary_has_required_fields { # @test
  local input=$'TAP version 14\n1..2\nok 1 - a\nnot ok 2 - b\n'
  local out_file="$BATS_TEST_TMPDIR/out.ndjson"
  printf '%s' "$input" | "$tap_dancer" format-ndjson >"$out_file" || true
  run jq -r 'select(.type == "summary") | [.passed, .failed, .total, .plan_count, .bailed, .valid] | @csv' "$out_file"
  assert_output "1,1,2,2,false,true"
}

function format_ndjson_empty_input_emits_summary_only { # @test
  local out_file="$BATS_TEST_TMPDIR/out.ndjson"
  printf '' | "$tap_dancer" format-ndjson >"$out_file" || true
  local count=$(wc -l <"$out_file")
  [ "$count" -eq 1 ]
  run jq -r '.type' "$out_file"
  assert_output "summary"
}

function format_ndjson_produces_valid_ndjson_each_line { # @test
  local input=$'TAP version 14\n1..2\nok 1 - a\nnot ok 2 - b\n'
  local out_file="$BATS_TEST_TMPDIR/out.ndjson"
  printf '%s' "$input" | "$tap_dancer" format-ndjson >"$out_file" || true
  # Each line MUST be a parseable JSON value
  while IFS= read -r line; do
    echo "$line" | jq -e '.type' >/dev/null || {
      echo "bad line: $line"
      return 1
    }
  done <"$out_file"
}

function format_ndjson_emits_leading_plan_record { # @test
  # A leading TAP plan (before any test point) becomes a first plan record.
  local input=$'TAP version 14\n1..2\nok 1 - a\nok 2 - b\n'
  local out_file="$BATS_TEST_TMPDIR/out.ndjson"
  printf '%s' "$input" | "$tap_dancer" format-ndjson >"$out_file"
  # First record is the plan with count == 2
  head -1 "$out_file" | jq -e '.type == "plan" and .count == 2'
  # summary.plan_count matches the plan record's count
  run jq -r 'select(.type == "summary") | .plan_count' "$out_file"
  assert_output "2"
}

function format_ndjson_trailing_plan_emits_no_plan_record { # @test
  # A trailing TAP plan is not an up-front announcement: no plan record,
  # but plan_count is still reported in the summary.
  local input=$'TAP version 14\nok 1 - a\nok 2 - b\n1..2\n'
  local out_file="$BATS_TEST_TMPDIR/out.ndjson"
  printf '%s' "$input" | "$tap_dancer" format-ndjson >"$out_file"
  run jq -rs '[.[] | select(.type == "plan")] | length' "$out_file"
  assert_output "0"
  run jq -r 'select(.type == "summary") | .plan_count' "$out_file"
  assert_output "2"
}

function format_ndjson_split_emits_plan_first_in_both_streams { # @test
  local passfile="$BATS_TEST_TMPDIR/pass.ndjson"
  local failfile="$BATS_TEST_TMPDIR/fail.ndjson"
  printf 'TAP version 14\n1..2\nok 1 - a\nnot ok 2 - b\n' |
    "$tap_dancer" format-ndjson --split --pass-out "$passfile" >"$failfile" || true
  # The plan record leads both streams.
  head -1 "$failfile" | jq -e '.type == "plan" and .count == 2'
  head -1 "$passfile" | jq -e '.type == "plan" and .count == 2'
}
