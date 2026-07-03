#! /usr/bin/env bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/common.bash"
  setup_test_home
  require_bin TAP_DANCER_LIB tap-dancer-load.bash 2>/dev/null || {
    if [[ -z ${TAP_DANCER_LIB:-} ]]; then
      echo "TAP_DANCER_LIB unset (path to dir containing load.bash)" >&2
      return 1
    fi
  }
  if [[ ! -f "$TAP_DANCER_LIB/load.bash" ]]; then
    echo "TAP_DANCER_LIB=$TAP_DANCER_LIB does not contain load.bash" >&2
    return 1
  fi
}

teardown() {
  teardown_test_home
}

run_bash() {
  TAP_DANCER_LIB="$TAP_DANCER_LIB" bash -c "source \"\$TAP_DANCER_LIB/load.bash\"; $1"
}

function load_emits_pragma_streamed_output { # @test
  run run_bash 'true'
  assert_success
  assert_line --index 0 'TAP version 14'
  assert_line --index 1 'pragma +streamed-output'
}

function tap_run_pass_emits_output_block { # @test
  run run_bash 'tap_run "echo hi" sh -c "echo hello; echo world"'
  assert_success
  assert_output --partial '# Output: 1 - echo hi'
  assert_output --partial '    hello'
  assert_output --partial '    world'
  assert_output --partial 'ok 1 - echo hi'
}

function tap_run_pass_with_no_output_emits_empty_block { # @test
  run run_bash 'tap_run "silent" true'
  assert_success
  assert_output --partial '# Output: 1 - silent'
  assert_output --partial 'ok 1 - silent'
  refute_output --partial '    '
}

function tap_run_fail_emits_yaml_without_output_field { # @test
  run run_bash 'tap_run --no-bail "boom" sh -c "echo bad >&2; exit 7"'
  assert_success
  assert_output --partial '# Output: 1 - boom'
  assert_output --partial '    bad'
  assert_output --partial 'not ok 1 - boom'
  assert_output --partial '  ---'
  assert_output --partial '  severity: fail'
  assert_output --partial '  exitcode: 7'
  assert_output --partial '  ...'
  refute_output --partial '  output: |'
}

function tap_run_fail_with_bail_aborts { # @test
  run run_bash 'tap_run "boom" false; echo unreached'
  assert_failure
  assert_output --partial 'not ok 1 - boom'
  assert_output --partial 'Bail out! boom failed'
  refute_output --partial 'unreached'
}

function tap_run_streams_lines_before_test_point { # @test
  run run_bash 'tap_run "ordered" sh -c "echo first; echo second"'
  assert_success
  local hdr_line out1_line out2_line tp_line
  hdr_line=$(printf '%s\n' "$output" | grep -n '^# Output: 1 - ordered$' | cut -d: -f1)
  out1_line=$(printf '%s\n' "$output" | grep -n '^    first$' | cut -d: -f1)
  out2_line=$(printf '%s\n' "$output" | grep -n '^    second$' | cut -d: -f1)
  tp_line=$(printf '%s\n' "$output" | grep -n '^ok 1 - ordered$' | cut -d: -f1)
  [ "$hdr_line" -lt "$out1_line" ]
  [ "$out1_line" -lt "$out2_line" ]
  [ "$out2_line" -lt "$tp_line" ]
}

function tap_run_output_roundtrips_through_go_validator { # @test
  local tap_dancer="${TAP_DANCER_BIN:-tap-dancer}"
  local script='source "$TAP_DANCER_LIB/load.bash"
tap_run "echo hi" sh -c "echo hello; echo world"
tap_run --no-bail "boom" sh -c "echo bad >&2; exit 7"
tap_run "silent" true
tap_plan 3'
  local stream
  stream=$(TAP_DANCER_LIB="$TAP_DANCER_LIB" bash -c "$script")

  local report
  report=$(printf '%s' "$stream" | "$tap_dancer" validate --format json)

  local total passed failed valid diag_count
  total=$(jq -r '.summary.total_tests' <<<"$report")
  passed=$(jq -r '.summary.passed' <<<"$report")
  failed=$(jq -r '.summary.failed' <<<"$report")
  valid=$(jq -r '.summary.valid' <<<"$report")
  diag_count=$(jq -r '.diagnostics | length // 0' <<<"$report")

  [ "$total" = "3" ]
  [ "$passed" = "2" ]
  [ "$failed" = "1" ]
  [ "$valid" = "true" ]
  [ "$diag_count" = "0" ]
}
