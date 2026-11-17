#!/usr/bin/env bash
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
#
# Usage: strip-formula-version.sh <formula file>
#
# Removes the version line from the Homebrew formula goreleaser rendered.
#
# goreleaser writes `version "<version>"` unconditionally and has no option to
# omit it. Homebrew's `brew audit --strict` reports that line as redundant when
# the version scanned from the URL is the same (audit_version in Homebrew's
# resource_auditor.rb), so the formula the tap checks must not carry it.
#
# Removing it is only sound when Homebrew then scans the very version the
# line declared. The rule below is deliberately narrow and a violation fails
# loudly with the formula left untouched:
#
#   - exactly one line matches `^  version "<non empty>"$`, two spaces of
#     indent, and no other line starts with the word version followed by
#     white space (a different indent, a trailing space or a second
#     declaration is an error, not a skip);
#   - the file has at least one url line, and every line starting with `url`
#     has the exact shape
#     `      url "https://github.com/<owner>/<repo>/releases/download/v<U>/<archive>.tar.gz"`
#     with <U> three dotted numbers and the same <U> in all of them. That is
#     the shape Homebrew's "GitHub releases" version parser reads, and it
#     reads <U> out of it (Version.parse in Homebrew's version.rb). An
#     earlier parser could win only for a stem that holds a date, which the
#     <archive> pattern excludes by allowing letters, digits and underscores
#     without a hyphen;
#   - the removed version <V> equals <U>, or is <U>-SNAPSHOT-<hex>. The second
#     form is what a goreleaser snapshot build writes, where the URLs carry
#     the tag goreleaser assumes and the version is the snapshot's own. The
#     step behaves the same on a snapshot and a release, so one skeleton
#     covers both. It never changes what a release installs, since a release
#     has <V> = <U>;
#   - no carriage return, no NUL byte, no empty file.
#
# The result replaces the file atomically (a temporary file in the same
# directory, then rename). The file mode is kept.
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <formula file>" >&2
  exit 2
fi

formula="$1"

fail() {
  echo "strip-formula-version: ${formula}: $1" >&2
  exit 1
}

if [ ! -f "${formula}" ]; then
  fail "no such file"
fi
if [ ! -s "${formula}" ]; then
  fail "the file is empty"
fi

# The bash this runs on may drop a NUL byte on read (3.2 does), so the file is
# compared with a copy that has none.
size="$(wc -c <"${formula}" | tr -d ' ')"
without_nul="$(tr -d '\0' <"${formula}" | wc -c | tr -d ' ')"
if [ "${size}" != "${without_nul}" ]; then
  fail "the file contains a NUL byte"
fi
export LC_ALL=C
if grep -q "$(printf '\r')" "${formula}"; then
  fail "the file contains a carriage return"
fi

version_re='^  version "([^"]+)"$'
loose_version_re='^[[:space:]]*version[[:space:]]'
url_re='^      url "https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/releases/download/v([0-9]+\.[0-9]+\.[0-9]+)/[A-Za-z0-9_]+\.tar\.gz"$'
loose_url_re='^[[:space:]]*url[[:space:]]'

loose_count="$(grep -c -E "${loose_version_re}" "${formula}" || true)"
strict_lines="$(grep -n -E "${version_re}" "${formula}" || true)"
if [ -z "${strict_lines}" ]; then
  fail "no line of the form '  version \"<version>\"' (two spaces of indent)"
fi
strict_count="$(printf '%s\n' "${strict_lines}" | wc -l | tr -d ' ')"
if [ "${strict_count}" -ne 1 ]; then
  fail "expected exactly one version line, found ${strict_count}"
fi
if [ "${loose_count}" -ne 1 ]; then
  fail "found ${loose_count} lines starting with the word version, expected only the one to remove"
fi
line_no="${strict_lines%%:*}"
[[ ${strict_lines#*:} =~ ${version_re} ]] || fail "the version line does not parse"
declared="${BASH_REMATCH[1]}"

url_version=""
url_count=0
while IFS= read -r line; do
  url_count=$((url_count + 1))
  if ! [[ ${line} =~ ${url_re} ]]; then
    fail "the url line '${line}' is not a plain GitHub release download of the shape v<major>.<minor>.<patch>"
  fi
  if [ -z "${url_version}" ]; then
    url_version="${BASH_REMATCH[1]}"
  elif [ "${url_version}" != "${BASH_REMATCH[1]}" ]; then
    fail "the urls carry different versions: ${url_version} and ${BASH_REMATCH[1]}"
  fi
done < <(grep -E "${loose_url_re}" "${formula}" || true)
if [ "${url_count}" -eq 0 ]; then
  fail "the formula has no url line"
fi

snapshot_re="^${url_version//./\\.}-SNAPSHOT-[0-9a-f]+\$"
if [ "${declared}" != "${url_version}" ] && ! [[ ${declared} =~ ${snapshot_re} ]]; then
  fail "the version ${declared} is not the version ${url_version} the urls carry, so removing it would change what the formula installs"
fi

# A name in the formula's own directory, so the rename below stays on one
# file system. The process id keeps two runs apart, and nothing else writes
# this directory while the step runs.
tmp="${formula}.strip.$$"
trap 'rm -f "${tmp}"' EXIT
# cp -p carries the mode over to the new file, the redirect below keeps it.
cp -p "${formula}" "${tmp}"
awk -v n="${line_no}" 'NR != n' "${formula}" >"${tmp}"
mv -f "${tmp}" "${formula}"
