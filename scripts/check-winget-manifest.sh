#!/usr/bin/env bash
# Copyright 2026 Simon Ostendorf
# SPDX-License-Identifier: Apache-2.0
#
# Usage: check-winget-manifest.sh <manifest dir> <tag> <checksums file> [version]
#
# Verifies the winget manifest set goreleaser generates from .goreleaser.yaml
# (the winget entry). The directory holds exactly the three files below, and
# every line of each must match the skeleton of the generated file, so nothing
# but the generated template passes. Beyond the skeleton:
#
#   - PackageVersion is the version: the tag without its leading v, or the
#     fourth argument. A snapshot build names its own version, so the snapshot
#     check passes the one its checksums file carries.
#   - Every installer URL is this repository's own release download of <tag>,
#     one for each Windows archive, and sits under the matching architecture.
#   - Every InstallerSha256 equals the entry of that archive in the checksums
#     file, which is the file the release signs.
#   - The two entries of the installer file may come in either order. goreleaser
#     lists the archives in the order their goroutines finished, which varies
#     between runs, and the schema gives the order of Installers no meaning.
#     The entries are put in one order before the skeleton comparison, each
#     architecture still has to appear exactly once.
#
# The shapes come from a snapshot build (task test:winget runs this script on
# one), so a change to the winget entry that alters the generated lines fails
# there first and the skeletons below are updated with it.
set -euo pipefail

# Several checks below overlap on purpose. The skeleton comparison at the end
# decides on its own for every altered file, and these are layered defence
# behind it, each with its own message: the files and the checksums file
# being non-empty, the version shape, the NUL and printable ASCII checks, the
# sha256 line directly after its url, and the lower case hex shape of the
# sha256. The architecture, foreign url and once each checks overlap with
# one another, the test script has one case each that breaks only one of them.

if [ "$#" -lt 3 ] || [ "$#" -gt 4 ]; then
  echo "usage: $0 <manifest dir> <tag> <checksums file> [version]" >&2
  exit 2
fi

dir="$1"
tag="$2"
checksums="$3"
version="${4:-${tag#v}}"

id="OstendorfCloud.gitcd"
version_file="${dir}/${id}.yaml"
installer_file="${dir}/${id}.installer.yaml"
locale_file="${dir}/${id}.locale.en-US.yaml"

if [ ! -d "${dir}" ]; then
  echo "manifest directory ${dir} does not exist" >&2
  exit 1
fi

# Exactly the three files, nothing else next to them.
count="$(find "${dir}" -mindepth 1 | wc -l | tr -d ' ')"
if [ "${count}" -ne 3 ]; then
  echo "manifest directory must hold exactly three files, has ${count} entries" >&2
  exit 1
fi
for f in "${version_file}" "${installer_file}" "${locale_file}"; do
  if [ ! -f "${f}" ] || [ ! -s "${f}" ]; then
    echo "manifest file ${f} is missing or empty" >&2
    exit 1
  fi
done
if [ ! -s "${checksums}" ]; then
  echo "checksums file ${checksums} is missing or empty" >&2
  exit 1
fi

if [ -z "${version}" ] || ! [[ ${version} =~ ^[0-9]+\.[0-9]+\.[0-9]+[0-9A-Za-z.+-]*$ ]]; then
  echo "version '${version}' is not a version" >&2
  exit 1
fi

# Defence in depth, behind the skeleton comparison: a NUL byte, which an old
# bash drops on read, and anything but printable ASCII and newlines.
for f in "${version_file}" "${installer_file}" "${locale_file}"; do
  size="$(wc -c <"${f}" | tr -d ' ')"
  without_nul="$(tr -d '\0' <"${f}" | wc -c | tr -d ' ')"
  if [ "${size}" != "${without_nul}" ]; then
    echo "${f} contains a NUL byte" >&2
    exit 1
  fi
  if LC_ALL=C grep -q -v -E '^[[:print:]]*$' "${f}"; then
    echo "${f} contains a character outside printable ASCII" >&2
    exit 1
  fi
  # The last byte is a newline. Command substitution drops it, so an empty
  # result means it was there.
  if [ -n "$(tail -c 1 "${f}")" ]; then
    echo "${f} does not end with a newline" >&2
    exit 1
  fi
done

compare_dir="$(mktemp -d "${TMPDIR:-/tmp}/check-winget-manifest.XXXXXXXX")"
trap 'rm -rf "${compare_dir}"' EXIT

tag_re="${tag//./\\.}"
tag_re="${tag_re//+/\\+}"
download="https://github.com/ostendorf-cloud/gitcd/releases/download/${tag_re}"
url_re="^    InstallerUrl: ${download}/gitcd_windows_(amd64|arm64)\\.zip\$"
sha_re='^    InstallerSha256: ([0-9a-f]{64})$'
date_re='^ReleaseDate: "[0-9]{4}-[0-9]{2}-[0-9]{2}"$'

fail() {
  echo "${1}" >&2
  exit 1
}

# Each file carries its version line exactly once, with the expected value.
for f in "${version_file}" "${installer_file}" "${locale_file}"; do
  total="$(grep -c '^PackageVersion: ' "${f}" || true)"
  exact="$(grep -cxF -- "PackageVersion: ${version}" "${f}" || true)"
  if [ "${total}" -ne 1 ] || [ "${exact}" -ne 1 ]; then
    fail "${f} must carry exactly one line 'PackageVersion: ${version}'"
  fi
done

# The release date of the installer file, one line of the shape of a date.
date_total="$(grep -c '^ReleaseDate: ' "${installer_file}" || true)"
date_ok="$(grep -cE -- "${date_re}" "${installer_file}" || true)"
if [ "${date_total}" -ne 1 ] || [ "${date_ok}" -ne 1 ]; then
  fail "${installer_file} must carry exactly one ReleaseDate line holding a quoted date"
fi

# checksum_of <archive> prints the sha256 of the archive's one line in the
# checksums file.
checksum_of() {
  local archive="$1" lines
  lines="$(grep -E "^[0-9a-f]{64}  ${archive//./\\.}\$" "${checksums}" || true)"
  if [ -z "${lines}" ] || [ "$(printf '%s\n' "${lines}" | wc -l | tr -d ' ')" -ne 1 ]; then
    fail "the checksums file must carry exactly one entry for ${archive}"
  fi
  printf '%s' "${lines%% *}"
}

# Put the two installer entries in one order, arm64 first, as the skeleton
# below has them. An entry starts at its Architecture line and runs over the
# lines indented further. The architecture lines of the entries are not
# changed, so an entry whose URL names the other architecture still fails the
# walk below.
head_part=""
arm_part=""
x64_part=""
tail_part=""
state="head"
current=""
arm_entries=0
x64_entries=0
while IFS= read -r line || [ -n "${line}" ]; do
  if [ "${state}" = "head" ]; then
    case "${line}" in
    '  - Architecture: arm64' | '  - Architecture: x64') state="entries" ;;
    *)
      head_part="${head_part}${line}"$'\n'
      continue
      ;;
    esac
  fi
  if [ "${state}" = "entries" ]; then
    case "${line}" in
    '  - Architecture: arm64')
      current="arm64"
      arm_entries=$((arm_entries + 1))
      arm_part="${arm_part}${line}"$'\n'
      continue
      ;;
    '  - Architecture: x64')
      current="x64"
      x64_entries=$((x64_entries + 1))
      x64_part="${x64_part}${line}"$'\n'
      continue
      ;;
    '    '*)
      if [ "${current}" = "arm64" ]; then
        arm_part="${arm_part}${line}"$'\n'
      else
        x64_part="${x64_part}${line}"$'\n'
      fi
      continue
      ;;
    *) state="tail" ;;
    esac
  fi
  tail_part="${tail_part}${line}"$'\n'
done <"${installer_file}"
if [ "${arm_entries}" -ne 1 ] || [ "${x64_entries}" -ne 1 ]; then
  fail "the installer file must list the arm64 entry and the x64 entry once each, has ${arm_entries} and ${x64_entries}"
fi
installer_canonical="${compare_dir}/installer.canonical"
printf '%s%s%s%s' "${head_part}" "${arm_part}" "${x64_part}" "${tail_part}" >"${installer_canonical}"

# Walk the installer file: every URL names this tag's archive of the
# architecture above it, every sha256 follows its URL and equals the checksums
# file's entry for that archive.
arch=""
archive=""
want_sha=0
seen=""
# The messages name the entry, not a line: the walk reads the reordered copy,
# whose line numbers are not those of the file as it was given.
where() {
  if [ -n "${arch}" ]; then
    echo "the ${arch} entry of the installer file"
  else
    echo "the head of the installer file"
  fi
}
while IFS= read -r line || [ -n "${line}" ]; do
  if [ "${want_sha}" -eq 1 ]; then
    if ! [[ ${line} =~ ${sha_re} ]]; then
      fail "$(where): an InstallerUrl must be followed by its InstallerSha256"
    fi
    want="$(checksum_of "${archive}")"
    if [ "${BASH_REMATCH[1]}" != "${want}" ]; then
      fail "$(where): the sha256 of ${archive} differs from the checksums file"
    fi
    want_sha=0
    continue
  fi
  case "${line}" in
  '  - Architecture: arm64') arch="arm64" ;;
  '  - Architecture: x64') arch="x64" ;;
  esac
  if [[ ${line} =~ ${url_re} ]]; then
    url_arch="${BASH_REMATCH[1]}"
    if { [ "${url_arch}" = "amd64" ] && [ "${arch}" != "x64" ]; } ||
      { [ "${url_arch}" = "arm64" ] && [ "${arch}" != "arm64" ]; }; then
      fail "$(where): a ${url_arch} archive under the ${arch:-no} architecture"
    fi
    archive="gitcd_windows_${url_arch}.zip"
    seen="${seen}${url_arch}"$'\n'
    want_sha=1
    continue
  fi
  if [[ ${line} == '    InstallerUrl: '* ]]; then
    fail "$(where): an InstallerUrl that is not this repository's release download of ${tag}"
  fi
done <"${installer_canonical}"
if [ "${want_sha}" -eq 1 ]; then
  fail "the installer file ends after an InstallerUrl without its InstallerSha256"
fi
# Each archive once, in whatever order the entries came. The entries are in
# one order by now, so a swapped pair of URLs shows up as the walk's
# architecture mismatch, and this catches a repeated or a missing archive.
if [ "$(printf '%s' "${seen}" | sort | tr '\n' ' ')" != "amd64 arm64 " ]; then
  fail "the installer file must list the arm64 archive and the amd64 archive once each"
fi

# The whole files against the skeletons of the generated files: version,
# date, URL and sha256 values replaced by a placeholder, everything else, the
# order and the indentation included, as goreleaser writes it. The installer
# entries are compared in the one order above.

normalise() {
  sed -E \
    -e 's|^(PackageVersion: ).*$|\1VERSION|' \
    -e 's|^(ReleaseDate: ).*$|\1DATE|' \
    -e 's|^(    InstallerUrl: ).*$|\1URL|' \
    -e 's|^(    InstallerSha256: ).*$|\1SHA256|' \
    "$1"
}

normalise "${version_file}" >"${compare_dir}/version"
cat >"${compare_dir}/version.skeleton" <<'SKELETON'
# This file was generated by GoReleaser. DO NOT EDIT.
# yaml-language-server: $schema=https://aka.ms/winget-manifest.version.1.28.0.schema.json
PackageIdentifier: OstendorfCloud.gitcd
PackageVersion: VERSION
DefaultLocale: en-US
ManifestType: version
ManifestVersion: 1.28.0
SKELETON

normalise "${locale_file}" >"${compare_dir}/locale"
cat >"${compare_dir}/locale.skeleton" <<'SKELETON'
# This file was generated by GoReleaser. DO NOT EDIT.
# yaml-language-server: $schema=https://aka.ms/winget-manifest.defaultLocale.1.28.0.schema.json
PackageIdentifier: OstendorfCloud.gitcd
PackageVersion: VERSION
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
SKELETON

normalise "${installer_canonical}" >"${compare_dir}/installer"
cat >"${compare_dir}/installer.skeleton" <<'SKELETON'
# This file was generated by GoReleaser. DO NOT EDIT.
# yaml-language-server: $schema=https://aka.ms/winget-manifest.installer.1.28.0.schema.json
PackageIdentifier: OstendorfCloud.gitcd
PackageVersion: VERSION
InstallerLocale: en-US
InstallerType: zip
ReleaseDate: DATE
Installers:
  - Architecture: arm64
    NestedInstallerType: portable
    NestedInstallerFiles:
      - RelativeFilePath: gitcd.exe
        PortableCommandAlias: gitcd
    InstallerUrl: URL
    InstallerSha256: SHA256
    UpgradeBehavior: uninstallPrevious
  - Architecture: x64
    NestedInstallerType: portable
    NestedInstallerFiles:
      - RelativeFilePath: gitcd.exe
        PortableCommandAlias: gitcd
    InstallerUrl: URL
    InstallerSha256: SHA256
    UpgradeBehavior: uninstallPrevious
ManifestType: installer
ManifestVersion: 1.28.0
Dependencies:
  PackageDependencies:
    - PackageIdentifier: Git.Git
SKELETON

for name in version locale installer; do
  if ! diff "${compare_dir}/${name}.skeleton" "${compare_dir}/${name}" >&2; then
    fail "the ${name} manifest differs from the generated skeleton (lines starting with > are in the file)"
  fi
done
