#!/usr/bin/env bash
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
#
# Builds the archives and checksums goreleaser already produces for this
# project, attaches them to the draft release release-me just created for
# each tag in TAGS, then publishes it.
#
# goreleaser never touches the GitHub release itself here (--skip=publish):
# its own lookup of an existing release by tag cannot see a draft one
# ("get a release by tag name" excludes drafts), per release-me's own
# docs/adr/0001-draft-releases-until-assets-attach.md. Every change to the
# release instead goes through gh, which does resolve a draft: "gh release
# upload" to attach assets, "gh release edit --draft=false" to publish.
#
# The formula for the organisation's Homebrew tap is rendered by the same
# goreleaser run (skip_upload, see .goreleaser.yaml) and uploaded as one more
# asset. Pushing it to the tap is publish-homebrew-formula.sh's job, in its
# own release.yaml job after this script published the release.
#
# TAGS is release.yaml's own "tags" action output: one tag per line, empty
# when release-me released nothing this run.
set -euo pipefail

if [ -z "${TAGS:-}" ]; then
  echo "nothing released, nothing to attach"
  exit 0
fi

git fetch origin --tags --force

# Every tag gets built in a dedicated git worktree, never by detaching the
# job's own checkout in place: the job's checkout may sit on the base
# branch, not necessarily the merge commit release-me tagged, in
# particular on the pull_request labeled/unlabeled trigger that checks out
# the base branch. Detaching HEAD in place would rewrite this very script
# and .goreleaser.yaml out from under the still-executing shell, since
# both live in the checkout a detaching "git checkout" would switch to a
# different commit of; a separate worktree leaves the running checkout,
# and this script, untouched.
#
# worktree_dir is removed by the trap below on every exit path, success or
# failure: a plain "remove it at the end of the script" line would never
# run once "set -euo pipefail" or an explicit "exit 1" stops the script
# early, for a tag whose release view, build, upload or publish step
# failed.
worktree_dir=""
# "<tag> <sha256>" lines of the formula each build generated, handed to the
# homebrew job as the formula-hashes output so it can verify the asset it
# downloads later against the file this run generated. Written by the trap
# too, so a failure after a build still leaves the hashes of the builds done.
hashes=""

current_tag=""

# collect_hash moves the hash file the build subshell left for the current
# tag into hashes. Idempotent: the file is gone afterwards.
collect_hash() {
  if [ -n "${worktree_dir}" ] && [ -f "${worktree_dir}.formula-hash" ]; then
    hashes="${hashes}${current_tag} $(cat "${worktree_dir}.formula-hash")"$'\n'
    rm -f "${worktree_dir}.formula-hash"
  fi
}

cleanup() {
  collect_hash
  if [ -n "${worktree_dir}" ]; then
    if [ -d "${worktree_dir}" ]; then
      git worktree remove --force "${worktree_dir}"
    fi
  fi
  if [ -n "${GITHUB_OUTPUT:-}" ] && [ -n "${hashes}" ]; then
    {
      echo "formula-hashes<<FORMULA_HASHES_EOF"
      printf '%s' "${hashes}"
      echo "FORMULA_HASHES_EOF"
    } >>"${GITHUB_OUTPUT}"
  fi
}
trap cleanup EXIT

while IFS= read -r tag; do
  if [ -z "${tag}" ]; then
    continue
  fi

  # A failing command substitution never trips "set -e" inside an "if" or
  # "while" condition, a well known shell pitfall: that exemption would
  # swallow a failure here too, treating a deleted release, an expired
  # token, a 5xx or a rate limit the same as "already published", and
  # exiting 0 having attached nothing. Reading the status into a variable
  # first, on its own statement, keeps "set -e" in effect.
  if ! draft="$(gh release view "${tag}" --json isDraft --jq .isDraft)"; then
    echo "cannot read the release for ${tag}" >&2
    exit 1
  fi
  case "${draft}" in
  true) ;;
  false)
    echo "${tag} is already published, assets must already be attached"
    continue
    ;;
  *)
    echo "unexpected isDraft value for ${tag}: ${draft}" >&2
    exit 1
    ;;
  esac

  echo "building ${tag} in its own worktree"
  # An explicit template, because macOS mktemp ignores TMPDIR without one
  # and always lands in the system temporary directory.
  current_tag="${tag}"
  worktree_dir="$(mktemp -d "${TMPDIR:-/tmp}/publish-release-assets.XXXXXXXX")"
  git worktree add --quiet --detach "${worktree_dir}" "${tag}"

  (
    cd "${worktree_dir}"

    echo "building archives for ${tag}"
    GORELEASER_CURRENT_TAG="${tag}" goreleaser release --clean --skip=announce,validate,publish

    formula_files=(dist/homebrew/Formula/*.rb)
    if [ "${#formula_files[@]}" -ne 1 ] || [ ! -f "${formula_files[0]}" ]; then
      echo "expected exactly one formula in dist/homebrew/Formula for ${tag}" >&2
      exit 1
    fi
    shasum -a 256 "${formula_files[0]}" | cut -d' ' -f1 >"${worktree_dir}.formula-hash"

    echo "attaching assets to the draft release ${tag}"
    # The Homebrew formula goreleaser rendered in this same run rides along
    # as an asset: its checksums are those of the archives uploaded here, so
    # publish-homebrew-formula.sh can push it to the tap later, from its own
    # job, without rebuilding anything.
    gh release upload "${tag}" dist/*.tar.gz dist/*.zip dist/*checksums.txt dist/homebrew/Formula/*.rb --clobber

    echo "publishing ${tag}"
    gh release edit "${tag}" --draft=false
  )

  collect_hash
  git worktree remove "${worktree_dir}"
  worktree_dir=""
done <<<"${TAGS}"
