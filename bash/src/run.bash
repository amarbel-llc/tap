tap_run() {
  local bail=1
  if [[ $1 == "--no-bail" ]]; then
    bail=0
    shift
  fi

  local desc="$1"
  shift

  _tap_test_num=$((_tap_test_num + 1))

  echo "# Output: ${_tap_test_num} - ${desc}"

  "$@" 2>&1 | awk '{ print "    " $0; fflush() }'
  local status=${PIPESTATUS[0]}

  if [[ $status -eq 0 ]]; then
    echo "ok ${_tap_test_num} - ${desc}"
  else
    echo "not ok ${_tap_test_num} - ${desc}"
    echo "  ---"
    echo "  severity: fail"
    echo "  exitcode: ${status}"
    echo "  ..."
    if [[ $bail -eq 1 ]]; then
      tap_bail_out "${desc} failed"
    fi
  fi
}
