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

cleanup() {
  if [ -n "${worktree_dir}" ] && [ -d "${worktree_dir}" ]; then
    git worktree remove --force "${worktree_dir}"
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
  worktree_dir="$(mktemp -d "${TMPDIR:-/tmp}/publish-release-assets.XXXXXXXX")"
  git worktree add --quiet --detach "${worktree_dir}" "${tag}"

  (
    cd "${worktree_dir}"

    echo "building archives for ${tag}"
    GORELEASER_CURRENT_TAG="${tag}" goreleaser release --clean --skip=announce,validate,publish

    echo "attaching assets to the draft release ${tag}"
    gh release upload "${tag}" dist/*.tar.gz dist/*.zip dist/*checksums.txt --clobber

    echo "publishing ${tag}"
    gh release edit "${tag}" --draft=false
  )

  git worktree remove "${worktree_dir}"
  worktree_dir=""
done <<<"${TAGS}"
