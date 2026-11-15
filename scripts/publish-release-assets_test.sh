#!/usr/bin/env bash
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
# Unit tests for publish-release-assets.sh. Fake git, goreleaser, gh and
# mktemp binaries stand in for the real tools, logging every call they
# receive, so these run offline and never touch a real repository or
# release.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="${script_dir}/publish-release-assets.sh"
# An explicit template, because macOS mktemp ignores TMPDIR without one
# and always lands in the system temporary directory.
work="$(mktemp -d "${TMPDIR:-/tmp}/publish-release-assets.XXXXXXXX")"
trap 'rm -rf "${work}"' EXIT

fake_bin="${work}/fake-bin"
mkdir -p "${fake_bin}"

# fake-bin/git logs the call. publish-release-assets.sh reads no output
# from git at all, so nothing here has to answer with anything.
# "worktree add" and "worktree remove" actually create and delete the
# directory they are given, like the real git worktree would for the
# script's own "cd" into it and later cleanup. Unlike real git, this fake
# is not a general purpose git emulator, but for these two subcommands it
# validates flags the same way real git does: "worktree add" only accepts
# "--quiet" and "--detach", "worktree remove" only accepts "--force", and
# anything else fails loudly with a message naming the flag, the same
# shape as real git's own "error: unknown option" for a flag a subcommand
# does not support. That is what caught "--quiet" on "worktree remove"
# here, a flag real git worktree remove has never supported; "fetch" needs
# no output at all.
cat <<'FAKE_GIT' >"${fake_bin}/git"
#!/usr/bin/env bash
echo "git $*" >>"${CALL_LOG}"

worktree_target_dir() {
  local dir=""
  for arg in "$@"; do
    case "${arg}" in
    -*) ;;
    *)
      if [ -z "${dir}" ]; then
        dir="${arg}"
      fi
      ;;
    esac
  done
  echo "${dir}"
}

validate_worktree_flags() {
  local subcommand="$1" allowed="$2"
  shift 2
  local arg
  for arg in "$@"; do
    case "${arg}" in
    -*)
      case " ${allowed} " in
      *" ${arg} "*) ;;
      *)
        echo "fake git: worktree ${subcommand}: unknown option \`${arg#--}'" >&2
        exit 1
        ;;
      esac
      ;;
    esac
  done
}

case "$1 $2" in
"worktree add")
  shift 2
  validate_worktree_flags "add" "--quiet --detach" "$@"
  mkdir -p "$(worktree_target_dir "$@")"
  exit 0
  ;;
"worktree remove")
  shift 2
  validate_worktree_flags "remove" "--force" "$@"
  rm -rf "$(worktree_target_dir "$@")"
  exit 0
  ;;
esac
FAKE_GIT
chmod +x "${fake_bin}/git"

# fake-bin/mktemp logs nothing: publish-release-assets.sh only ever calls
# it to name one tag's worktree directory, and that call itself is not
# part of what a test asserts about. It ignores the template it is given
# and returns a deterministic, numbered directory under the test's own
# work directory instead, so a test can name the exact path a worktree
# add or remove call used.
# The counter file is reset at the start of every run_case,
# run_failure_case and run_partial_failure_case, so every case starts
# counting again from "worktree-1".
cat <<'FAKE_MKTEMP' >"${fake_bin}/mktemp"
#!/usr/bin/env bash
base="$(dirname "${CALL_LOG}")"
counter_file="${base}/mktemp-counter"
n=0
if [ -f "${counter_file}" ]; then
  n="$(cat "${counter_file}")"
fi
n=$((n + 1))
echo "${n}" >"${counter_file}"
dir="${base}/worktree-${n}"
mkdir -p "${dir}"
echo "${dir}"
FAKE_MKTEMP
chmod +x "${fake_bin}/mktemp"

# fake-bin/goreleaser logs the call and drops the archives and checksum
# file publish-release-assets.sh globs for, named like the real
# .goreleaser.yaml would for a build of "fixture" at version 1.0.0, into
# its current directory, the worktree the real script "cd"s into first:
# one tar.gz and one zip, matching gitcd's own per-platform format split.
# GORELEASER_CURRENT_TAG naming a tag ending "-goreleaser-fails" fails the
# build instead, to test that a failure partway through one tag's build
# still removes that tag's worktree.
cat <<'FAKE_GORELEASER' >"${fake_bin}/goreleaser"
#!/usr/bin/env bash
echo "goreleaser $*" >>"${CALL_LOG}"
case "${GORELEASER_CURRENT_TAG:-}" in
*-goreleaser-fails)
  echo "fake goreleaser: build failed" >&2
  exit 1
  ;;
esac
mkdir -p dist
echo archive >dist/fixture_linux_amd64.tar.gz
echo archive >dist/fixture_windows_amd64.zip
echo checksum >dist/fixture_1.0.0_checksums.txt
mkdir -p dist/homebrew/Formula
echo formula >dist/homebrew/Formula/fixture.rb
FAKE_GORELEASER
chmod +x "${fake_bin}/goreleaser"

# fake-bin/gh logs every call. "release view" answers isDraft from the tag
# name itself: a tag containing "-draft" or "-published" need no separate
# state file; containing them, rather than ending with them, lets a tag
# also carry a second marker, such as "-goreleaser-fails", for a fake
# further down the pipeline to react to. "-view-fails" simulates a release
# GitHub cannot read back, a deleted release, an expired token, a 5xx or a
# rate limit alike. Anything else it does not recognize fails loudly, to
# catch a call the script was not supposed to make.
cat <<'FAKE_GH' >"${fake_bin}/gh"
#!/usr/bin/env bash
echo "gh $*" >>"${CALL_LOG}"
if [ "$1 $2" = "release view" ]; then
  case "$3" in
  *-draft*) echo true ;;
  *-published*) echo false ;;
  *-view-fails*) echo "fake gh: release view failed" >&2; exit 1 ;;
  *) echo "fake gh: unknown tag $3" >&2; exit 1 ;;
  esac
  exit 0
fi
case "$1 $2" in
"release upload")
  case "$3" in
  *-upload-fails*) echo "fake gh: upload failed" >&2; exit 1 ;;
  esac
  exit 0
  ;;
"release edit") exit 0 ;;
*) echo "fake gh: unexpected call: $*" >&2; exit 1 ;;
esac
FAKE_GH
chmod +x "${fake_bin}/gh"

export CALL_LOG
export PATH="${fake_bin}:${PATH}"

failures=0

# reset_case_state truncates the call log and clears every piece of state
# a previous case could have left behind: the mktemp counter, so worktree
# directory numbering restarts at 1, any worktree directory a previous
# case's script run did not clean up itself, and the build directory the
# real script runs from, since it drops "dist" into its current directory
# and must never see a previous case's leftovers.
reset_case_state() {
  CALL_LOG="${work}/calls.log"
  : >"${CALL_LOG}"
  rm -f "${work}/mktemp-counter"
  rm -rf "${work}"/worktree-*

  local build_dir="${work}/build"
  rm -rf "${build_dir}"
  mkdir -p "${build_dir}"

  export GITHUB_OUTPUT="${work}/github-output"
  : >"${GITHUB_OUTPUT}"
}

# check_formula_hashes name want asserts the formula-hashes step output the
# last case wrote, the input the homebrew job verifies the formula asset
# against. want is the whole block, "" for none.
check_formula_hashes() {
  local case_name="$1" want="$2" got
  got="$(cat "${GITHUB_OUTPUT}")"
  if [ "${got}" != "${want}" ]; then
    echo "FAIL ${case_name}: output = ${got}, want ${want}"
    failures=$((failures + 1))
    return
  fi
  echo "PASS ${case_name}"
}

# run_script runs the script with TAGS=tags from a fresh build directory,
# capturing stdout, stderr and the exit status for the caller to assert on.
run_script() {
  local tags="$1"
  local build_dir="${work}/build"
  if (cd "${build_dir}" && TAGS="${tags}" "${script}") >"${work}/stdout" 2>"${work}/stderr"; then
    echo 0
  else
    echo "$?"
  fi
}

# run_case name tags want_calls
run_case() {
  local case_name="$1" tags="$2" want_calls="$3"
  reset_case_state

  local status
  status="$(run_script "${tags}")"
  if [ "${status}" -ne 0 ]; then
    echo "FAIL ${case_name}: exit status = ${status}, want 0"
    echo "stderr: $(cat "${work}/stderr")"
    failures=$((failures + 1))
    return
  fi

  local got_calls
  got_calls="$(cat "${CALL_LOG}")"
  if [ "${got_calls}" != "${want_calls}" ]; then
    echo "FAIL ${case_name}: calls = ${got_calls@Q}, want ${want_calls@Q}"
    failures=$((failures + 1))
    return
  fi

  echo "PASS ${case_name}"
}

# run_failure_case name tags want_stderr_substring asserts that a failure
# reading the release stops the script before it ever builds, uploads or
# publishes anything, and before it ever creates a worktree: treating an
# unread release as "nothing to do" would otherwise leave a draft with no
# assets and a workflow run reporting a false success.
run_failure_case() {
  local case_name="$1" tags="$2" want_stderr_substring="$3"
  reset_case_state

  local status
  status="$(run_script "${tags}")"
  if [ "${status}" -eq 0 ]; then
    echo "FAIL ${case_name}: exit status = 0, want non-zero"
    failures=$((failures + 1))
    return
  fi

  if ! grep -qF "${want_stderr_substring}" "${work}/stderr"; then
    echo "FAIL ${case_name}: stderr = $(cat "${work}/stderr"), want it to contain ${want_stderr_substring@Q}"
    failures=$((failures + 1))
    return
  fi

  local got_calls
  got_calls="$(cat "${CALL_LOG}")"
  if printf '%s' "${got_calls}" | grep -qE "worktree add|goreleaser|release upload|release edit"; then
    echo "FAIL ${case_name}: calls = ${got_calls@Q}, want it to stop before building, uploading or publishing"
    failures=$((failures + 1))
    return
  fi

  echo "PASS ${case_name}"
}

# run_partial_failure_case name tags want_stderr_substring asserts the
# opposite side of the same cleanup: a failure that happens after a
# worktree already exists, partway through one tag's build, upload or
# publish, must still leave no worktree behind, neither as a git worktree
# nor as a directory on disk, the trap covering both the "normal" and the
# "killed by set -e" exit path alike.
run_partial_failure_case() {
  local case_name="$1" tags="$2" want_stderr_substring="$3"
  reset_case_state

  local status
  status="$(run_script "${tags}")"
  if [ "${status}" -eq 0 ]; then
    echo "FAIL ${case_name}: exit status = 0, want non-zero"
    failures=$((failures + 1))
    return
  fi

  if ! grep -qF "${want_stderr_substring}" "${work}/stderr"; then
    echo "FAIL ${case_name}: stderr = $(cat "${work}/stderr"), want it to contain ${want_stderr_substring@Q}"
    failures=$((failures + 1))
    return
  fi

  if ! grep -qF "git worktree remove --force ${work}/worktree-1" "${CALL_LOG}"; then
    echo "FAIL ${case_name}: calls = $(cat "${CALL_LOG}"), want the trap to still remove the worktree"
    failures=$((failures + 1))
    return
  fi

  if find "${work}" -maxdepth 1 -name 'worktree-*' | grep -q .; then
    echo "FAIL ${case_name}: a worktree directory is still on disk after the failure"
    failures=$((failures + 1))
    return
  fi

  echo "PASS ${case_name}"
}

run_case "nothing released makes no call at all" "" ""

run_case "a draft release is built, attached and published" "v1.0.0-draft" \
  "git fetch origin --tags --force
gh release view v1.0.0-draft --json isDraft --jq .isDraft
git worktree add --quiet --detach ${work}/worktree-1 v1.0.0-draft
goreleaser release --clean --skip=announce,validate,publish
gh release upload v1.0.0-draft dist/fixture_linux_amd64.tar.gz dist/fixture_windows_amd64.zip dist/fixture_1.0.0_checksums.txt dist/homebrew/Formula/fixture.rb --clobber
gh release edit v1.0.0-draft --draft=false
git worktree remove ${work}/worktree-1"
formula_hash="$(printf 'formula\n' | shasum -a 256 | cut -d' ' -f1)"
check_formula_hashes "the formula hash of the build is a step output" \
  "formula-hashes<<FORMULA_HASHES_EOF
v1.0.0-draft ${formula_hash}
FORMULA_HASHES_EOF"

run_case "an already published release is left alone" "v1.0.0-published" \
  "git fetch origin --tags --force
gh release view v1.0.0-published --json isDraft --jq .isDraft"

run_case "every tag in a multi-line TAGS is handled" "v1.0.0-draft
v2.0.0-published" \
  "git fetch origin --tags --force
gh release view v1.0.0-draft --json isDraft --jq .isDraft
git worktree add --quiet --detach ${work}/worktree-1 v1.0.0-draft
goreleaser release --clean --skip=announce,validate,publish
gh release upload v1.0.0-draft dist/fixture_linux_amd64.tar.gz dist/fixture_windows_amd64.zip dist/fixture_1.0.0_checksums.txt dist/homebrew/Formula/fixture.rb --clobber
gh release edit v1.0.0-draft --draft=false
git worktree remove ${work}/worktree-1
gh release view v2.0.0-published --json isDraft --jq .isDraft"
check_formula_hashes "only a build that ran records a hash" \
  "formula-hashes<<FORMULA_HASHES_EOF
v1.0.0-draft ${formula_hash}
FORMULA_HASHES_EOF"

run_failure_case \
  "a gh release view failure stops the script instead of being treated as already published" \
  "v1.0.0-view-fails" "cannot read the release for v1.0.0-view-fails"

run_partial_failure_case \
  "a goreleaser failure partway through a build still removes the worktree" \
  "v1.0.0-draft-goreleaser-fails" "fake goreleaser: build failed"
check_formula_hashes "a build that failed records no hash" ""

run_partial_failure_case \
  "an upload failure still removes the worktree" \
  "v1.0.0-draft-upload-fails" "fake gh: upload failed"
check_formula_hashes "a failure after the build still hands over the hash" \
  "formula-hashes<<FORMULA_HASHES_EOF
v1.0.0-draft-upload-fails ${formula_hash}
FORMULA_HASHES_EOF"

if [ "${failures}" -ne 0 ]; then
  echo "${failures} test(s) failed"
  exit 1
fi

echo "all tests passed"
