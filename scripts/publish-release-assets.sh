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
# The same run signs the checksums file keyless with cosign (the signs entry
# of .goreleaser.yaml, which needs the job's OIDC token). The script refuses
# to attach or publish anything when the signature bundle is missing or does
# not verify against the identity of release.yaml on the main branch, so a
# failure leaves the draft release as it was, for a rerun.
#
# The formula for the organisation's Homebrew tap is rendered by the same
# goreleaser run (skip_upload, see .goreleaser.yaml) and uploaded as one more
# asset. Pushing it to the tap is publish-homebrew-formula.sh's job, in its
# own release.yaml job after this script published the release.
#
# The winget manifest set (version, installer and default locale file) comes
# out of the same run and is uploaded too. Submitting it to
# microsoft/winget-pkgs is manual (docs/adr/0005-winget-manifest-semi-automatic.md),
# no job here touches that repository. The script refuses an incomplete set
# before it attaches or publishes anything.
#
# TAGS is release.yaml's own "tags" action output: one tag per line, empty
# when release-me released nothing this run.
set -euo pipefail

# The manifest check below runs from this checkout, not from the tag's
# worktree, and the build subshell changes directory, so the path is resolved
# once, absolute.
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ -z "${TAGS:-}" ]; then
  echo "nothing released, nothing to attach"
  exit 0
fi

# The signature check below builds its identity from the repository, which
# the workflow always sets. Without it the script cannot verify, so it stops.
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY must name the repository the release belongs to}"

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
    # goreleaser always writes a version line, which Homebrew's audit flags as
    # redundant with the version in the URLs. The step removes it, and only
    # when the URLs carry the same version. It runs before the hash below is
    # taken, so the file that is hashed, uploaded and later verified and
    # pushed is the stripped one. See docs/adr/0003-homebrew-formula-not-cask.md.
    #
    # Only a plain vMAJOR.MINOR.PATCH tag is stripped. For a tag with a
    # pre-release or build part, Homebrew would scan another version out of
    # the URLs than the line declares, so removing it would change what the
    # formula installs. That formula keeps its version line as rendered. It is
    # attached like any other, and publish-homebrew-formula.sh never pushes
    # it: a pre-release is skipped there and any other such tag is refused.
    if [[ ${tag} =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      echo "removing the redundant version line from the formula of ${tag}"
      if ! "${script_dir}/strip-formula-version.sh" "${formula_files[0]}"; then
        echo "the formula of ${tag} cannot be stripped of its version line. Nothing was attached or published, the draft release stays." >&2
        exit 1
      fi
    else
      echo "${tag} is not a plain release tag, the formula keeps its version line and is never pushed"
    fi
    # The winget manifest set of the same run (the winget entry of
    # .goreleaser.yaml, skip_upload): one version directory with the three
    # files winget-pkgs wants. An incomplete set stops the script here, before
    # anything is attached or published. The glob is unquoted on purpose, an
    # unmatched one stays literal and fails the -d.
    winget_dirs=(dist/winget/manifests/o/OstendorfCloud/gitcd/*/)
    if [ "${#winget_dirs[@]}" -ne 1 ] || [ ! -d "${winget_dirs[0]}" ]; then
      echo "expected exactly one version directory under dist/winget/manifests/o/OstendorfCloud/gitcd for ${tag}" >&2
      exit 1
    fi
    winget_dir="${winget_dirs[0]%/}"
    winget_files=(
      "${winget_dir}/OstendorfCloud.gitcd.yaml"
      "${winget_dir}/OstendorfCloud.gitcd.installer.yaml"
      "${winget_dir}/OstendorfCloud.gitcd.locale.en-US.yaml"
    )
    for winget_file in "${winget_files[@]}"; do
      if [ ! -s "${winget_file}" ]; then
        echo "the winget manifest file ${winget_file} for ${tag} is missing or empty" >&2
        exit 1
      fi
    done
    winget_entries="$(find "${winget_dir}" -mindepth 1 | wc -l | tr -d ' ')"
    if [ "${winget_entries}" -ne 3 ]; then
      echo "the winget manifest directory ${winget_dir} for ${tag} must hold exactly three files, has ${winget_entries} entries" >&2
      exit 1
    fi
    # goreleaser signed the checksums file in the same run (the signs entry
    # of .goreleaser.yaml, cosign keyless). A failed signing step already
    # failed the build above. This check covers the case where it ran but
    # left no usable bundle, so a release is never published unsigned. The
    # globs are unquoted on purpose, an unmatched one stays literal and fails
    # the -f.
    signature_files=(dist/*checksums.txt.sigstore.json)
    if [ "${#signature_files[@]}" -ne 1 ] || [ ! -f "${signature_files[0]}" ] || [ ! -s "${signature_files[0]}" ]; then
      echo "expected exactly one non-empty checksums signature bundle in dist for ${tag}" >&2
      exit 1
    fi
    signature_file="${signature_files[0]}"
    checksums_file="${signature_file%.sigstore.json}"
    if [ ! -s "${checksums_file}" ]; then
      echo "the checksums file ${checksums_file} for ${tag} is missing or empty" >&2
      exit 1
    fi

    # The winget manifest against the tag of this run and the checksums file
    # of this run, not against values read out of the manifest itself: every
    # line must be part of the generated template, every URL this tag's
    # release download and every sha256 the checksums file's entry. Before
    # the signature check and any upload, so a failure leaves the draft as it
    # was. The files are not signed, this check is what stands in for it.
    echo "checking the winget manifest for ${tag}"
    if ! "${script_dir}/check-winget-manifest.sh" "${winget_dir}" "${tag}" "${checksums_file}"; then
      echo "the winget manifest for ${tag} failed its check. Nothing was attached or published, the draft release stays." >&2
      exit 1
    fi

    # Verify the bundle against the identity docs/install.md tells readers to
    # use, before anything is attached or published. The identity is built
    # from the repository and the fixed workflow path and branch, never from
    # GITHUB_WORKFLOW_REF: that would accept whatever identity this very run
    # signed with. A run from another ref, such as the pull_request label
    # run, signs as refs/pull/<n>/merge, fails here, and leaves the draft
    # release as it is. Re-run the failed push run then.
    #
    # There is no switch for this step. cosign is looked up on PATH like
    # every tool here, so a test puts a fake first on PATH, and no variable
    # can turn the check off or point it elsewhere.
    echo "verifying the signature of ${checksums_file} for ${tag}"
    if ! cosign verify-blob --bundle "${signature_file}" \
      --certificate-identity "https://github.com/${GITHUB_REPOSITORY}/.github/workflows/release.yaml@refs/heads/main" \
      --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
      "${checksums_file}"; then
      echo "the signature of ${tag} does not verify against the release workflow identity on the main branch." >&2
      echo "Nothing was attached or published, the draft release stays. Re-run the failed push run, not a label run." >&2
      exit 1
    fi

    shasum -a 256 "${formula_files[0]}" | cut -d' ' -f1 >"${worktree_dir}.formula-hash"

    echo "attaching assets to the draft release ${tag}"
    # The Homebrew formula goreleaser rendered in this same run rides along
    # as an asset: its checksums are those of the archives uploaded here, so
    # publish-homebrew-formula.sh can push it to the tap later, from its own
    # job, without rebuilding anything. The signature bundle of the checksums
    # file rides along too, and so do the three winget manifest files: their
    # installer checksums are those of the archives uploaded here, and the
    # owner submits them to microsoft/winget-pkgs by hand, see docs/install.md.
    # Nothing here pushes to that repository.
    gh release upload "${tag}" dist/*.tar.gz dist/*.zip "${checksums_file}" "${signature_file}" dist/homebrew/Formula/*.rb \
      "${winget_files[@]}" --clobber

    echo "publishing ${tag}"
    gh release edit "${tag}" --draft=false
  )

  collect_hash
  git worktree remove "${worktree_dir}"
  worktree_dir=""
done <<<"${TAGS}"
