bats_load_library bats-support
bats_load_library bats-assert

# Stand-in for bob's bats-island setup_test_home / teardown_test_home helpers.
# Each test gets a fresh HOME inside BATS_TEST_TMPDIR so commands that touch
# $HOME don't leak between tests.
setup_test_home() {
  TEST_HOME="$BATS_TEST_TMPDIR/home"
  mkdir -p "$TEST_HOME"
  export HOME="$TEST_HOME"
  export XDG_CONFIG_HOME="$TEST_HOME/.config"
  export XDG_CACHE_HOME="$TEST_HOME/.cache"
  export XDG_DATA_HOME="$TEST_HOME/.local/share"
}

teardown_test_home() {
  : # BATS_TEST_TMPDIR is removed by bats automatically
}

require_bin() {
  local var="$1" name="$2"
  local val
  val="${!var:-}"
  if [[ -z "$val" ]]; then
    val=$(command -v "$name" || true)
  fi
  if [[ -z "$val" || ! -x "$val" ]]; then
    echo "missing $name (set $var or put on PATH)" >&2
    return 1
  fi
  printf -v "$var" '%s' "$val"
  export "$var"
}

require_bin TAP_DANCER_BIN tap-dancer
