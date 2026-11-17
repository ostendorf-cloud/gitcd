#!/usr/bin/env bash
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
#
# Pushes the Homebrew formula of each tag in TAGS to the organisation's tap.
#
# publish-release-assets.sh already rendered the formula in the same
# goreleaser run that built the archives, so its URLs and checksums match
# the published assets, attached it to the release as FORMULA_ASSET and
# recorded its sha256 per tag. This script runs afterwards, in its own job,
# and only reads that asset back: it never rebuilds anything, because a
# rebuild would not reproduce the archives' checksums.
#
# Order, and why it is safe to run again:
#   1. the release must be published, so the formula's URLs resolve, and
#      must not be a pre-release, by the release's own flags;
#   2. the formula is downloaded from the release;
#   3. the formula is verified against the build, see VERIFY_MODE;
#   4. the formula's lines must all be part of the generated template, see
#      check-homebrew-formula.sh, and its version, read from its URLs since
#      the formula has no version line, must be the tag's;
#   5. the tap's current file is compared: an identical one is left alone,
#      a newer version in the tap is refused, otherwise one commit is made
#      through the contents API.
# A rerun for the same tag finds the identical file and changes nothing.
#
# VERIFY_MODE selects what the downloaded formula is verified against:
#   hash      (default) the sha256 FORMULA_HASHES records for the tag, taken
#             from the formula the release job generated. Rests on the
#             integrity of the workflow run's own outputs.
#   checksums every sha256 in the formula must equal the matching line of
#             the release's own checksums file. Used for a manual run after
#             the outputs are gone. Rests on trust in the release's assets
#             as they are published now: it proves the formula agrees with
#             them, not that they are the ones the build produced.
#
# TAGS is release.yaml's own "tags" output, one tag per line.
# FORMULA_HASHES is its "formula-hashes" output, lines of "<tag> <sha256>".
# ALLOW_DOWNGRADE=true lets a tag older than the tap's formula through.
# GH_TOKEN reads this repository's release, TAP_TOKEN writes the tap only.
# Neither is ever printed.
set -euo pipefail

if [ -z "${TAGS:-}" ]; then
  echo "nothing released, no formula to push"
  exit 0
fi

: "${TAP_TOKEN:?TAP_TOKEN is required}"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tap_repository="${TAP_REPOSITORY:-ostendorf-cloud/homebrew-tap}"
tap_branch="${TAP_BRANCH:-main}"
formula_asset="${FORMULA_ASSET:-gitcd.rb}"
formula_path="${FORMULA_PATH:-Formula/gitcd.rb}"
verify_mode="${VERIFY_MODE:-hash}"
allow_downgrade="${ALLOW_DOWNGRADE:-false}"

case "${verify_mode}" in
hash | checksums) ;;
*)
  echo "unknown VERIFY_MODE ${verify_mode}, want hash or checksums" >&2
  exit 1
  ;;
esac

work_dir=""

cleanup() {
  if [ -n "${work_dir}" ] && [ -d "${work_dir}" ]; then
    rm -rf "${work_dir}"
  fi
}
trap cleanup EXIT

# An explicit template, because macOS mktemp ignores TMPDIR without one.
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/publish-homebrew-formula.XXXXXXXX")"

# tap_gh runs gh with the tap token only for the call that needs it.
tap_gh() {
  GH_TOKEN="${TAP_TOKEN}" gh "$@"
}

# formula_version <file> prints the version a formula installs. A formula
# without a version line, which is every formula this repository publishes
# since the release strips it, has it in the download URL:
# .../releases/download/v<version>/<archive>. An older formula in the tap still
# carries a version line, which wins. Prints nothing when neither exists.
formula_version() {
  local declared
  declared="$(sed -n -E 's/^  version "([^"]*)"$/\1/p' "$1" | head -n 1)"
  if [ -n "${declared}" ]; then
    printf '%s\n' "${declared}"
    return 0
  fi
  sed -n -E 's|^      url "https://github\.com/[^/"]+/[^/"]+/releases/download/v([^/"]+)/[^/"]+"$|\1|p' "$1" | head -n 1
}

endpoint="repos/${tap_repository}/contents/${formula_path}"
tag_re='^v[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.+-]+)?$'

while IFS= read -r tag; do
  if [ -z "${tag}" ]; then
    continue
  fi

  if ! [[ ${tag} =~ ${tag_re} ]]; then
    echo "${tag} is not a release tag of the form vMAJOR.MINOR.PATCH" >&2
    exit 1
  fi

  # The release's own flags decide, not the tag's spelling: a hyphen in
  # build metadata does not make a pre-release.
  if ! flags="$(gh release view "${tag}" --json isDraft,isPrerelease --jq '"\(.isDraft) \(.isPrerelease)"')"; then
    echo "cannot read the release for ${tag}: it is missing, still a draft or not visible to the token" >&2
    exit 1
  fi
  read -r draft prerelease <<<"${flags}"
  if [ "${draft}" != "false" ]; then
    echo "${tag} is a draft, the formula's URLs would not resolve" >&2
    exit 1
  fi
  if [ "${prerelease}" = "true" ]; then
    echo "${tag} is a pre-release, no formula pushed"
    continue
  fi
  # The release strips the version line of a plain vMAJOR.MINOR.PATCH tag
  # only. A tag with a build part that the release does not flag as a
  # pre-release would carry a formula with its version line, which the
  # skeleton check below refuses anyway, and whose URL Homebrew would scan
  # another version out of. Say so here instead of with a skeleton diff.
  if ! [[ ${tag} =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "${tag} is not a plain vMAJOR.MINOR.PATCH tag: its formula keeps a version line and is never pushed, nothing pushed" >&2
    exit 1
  fi

  formula_dir="${work_dir}/${tag}"
  mkdir -p "${formula_dir}"
  if ! gh release download "${tag}" --pattern "${formula_asset}" --dir "${formula_dir}" --clobber; then
    echo "cannot download ${formula_asset} from the release ${tag}" >&2
    exit 1
  fi
  formula="${formula_dir}/${formula_asset}"

  if [ "${verify_mode}" = "hash" ]; then
    want_hash="$(awk -v t="${tag}" '$1 == t { print $2 }' <<<"${FORMULA_HASHES:-}")"
    if [ -z "${want_hash}" ]; then
      echo "no formula hash recorded for ${tag}: rerun the failed jobs of the release run, or use the manual run with the checksums mode" >&2
      exit 1
    fi
    got_hash="$(shasum -a 256 "${formula}" | cut -d' ' -f1)"
    if [ "${got_hash}" != "${want_hash}" ]; then
      echo "${formula_asset} of ${tag} differs from the formula the release job generated, nothing pushed" >&2
      exit 1
    fi
  else
    checksums="${formula_dir}/checksums.txt"
    mkdir -p "${formula_dir}/sums"
    if ! gh release download "${tag}" --pattern "*_checksums.txt" --dir "${formula_dir}/sums" --clobber; then
      echo "cannot download the checksums file from the release ${tag}" >&2
      exit 1
    fi
    cat "${formula_dir}"/sums/*_checksums.txt >"${checksums}"
    # Pairs of "<archive name> <sha256>", straight from the formula.
    pairs="$(awk '/^      url "/ { n = $2; gsub(/"/, "", n); sub(".*/", "", n) } /^      sha256 "/ { h = $2; gsub(/"/, "", h); print n, h }' "${formula}")"
    if [ -z "${pairs}" ]; then
      echo "no archive checksums found in ${formula_asset} of ${tag}" >&2
      exit 1
    fi
    while read -r name hash; do
      want="$(awk -v n="${name}" '$2 == n { print $1 }' "${checksums}")"
      if [ -z "${want}" ] || [ "${want}" != "${hash}" ]; then
        echo "the checksum of ${name} in ${formula_asset} is not the one the release lists, nothing pushed" >&2
        exit 1
      fi
    done <<<"${pairs}"
  fi

  if ! "${script_dir}/check-homebrew-formula.sh" "${formula}" "${tag}"; then
    echo "${formula_asset} of ${tag} does not match the generated formula, nothing pushed" >&2
    exit 1
  fi
  # The formula has no version line, so the version it installs is the one in
  # its URLs. check-homebrew-formula.sh above already requires every URL to be
  # a download of this very tag, this states the same fact for the reader.
  asset_version="$(formula_version "${formula}")"
  if [ "${asset_version}" != "${tag#v}" ]; then
    echo "${formula_asset} carries the version '${asset_version}', not ${tag#v}" >&2
    exit 1
  fi

  # Read the current file first. Only a 404 means "no formula yet", any
  # other failure (token, 5xx, rate limit) must stop here instead of
  # being mistaken for a missing file.
  sha=""
  if sha="$(tap_gh api "${endpoint}?ref=${tap_branch}" --jq .sha 2>"${work_dir}/api-error")"; then
    current="${work_dir}/current.rb"
    if ! tap_gh api -H "Accept: application/vnd.github.raw+json" "${endpoint}?ref=${tap_branch}" >"${current}"; then
      echo "cannot read ${formula_path} from ${tap_repository}" >&2
      exit 1
    fi
    if cmp -s "${current}" "${formula}"; then
      echo "${formula_path} in ${tap_repository} is already up to date for ${tag}"
      continue
    fi

    # Never put an older formula over a newer one. Only the manual run can
    # override this, with its explicit input.
    #
    # sort -V is not semver: it ranks 1.0.0+build and 1.0.0-rc.1 above 1.0.0.
    # That does not matter here, since a pre-release never reaches the tap
    # (the release's own flag skips it above) and release-me emits no build
    # metadata. The test "sort -V is not semver" documents the behaviour.
    tap_version="$(formula_version "${current}")"
    if [ -z "${tap_version}" ]; then
      echo "cannot read the version of ${formula_path} in ${tap_repository}" >&2
      exit 1
    fi
    newest="$(printf '%s\n%s\n' "${tap_version}" "${tag#v}" | sort -V | tail -n 1)"
    if [ "${tap_version}" != "${tag#v}" ] && [ "${newest}" = "${tap_version}" ] && [ "${allow_downgrade}" != "true" ]; then
      echo "the tap holds ${tap_version}, newer than ${tag#v}: refusing to put an older formula back" >&2
      exit 1
    fi
  elif grep -q "HTTP 404" "${work_dir}/api-error"; then
    sha=""
  else
    echo "cannot read ${formula_path} from ${tap_repository}" >&2
    exit 1
  fi

  echo "pushing ${formula_path} for ${tag} to ${tap_repository}"
  put_args=(
    -X PUT "${endpoint}"
    -f "message=gitcd ${tag#v}"
    -f "branch=${tap_branch}"
    -f "content=$(base64 <"${formula}" | tr -d '\n')"
  )
  if [ -n "${sha}" ]; then
    put_args+=(-f "sha=${sha}")
  fi
  if ! tap_gh api "${put_args[@]}" --silent; then
    echo "cannot push ${formula_path} for ${tag} to ${tap_repository}: the release is public without a formula, rerun the failed jobs" >&2
    exit 1
  fi
done <<<"${TAGS}"
