#!/usr/bin/env bash
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
#
# Usage: audit-formula.sh <formula file>
#
# Runs `brew audit --strict` and `brew style` on the formula the release
# would publish, the way the tap's own check does (its scripts/check-formula.sh
# in ostendorf-cloud/homebrew-tap): the formula is committed into a scratch git
# repository, that repository is tapped under its own name, the one formula is
# trusted, both commands run on it, and trust and tap are removed at the end,
# on every exit path. It refuses to run when the scratch tap name is already
# tapped, so it never touches a tap that was there before.
#
# CI only: it needs Homebrew, which ubuntu-latest and macos-latest runners
# carry, and it EXECUTES nothing of the formula, but brew loads it as Ruby on
# this machine. The formula comes from this repository's own build. Run it
# locally only on a machine where a scratch tap is acceptable.
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <formula file>" >&2
  exit 2
fi

formula="$1"
tap="${AUDIT_TAP:-gitcd-ci/audit}"
name="$(basename "${formula}" .rb)"
ref="${tap}/${name}"
work=""
tapped=""
trusted=""

cleanup() {
  # Trust is stored outside the tap, so untapping is not assumed to drop it.
  if [ -n "${trusted}" ]; then
    brew untrust --formula "${ref}" >/dev/null 2>&1 || true
  fi
  if [ -n "${tapped}" ]; then
    brew untap --force "${tap}" >/dev/null 2>&1 || true
  fi
  if [ -n "${work}" ]; then
    rm -rf "${work}"
  fi
}

die() {
  echo "audit-formula: $*" >&2
  exit 2
}

trap cleanup EXIT

[ -s "${formula}" ] || die "${formula} is missing or empty"
[[ ${name} =~ ^[a-z0-9][a-z0-9@._+-]*$ ]] || die "unexpected formula name '${name}'"

# macOS runners have brew on PATH. Linux runners ship Homebrew under
# /home/linuxbrew/.linuxbrew without putting it on PATH.
if ! command -v brew >/dev/null 2>&1; then
  if [ -x /home/linuxbrew/.linuxbrew/bin/brew ]; then
    eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)"
  else
    die "brew not found: this check needs Homebrew on the runner"
  fi
fi
export HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_ANALYTICS=1 HOMEBREW_NO_ENV_HINTS=1 HOMEBREW_NO_INSTALL_CLEANUP=1

if brew tap | grep -Fxq "${tap}"; then
  die "${tap} is already tapped: untap it first, this check needs its own scratch tap"
fi

# An explicit template, because macOS mktemp ignores TMPDIR without one.
work="$(mktemp -d "${TMPDIR:-/tmp}/audit-formula.XXXXXXXX")"
mkdir "${work}/Formula"
cp "${formula}" "${work}/Formula/${name}.rb"
git -C "${work}" init --quiet
git -C "${work}" add Formula
git -C "${work}" -c user.name=ci -c user.email=ci@example.invalid commit --quiet -m "formula under audit"

brew tap "${tap}" "${work}"
tapped=1

# brew audit and brew style refuse to load a formula from an untrusted tap.
# The narrow form trusts this one formula only.
trusted=1
brew trust --formula "${ref}"

status=0
brew audit --strict "${ref}" || status=1
brew style "${ref}" || status=1
# The last command's status is the script's: 0 when both passed, 1 otherwise.
[ "${status}" -eq 0 ]
