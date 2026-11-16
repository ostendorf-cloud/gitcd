#!/usr/bin/env bash
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
# Tests for check-goreleaser-output.sh with the output of "goreleaser check",
# plain and with the colour escape sequences it prints on a CI runner.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
checker="${script_dir}/check-goreleaser-output.sh"
finished=no
# An old bash can stop a script on an expansion it does not know and still
# exit 0, so a run that did not reach its last line fails here.
trap 'if [ "${finished}" != yes ]; then if [ "${failures:-0}" -eq 0 ]; then echo "test script stopped before its last line" >&2; fi; exit 1; fi' EXIT

failures=0
e="$(printf '\033')"

# Plain output, as goreleaser prints it without a terminal.
plain_checking='  • checking                                  path=.goreleaser.yaml'
plain_brews='  • DEPRECATED:  brews  should not be used anymore, check https://goreleaser.com/deprecations#brews for more info'
plain_summary='  • .goreleaser.yaml                                 error=configuration is valid, but uses deprecated properties'
plain_failed='  ⨯ check failed                                     error=1 out of 1 configuration file(s) have issues'

# The same lines with the escape sequences of the CI log.
colour_checking="${e}[1;94m  •${e}[m ${e}[1mchecking${e}[m                                  ${e}[34mpath${e}[m=.goreleaser.yaml"
colour_brews="${e}[1;93m  •${e}[m ${e}[1;93mDEPRECATED: ${e}[40m ${e}[m${e}[94;40mbrews${e}[m${e}[40m ${e}[m should not be used anymore, check ${e}[33mhttps://goreleaser.com/deprecations#brews${e}[m for more info${e}[m"
colour_summary="${e}[1;93m  •${e}[m .goreleaser.yaml                                 ${e}[1;93merror${e}[m=configuration is valid, but uses deprecated properties"
colour_failed="${e}[1;91m  ⨯${e}[m ${e}[1;91mcheck failed${e}[m                                     ${e}[1;91merror${e}[m=1 out of 1 configuration file(s) have issues"
colour_other="${e}[1;93m  •${e}[m ${e}[1;93mDEPRECATED: ${e}[m archives.format should not be used anymore"

# check name want status output
check() {
  local name="$1" want="$2" status="$3" output="$4" got=0
  printf '%s\n' "${output}" | "${checker}" "${status}" >/dev/null 2>&1 || got=$?
  if [ "${want}" = "accept" ] && [ "${got}" -ne 0 ]; then
    echo "FAIL ${name}: rejected with ${got}, want accepted"
    failures=$((failures + 1))
    return
  fi
  if [ "${want}" = "reject" ] && [ "${got}" -eq 0 ]; then
    echo "FAIL ${name}: accepted, want rejected"
    failures=$((failures + 1))
    return
  fi
  echo "PASS ${name}"
}

nl=$'\n'
plain="${plain_checking}${nl}${plain_brews}${nl}${plain_summary}${nl}${plain_failed}"
colour="${colour_checking}${nl}${colour_brews}${nl}${colour_summary}${nl}${colour_failed}"

check "a clean check is accepted" accept 0 "${plain_checking}"
check "the brews deprecation is accepted" accept 2 "${plain}"
check "the brews deprecation with colours is accepted" accept 2 "${colour}"
check "a second deprecation is rejected" reject 2 "${plain_checking}${nl}${plain_brews}${nl}  • DEPRECATED: archives.format should not be used anymore${nl}${plain_summary}"
check "a second deprecation with colours is rejected" reject 2 "${colour_checking}${nl}${colour_brews}${nl}${colour_other}${nl}${colour_summary}"
check "an invalid file is rejected" reject 1 "${plain_checking}${nl}  ⨯ command failed"
check "the notice without its summary line is rejected" reject 2 "${plain_checking}${nl}${plain_brews}"
check "another wording of the notice is rejected" reject 2 "${plain_checking}${nl}  • DEPRECATED: brews should not be used, see the docs${nl}${plain_summary}"
check "an unexpected exit status is rejected" reject 3 "${plain}"

finished=yes
if [ "${failures}" -ne 0 ]; then
  echo "${failures} test(s) failed"
  exit 1
fi

echo "all tests passed"
