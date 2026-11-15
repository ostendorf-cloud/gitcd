#!/usr/bin/env bash
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
#
# Usage: goreleaser check 2>&1 | check-goreleaser-output.sh <exit status>
#
# Decides whether the output of "goreleaser check" is acceptable. Exit
# status 0 is. Exit status 2, a valid file that uses a deprecated property,
# is accepted only for exactly the brews deprecation of
# docs/adr/0003-homebrew-formula-not-cask.md: two lines mention a
# deprecation, the notice and the summary, and both match whole. Any other
# exit status, wording or deprecation fails.
#
# goreleaser colours its output on a CI runner, so the ANSI escape sequences
# are removed first. The escape byte comes from printf, since BSD sed does
# not know \x1b.
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <exit status of goreleaser check>" >&2
  exit 2
fi

status="$1"
esc="$(printf '\033')"
out="$(sed "s/${esc}\[[0-9;]*[A-Za-z]//g")"
echo "${out}"

if [ "${status}" -eq 0 ]; then
  exit 0
fi

brews='  • DEPRECATED:  brews  should not be used anymore, check https://goreleaser.com/deprecations#brews for more info'
summary='^  • \.goreleaser\.yaml +error=configuration is valid, but uses deprecated properties$'

if [ "${status}" -eq 2 ] &&
  [ "$(echo "${out}" | grep -ci deprecat)" -eq 2 ] &&
  echo "${out}" | grep -Fxq -- "${brews}" &&
  echo "${out}" | grep -Eq -- "${summary}"; then
  echo "accepted: the brews deprecation, see docs/adr/0003-homebrew-formula-not-cask.md"
  exit 0
fi

echo "goreleaser check exited ${status} with output this check does not accept" >&2
exit "${status}"
