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
finished=no
# An old bash can stop a script on an expansion it does not know and still
# exit 0, so a run that did not reach its last line fails here.
trap 'rm -rf "${work}"; if [ "${finished}" != yes ]; then if [ "${failures:-0}" -eq 0 ]; then echo "test script stopped before its last line" >&2; fi; exit 1; fi' EXIT

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
# The two Windows entries the winget manifest is checked against.
echo "1111111111111111111111111111111111111111111111111111111111111111  gitcd_windows_amd64.zip" >dist/fixture_1.0.0_checksums.txt
echo "2222222222222222222222222222222222222222222222222222222222222222  gitcd_windows_arm64.zip" >>dist/fixture_1.0.0_checksums.txt
# The signs entry of .goreleaser.yaml signs the checksums file. A tag
# containing "-unsigned" simulates a run that left no bundle,
# "-empty-bundle" an empty one, "-two-bundles" a second one, and
# "-verify-fails" a bundle the verifier rejects (the fake cosign reads the
# marker file).
case "${GORELEASER_CURRENT_TAG:-}" in
*-unsigned*) ;;
*-empty-bundle*) : >dist/fixture_1.0.0_checksums.txt.sigstore.json ;;
*) echo bundle >dist/fixture_1.0.0_checksums.txt.sigstore.json ;;
esac
case "${GORELEASER_CURRENT_TAG:-}" in
*-empty-checksums*) : >dist/fixture_1.0.0_checksums.txt ;;
*-extra-checksums*) echo checksum >dist/other_checksums.txt ;;
*-two-bundles*) echo bundle >dist/other_checksums.txt.sigstore.json ;;
*-verify-fails*) : >dist/verify-fails ;;
esac
mkdir -p dist/homebrew/Formula
# The formula carries the version line goreleaser always writes, with the
# version and the URLs of the tag being built. The script removes the line for
# a plain vMAJOR.MINOR.PATCH tag only. FAKE_FORMULA_VERSION overrides the
# version of the line, to simulate one the URLs do not carry, which the strip
# step must refuse.
tag="${GORELEASER_CURRENT_TAG:-}"
formula_version="${FAKE_FORMULA_VERSION:-${tag#v}}"
cat >dist/homebrew/Formula/fixture.rb <<FORMULA
class Fixture < Formula
  version "${formula_version}"
  on_linux do
      url "https://github.com/acme/widget/releases/download/${tag}/widget_linux_amd64.tar.gz"
  end
end
FORMULA
# The winget entry renders the manifest set into dist/winget. A tag containing
# "-no-winget" simulates a run that rendered none, "-partial-winget" a set
# without its locale file, "-empty-winget" an empty installer file,
# "-extra-winget" a fourth file and "-two-winget" a second version directory.
winget_dir=dist/winget/manifests/o/OstendorfCloud/gitcd/1.0.0
tag="${GORELEASER_CURRENT_TAG:-}"
# The manifest set is a valid one for the tag, rendered from the templates
# the test wrote, so the check of the publish script passes on the happy path.
render() {
  sed -e "s|@TAG@|${tag}|g" -e "s|@VERSION@|${tag#v}|g" "${WINGET_TEMPLATES}/$1" >"${winget_dir}/$2"
}
case "${tag}" in
*-no-winget*) ;;
*)
  mkdir -p "${winget_dir}"
  render version.tmpl OstendorfCloud.gitcd.yaml
  render installer.tmpl OstendorfCloud.gitcd.installer.yaml
  render locale.tmpl OstendorfCloud.gitcd.locale.en-US.yaml
  ;;
esac
# "-wrong-url" names another tag in a URL, "-wrong-sha" a sha256 the checksums
# file does not hold, "-extra-key" a key the template does not have.
case "${tag}" in
*-partial-winget*) rm "${winget_dir}/OstendorfCloud.gitcd.locale.en-US.yaml" ;;
*-empty-winget*) : >"${winget_dir}/OstendorfCloud.gitcd.installer.yaml" ;;
*-extra-winget*) echo extra >"${winget_dir}/OstendorfCloud.gitcd.locale.de-DE.yaml" ;;
*-two-winget*) mkdir -p dist/winget/manifests/o/OstendorfCloud/gitcd/1.0.1 ;;
*-wrong-url*)
  sed "s|download/${tag}/|download/v9.9.9/|" "${winget_dir}/OstendorfCloud.gitcd.installer.yaml" >"${winget_dir}/changed"
  mv "${winget_dir}/changed" "${winget_dir}/OstendorfCloud.gitcd.installer.yaml"
  ;;
*-wrong-sha*)
  sed "s|11111111|33333333|" "${winget_dir}/OstendorfCloud.gitcd.installer.yaml" >"${winget_dir}/changed"
  mv "${winget_dir}/changed" "${winget_dir}/OstendorfCloud.gitcd.installer.yaml"
  ;;
*-extra-key*) echo "Extra: x" >>"${winget_dir}/OstendorfCloud.gitcd.locale.en-US.yaml" ;;
esac
FAKE_GORELEASER
chmod +x "${fake_bin}/goreleaser"

# fake-bin/cosign logs the call. The script finds cosign on PATH like every
# other tool, there is no switch to turn the verification off, so this fake
# is how a test chooses the outcome. It fails when goreleaser left the
# verify-fails marker next to the bundle, and otherwise needs the arguments
# the documented command uses.
cat <<'FAKE_COSIGN' >"${fake_bin}/cosign"
#!/usr/bin/env bash
echo "cosign $*" >>"${CALL_LOG}"
if [ -f dist/verify-fails ]; then
  echo "fake cosign: signature does not match the expected identity" >&2
  exit 1
fi
exit 0
FAKE_COSIGN
chmod +x "${fake_bin}/cosign"

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
  *-draft* | v1.0.0 | v1.0.0-rc.1) echo true ;;
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

# Templates of the three winget files the fake goreleaser renders, in the
# shape check-winget-manifest.sh accepts. @TAG@ and @VERSION@ are filled in
# per tag, the sha256 values match the fake checksums file.
WINGET_TEMPLATES="${work}/winget-templates"
export WINGET_TEMPLATES
mkdir -p "${WINGET_TEMPLATES}"
cat <<'TEMPLATE' >"${WINGET_TEMPLATES}/version.tmpl"
# This file was generated by GoReleaser. DO NOT EDIT.
# yaml-language-server: $schema=https://aka.ms/winget-manifest.version.1.28.0.schema.json
PackageIdentifier: OstendorfCloud.gitcd
PackageVersion: @VERSION@
DefaultLocale: en-US
ManifestType: version
ManifestVersion: 1.28.0
TEMPLATE
cat <<'TEMPLATE' >"${WINGET_TEMPLATES}/locale.tmpl"
# This file was generated by GoReleaser. DO NOT EDIT.
# yaml-language-server: $schema=https://aka.ms/winget-manifest.defaultLocale.1.28.0.schema.json
PackageIdentifier: OstendorfCloud.gitcd
PackageVersion: @VERSION@
PackageLocale: en-US
Publisher: Ostendorf Cloud
PublisherUrl: https://github.com/ostendorf-cloud
PublisherSupportUrl: https://github.com/ostendorf-cloud/gitcd/issues
PackageName: gitcd
PackageUrl: https://github.com/ostendorf-cloud/gitcd
License: Apache-2.0
LicenseUrl: https://github.com/ostendorf-cloud/gitcd/blob/main/LICENSE
ShortDescription: Clone repositories into a predictable layout and apply a git identity profile per remote
Moniker: gitcd
Tags:
  - git
  - cli
  - developer-tools
ManifestType: defaultLocale
ManifestVersion: 1.28.0
TEMPLATE
cat <<'TEMPLATE' >"${WINGET_TEMPLATES}/installer.tmpl"
# This file was generated by GoReleaser. DO NOT EDIT.
# yaml-language-server: $schema=https://aka.ms/winget-manifest.installer.1.28.0.schema.json
PackageIdentifier: OstendorfCloud.gitcd
PackageVersion: @VERSION@
InstallerLocale: en-US
InstallerType: zip
ReleaseDate: "2026-11-16"
Installers:
  - Architecture: arm64
    NestedInstallerType: portable
    NestedInstallerFiles:
      - RelativeFilePath: gitcd.exe
        PortableCommandAlias: gitcd
    InstallerUrl: https://github.com/ostendorf-cloud/gitcd/releases/download/@TAG@/gitcd_windows_arm64.zip
    InstallerSha256: 2222222222222222222222222222222222222222222222222222222222222222
    UpgradeBehavior: uninstallPrevious
  - Architecture: x64
    NestedInstallerType: portable
    NestedInstallerFiles:
      - RelativeFilePath: gitcd.exe
        PortableCommandAlias: gitcd
    InstallerUrl: https://github.com/ostendorf-cloud/gitcd/releases/download/@TAG@/gitcd_windows_amd64.zip
    InstallerSha256: 1111111111111111111111111111111111111111111111111111111111111111
    UpgradeBehavior: uninstallPrevious
ManifestType: installer
ManifestVersion: 1.28.0
Dependencies:
  PackageDependencies:
    - PackageIdentifier: Git.Git
TEMPLATE

export CALL_LOG
export GITHUB_REPOSITORY="acme/gitcd"
# What a pull request label run carries. The expected identity below must not
# follow it, so the verify call in every case proves it is ignored.
export GITHUB_WORKFLOW_REF="acme/gitcd/.github/workflows/release.yaml@refs/pull/1/merge"
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
    echo "FAIL ${case_name}: calls = $(printf '%q' "${got_calls}"), want $(printf '%q' "${want_calls}")"
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
    echo "FAIL ${case_name}: stderr = $(cat "${work}/stderr"), want it to contain $(printf '%q' "${want_stderr_substring}")"
    failures=$((failures + 1))
    return
  fi

  local got_calls
  got_calls="$(cat "${CALL_LOG}")"
  if printf '%s' "${got_calls}" | grep -qE "worktree add|goreleaser|release upload|release edit"; then
    echo "FAIL ${case_name}: calls = $(printf '%q' "${got_calls}"), want it to stop before building, uploading or publishing"
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
    echo "FAIL ${case_name}: stderr = $(cat "${work}/stderr"), want it to contain $(printf '%q' "${want_stderr_substring}")"
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

# The exact command a reader runs from docs/install.md, with the identity
# built from the repository and the fixed workflow path and branch.
verify_call="cosign verify-blob --bundle dist/fixture_1.0.0_checksums.txt.sigstore.json --certificate-identity https://github.com/acme/gitcd/.github/workflows/release.yaml@refs/heads/main --certificate-oidc-issuer https://token.actions.githubusercontent.com dist/fixture_1.0.0_checksums.txt"

winget_base="dist/winget/manifests/o/OstendorfCloud/gitcd/1.0.0/OstendorfCloud.gitcd"
winget_upload="${winget_base}.yaml ${winget_base}.installer.yaml ${winget_base}.locale.en-US.yaml"

run_case "nothing released makes no call at all" "" ""

run_case "a draft release is built, attached and published" "v1.0.0-draft" \
  "git fetch origin --tags --force
gh release view v1.0.0-draft --json isDraft --jq .isDraft
git worktree add --quiet --detach ${work}/worktree-1 v1.0.0-draft
goreleaser release --clean --skip=announce,validate,publish
${verify_call}
gh release upload v1.0.0-draft dist/fixture_linux_amd64.tar.gz dist/fixture_windows_amd64.zip dist/fixture_1.0.0_checksums.txt dist/fixture_1.0.0_checksums.txt.sigstore.json dist/homebrew/Formula/fixture.rb ${winget_upload} --clobber
gh release edit v1.0.0-draft --draft=false
git worktree remove ${work}/worktree-1"
# fixture_hash tag [stripped] is the sha256 of the formula the fake goreleaser
# renders for tag, without its version line when the second argument is given.
# The strip step runs before the file is hashed and uploaded, and only for a
# plain tag, so "v1.0.0-draft" is hashed as rendered.
fixture_hash() {
  {
    printf 'class Fixture < Formula\n'
    if [ -z "${2:-}" ]; then
      printf '  version "%s"\n' "${1#v}"
    fi
    printf '  on_linux do\n      url "https://github.com/acme/widget/releases/download/%s/widget_linux_amd64.tar.gz"\n  end\nend\n' "$1"
  } | shasum -a 256 | cut -d' ' -f1
}
formula_hash="$(fixture_hash v1.0.0-draft)"
check_formula_hashes "the formula hash of the build is a step output" \
  "formula-hashes<<FORMULA_HASHES_EOF
v1.0.0-draft ${formula_hash}
FORMULA_HASHES_EOF"

run_case "only the verified checksums file and its bundle are uploaded, not every matching file" \
  "v1.0.0-draft-extra-checksums" \
  "git fetch origin --tags --force
gh release view v1.0.0-draft-extra-checksums --json isDraft --jq .isDraft
git worktree add --quiet --detach ${work}/worktree-1 v1.0.0-draft-extra-checksums
goreleaser release --clean --skip=announce,validate,publish
${verify_call}
gh release upload v1.0.0-draft-extra-checksums dist/fixture_linux_amd64.tar.gz dist/fixture_windows_amd64.zip dist/fixture_1.0.0_checksums.txt dist/fixture_1.0.0_checksums.txt.sigstore.json dist/homebrew/Formula/fixture.rb ${winget_upload} --clobber
gh release edit v1.0.0-draft-extra-checksums --draft=false
git worktree remove ${work}/worktree-1"

run_case "an already published release is left alone" "v1.0.0-published" \
  "git fetch origin --tags --force
gh release view v1.0.0-published --json isDraft --jq .isDraft"

run_case "every tag in a multi-line TAGS is handled" "v1.0.0-draft
v2.0.0-published" \
  "git fetch origin --tags --force
gh release view v1.0.0-draft --json isDraft --jq .isDraft
git worktree add --quiet --detach ${work}/worktree-1 v1.0.0-draft
goreleaser release --clean --skip=announce,validate,publish
${verify_call}
gh release upload v1.0.0-draft dist/fixture_linux_amd64.tar.gz dist/fixture_windows_amd64.zip dist/fixture_1.0.0_checksums.txt dist/fixture_1.0.0_checksums.txt.sigstore.json dist/homebrew/Formula/fixture.rb ${winget_upload} --clobber
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

# check_nothing_published name asserts that the last case neither attached
# nor published the release, the draft stays as it was for a rerun.
check_nothing_published() {
  local case_name="$1"
  if grep -qE "release upload|release edit" "${CALL_LOG}"; then
    echo "FAIL ${case_name}: calls = $(printf '%q' "$(cat "${CALL_LOG}")"), want no upload and no publish"
    failures=$((failures + 1))
    return
  fi
  echo "PASS ${case_name}"
}

# check_no_verifier_call name asserts that the last case stopped before it
# asked cosign anything.
check_no_verifier_call() {
  local case_name="$1"
  if grep -q "^cosign " "${CALL_LOG}"; then
    echo "FAIL ${case_name}: calls = $(printf '%q' "$(cat "${CALL_LOG}")"), want cosign never called"
    failures=$((failures + 1))
    return
  fi
  echo "PASS ${case_name}"
}

bundle_error="expected exactly one non-empty checksums signature bundle in dist for"

run_partial_failure_case \
  "a missing signature bundle stops the script and removes the worktree" \
  "v1.0.0-draft-unsigned" "${bundle_error} v1.0.0-draft-unsigned"
check_nothing_published "a missing signature bundle attaches and publishes nothing"
check_no_verifier_call "a missing signature bundle is not handed to the verifier"
check_formula_hashes "a build without a signature records no hash" ""

run_partial_failure_case \
  "an empty signature bundle stops the script and removes the worktree" \
  "v1.0.0-draft-empty-bundle" "${bundle_error} v1.0.0-draft-empty-bundle"
check_nothing_published "an empty signature bundle attaches and publishes nothing"
check_no_verifier_call "an empty signature bundle is not handed to the verifier"
check_formula_hashes "a build with an empty bundle records no hash" ""

run_partial_failure_case \
  "two signature bundles stop the script and remove the worktree" \
  "v1.0.0-draft-two-bundles" "${bundle_error} v1.0.0-draft-two-bundles"
check_nothing_published "two signature bundles attach and publish nothing"
check_no_verifier_call "two signature bundles are not handed to the verifier"
check_formula_hashes "a build with two bundles records no hash" ""

run_partial_failure_case \
  "an empty checksums file stops the script and removes the worktree" \
  "v1.0.0-draft-empty-checksums" "the checksums file dist/fixture_1.0.0_checksums.txt for v1.0.0-draft-empty-checksums is missing or empty"
check_nothing_published "an empty checksums file attaches and publishes nothing"
check_no_verifier_call "an empty checksums file is not handed to the verifier"
check_formula_hashes "a build with an empty checksums file records no hash" ""

run_partial_failure_case \
  "a signature that does not verify stops the script and removes the worktree" \
  "v1.0.0-draft-verify-fails" "does not verify against the release workflow identity on the main branch"
if ! grep -qxF "${verify_call}" "${CALL_LOG}"; then
  echo "FAIL the failing verification did not run the documented command: calls = $(printf '%q' "$(cat "${CALL_LOG}")")"
  failures=$((failures + 1))
else
  echo "PASS the failing verification ran the documented command"
fi
check_nothing_published "a signature that does not verify attaches and publishes nothing"
check_formula_hashes "a build whose signature does not verify records no hash" ""

# A plain tag has its version line removed, before the hash is taken: the hash
# is that of the stripped file and the upload still happens.
run_case "a stable tag is built, stripped, attached and published" "v1.0.0" \
  "git fetch origin --tags --force
gh release view v1.0.0 --json isDraft --jq .isDraft
git worktree add --quiet --detach ${work}/worktree-1 v1.0.0
goreleaser release --clean --skip=announce,validate,publish
${verify_call}
gh release upload v1.0.0 dist/fixture_linux_amd64.tar.gz dist/fixture_windows_amd64.zip dist/fixture_1.0.0_checksums.txt dist/fixture_1.0.0_checksums.txt.sigstore.json dist/homebrew/Formula/fixture.rb ${winget_upload} --clobber
gh release edit v1.0.0 --draft=false
git worktree remove ${work}/worktree-1"
check_formula_hashes "a stable tag hands over the hash of the stripped formula" \
  "formula-hashes<<FORMULA_HASHES_EOF
v1.0.0 $(fixture_hash v1.0.0 stripped)
FORMULA_HASHES_EOF"

# A pre-release tag keeps the formula as rendered, version line included: its
# URLs would make Homebrew scan another version. It publishes as before and the
# homebrew job never pushes it.
run_case "a pre-release tag is built, attached and published with its formula unstripped" "v1.0.0-rc.1" \
  "git fetch origin --tags --force
gh release view v1.0.0-rc.1 --json isDraft --jq .isDraft
git worktree add --quiet --detach ${work}/worktree-1 v1.0.0-rc.1
goreleaser release --clean --skip=announce,validate,publish
${verify_call}
gh release upload v1.0.0-rc.1 dist/fixture_linux_amd64.tar.gz dist/fixture_windows_amd64.zip dist/fixture_1.0.0_checksums.txt dist/fixture_1.0.0_checksums.txt.sigstore.json dist/homebrew/Formula/fixture.rb ${winget_upload} --clobber
gh release edit v1.0.0-rc.1 --draft=false
git worktree remove ${work}/worktree-1"
check_formula_hashes "a pre-release tag hands over the hash of the formula as rendered" \
  "formula-hashes<<FORMULA_HASHES_EOF
v1.0.0-rc.1 $(fixture_hash v1.0.0-rc.1)
FORMULA_HASHES_EOF"

export FAKE_FORMULA_VERSION=1.0.1
run_partial_failure_case \
  "a formula version the urls do not carry stops the script and removes the worktree" \
  "v1.0.0" "is not the version 1.0.0 the urls carry"
check_nothing_published "a formula whose version line cannot be removed attaches and publishes nothing"
check_no_verifier_call "a formula whose version line cannot be removed is not handed to the verifier"
check_formula_hashes "a formula whose version line cannot be removed records no hash" ""
unset FAKE_FORMULA_VERSION

winget_dir_error="expected exactly one version directory under dist/winget/manifests/o/OstendorfCloud/gitcd for"

run_partial_failure_case \
  "no winget manifest stops the script and removes the worktree" \
  "v1.0.0-draft-no-winget" "${winget_dir_error} v1.0.0-draft-no-winget"
check_nothing_published "no winget manifest attaches and publishes nothing"
check_no_verifier_call "no winget manifest is not handed to the verifier"
check_formula_hashes "a build without a winget manifest records no hash" ""

run_partial_failure_case \
  "two winget version directories stop the script and remove the worktree" \
  "v1.0.0-draft-two-winget" "${winget_dir_error} v1.0.0-draft-two-winget"
check_nothing_published "two winget version directories attach and publish nothing"

run_partial_failure_case \
  "a winget set without its locale file stops the script and removes the worktree" \
  "v1.0.0-draft-partial-winget" "the winget manifest file ${winget_base}.locale.en-US.yaml for v1.0.0-draft-partial-winget is missing or empty"
check_nothing_published "an incomplete winget set attaches and publishes nothing"
check_no_verifier_call "an incomplete winget set is not handed to the verifier"
check_formula_hashes "a build with an incomplete winget set records no hash" ""

run_partial_failure_case \
  "an empty winget installer file stops the script and removes the worktree" \
  "v1.0.0-draft-empty-winget" "the winget manifest file ${winget_base}.installer.yaml for v1.0.0-draft-empty-winget is missing or empty"
check_nothing_published "an empty winget installer file attaches and publishes nothing"

run_partial_failure_case \
  "a fourth file in the winget directory stops the script and removes the worktree" \
  "v1.0.0-draft-extra-winget" "must hold exactly three files, has 4 entries"
check_nothing_published "a fourth winget file attaches and publishes nothing"

# The manifest is checked against this run's tag and checksums file before the
# signature is verified and before anything is attached.
run_partial_failure_case \
  "a winget url for another tag stops the script and removes the worktree" \
  "v1.0.0-draft-wrong-url" "is not this repository's release download of v1.0.0-draft-wrong-url"
check_nothing_published "a winget url for another tag attaches and publishes nothing"
check_no_verifier_call "a winget url for another tag is not handed to the verifier"
check_formula_hashes "a build with a wrong winget url records no hash" ""

run_partial_failure_case \
  "a winget sha256 that differs from the checksums file stops the script" \
  "v1.0.0-draft-wrong-sha" "differs from the checksums file"
check_nothing_published "a winget sha256 that differs attaches and publishes nothing"
check_no_verifier_call "a winget sha256 that differs is not handed to the verifier"

run_partial_failure_case \
  "an extra key in a winget file stops the script" \
  "v1.0.0-draft-extra-key" "differs from the generated skeleton"
check_nothing_published "an extra winget key attaches and publishes nothing"
check_no_verifier_call "an extra winget key is not handed to the verifier"
run_partial_failure_case \
  "a failed manifest check says the draft stays" \
  "v1.0.0-draft-extra-key" "Nothing was attached or published, the draft release stays"

saved_repository="${GITHUB_REPOSITORY}"
unset GITHUB_REPOSITORY
run_failure_case \
  "a missing GITHUB_REPOSITORY stops the script before it builds anything" \
  "v1.0.0-draft" "GITHUB_REPOSITORY must name the repository"
export GITHUB_REPOSITORY="${saved_repository}"

run_partial_failure_case \
  "an upload failure still removes the worktree" \
  "v1.0.0-draft-upload-fails" "fake gh: upload failed"
check_formula_hashes "a failure after the build still hands over the hash" \
  "formula-hashes<<FORMULA_HASHES_EOF
v1.0.0-draft-upload-fails $(fixture_hash v1.0.0-draft-upload-fails)
FORMULA_HASHES_EOF"

if [ "${failures}" -ne 0 ]; then
  echo "${failures} test(s) failed"
  exit 1
fi

finished=yes
echo "all tests passed"
