#! /usr/bin/env bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/common.bash"
  setup_test_home
  export output
  tap_dancer="${TAP_DANCER_BIN:-tap-dancer}"
}

teardown() {
  teardown_test_home
}

function exec_parallel_all_succeed { # @test
  run "$tap_dancer" exec-parallel echo {} ::: a b c
  assert_success
  assert_output --partial "ok 1 - echo a"
  assert_output --partial "ok 2 - echo b"
  assert_output --partial "ok 3 - echo c"
  assert_output --partial "1..3"
}

function exec_parallel_reports_failure { # @test
  run "$tap_dancer" exec-parallel "sh -c 'if [ {} = b ]; then exit 1; fi'" ::: a b c
  assert_failure
  assert_output --partial "ok 1"
  assert_output --partial "not ok 2"
  assert_output --partial "ok 3"
}

function exec_parallel_preserves_argument_order { # @test
  # Use sleep to make later args finish before earlier ones;
  # output order should still follow argument order
  run "$tap_dancer" exec-parallel "sh -c 'if [ {} = a ]; then sleep 0.2; fi; echo {}'" ::: a b c
  assert_success
  assert_output --partial "ok 1 - "
  assert_output --partial "ok 2 - "
  assert_output --partial "ok 3 - "
}

function exec_parallel_missing_separator_fails { # @test
  run "$tap_dancer" exec-parallel echo hello
  assert_failure
  assert_output --partial "missing ::: separator"
}

function exec_parallel_missing_template_fails { # @test
  run "$tap_dancer" exec-parallel ::: a b
  assert_failure
  assert_output --partial "missing command template"
}

function exec_parallel_no_args_after_separator_fails { # @test
  run "$tap_dancer" exec-parallel echo {} :::
  assert_failure
  assert_output --partial "no arguments after :::"
}

function exec_parallel_verbose_includes_diagnostics { # @test
  run "$tap_dancer" exec-parallel --verbose echo {} ::: hello
  assert_success
  assert_output --partial "ok 1 - echo hello"
  assert_output --partial "---"
  assert_output --partial "hello"
}

function exec_parallel_j1_runs_sequentially { # @test
  run "$tap_dancer" exec-parallel -j 1 echo {} ::: a b c d
  assert_success
  assert_output --partial "ok 1 - echo a"
  assert_output --partial "ok 2 - echo b"
  assert_output --partial "ok 3 - echo c"
  assert_output --partial "ok 4 - echo d"
  assert_output --partial "1..4"
}

function exec_parallel_j2_limits_concurrency { # @test
  # Write timestamps to files to verify at most 2 run concurrently.
  # Each job sleeps 0.3s; with -j2 and 4 jobs, total time should be ~0.6s not ~0.3s.
  local dir="$BATS_TEST_TMPDIR/concurrency"
  mkdir -p "$dir"

  run "$tap_dancer" exec-parallel -j 2 \
    "sh -c 'echo start-{} >> $dir/log; sleep 0.3; echo end-{} >> $dir/log'" \
    ::: a b c d
  assert_success
  assert_output --partial "1..4"

  # All 4 jobs completed
  run grep -c "^end-" "$dir/log"
  assert_output "4"
}

function exec_parallel_j1_enforces_serial_execution { # @test
  # With -j1, jobs must run one at a time. We verify by checking that
  # no two jobs overlap: every "start" is preceded by the previous job's "end".
  local dir="$BATS_TEST_TMPDIR/serial"
  mkdir -p "$dir"

  run "$tap_dancer" exec-parallel -j 1 \
    "sh -c 'echo start-{} >> $dir/log; sleep 0.1; echo end-{} >> $dir/log'" \
    ::: a b c
  assert_success

  # Verify no overlap: start/end pairs must alternate (no two starts in a row)
  local pattern=""
  local prev=""
  while IFS= read -r line; do
    local kind="${line%%-*}"
    if [[ $kind == "start" && $prev == "start" ]]; then
      fail "two starts in a row — jobs overlapped: $(cat "$dir/log")"
    fi
    prev="$kind"
  done <"$dir/log"

  # All 3 jobs completed
  run grep -c "^end-" "$dir/log"
  assert_output "3"
}

function exec_parallel_j_flag_with_equals { # @test
  run "$tap_dancer" exec-parallel -j 1 echo {} ::: x y
  assert_success
  assert_output --partial "ok 1 - echo x"
  assert_output --partial "ok 2 - echo y"
  assert_output --partial "1..2"
}

function exec_parallel_jobs_long_flag { # @test
  run "$tap_dancer" exec-parallel --jobs 1 echo {} ::: x y
  assert_success
  assert_output --partial "ok 1 - echo x"
  assert_output --partial "ok 2 - echo y"
  assert_output --partial "1..2"
}

function exec_parallel_j0_means_unlimited { # @test
  # -j 0 is the default (unlimited). Should behave same as no -j flag.
  run "$tap_dancer" exec-parallel -j 0 echo {} ::: a b c
  assert_success
  assert_output --partial "ok 1 - echo a"
  assert_output --partial "ok 3 - echo c"
  assert_output --partial "1..3"
}

function exec_parallel_failure_diagnostics_include_exit_code { # @test
  run "$tap_dancer" exec-parallel "sh -c 'exit 42'" ::: fail
  assert_failure
  assert_output --partial "not ok 1"
  assert_output --partial "exit-code: 42"
}

function exec_parallel_failure_diagnostics_include_stderr { # @test
  run "$tap_dancer" exec-parallel "sh -c 'echo oops >&2; exit 1'" ::: fail
  assert_failure
  assert_output --partial "not ok 1"
  assert_output --partial "oops"
}

function exec_parallel_produces_valid_tap { # @test
  local tap_output
  tap_output=$("$tap_dancer" exec-parallel echo {} ::: a b c 2>/dev/null)
  run "$tap_dancer" validate <<<"$tap_output"
  assert_success
}

function exec_parallel_j1_produces_valid_tap { # @test
  local tap_output
  tap_output=$("$tap_dancer" exec-parallel -j 1 echo {} ::: a b c 2>/dev/null)
  run "$tap_dancer" validate <<<"$tap_output"
  assert_success
}
