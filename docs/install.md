# Install

## Homebrew

On macOS and Linux:

```sh
brew install ostendorf-cloud/tap/gitcd
gitcd version
```

- The formula comes from `ostendorf-cloud/homebrew-tap`, written by gitcd's release pipeline.
- Each release replaces the formula. `brew upgrade gitcd` follows it.
- The formula installs `git` as a dependency, since the commands that touch repositories shell
  out to git.
- Why a formula and not a cask: `docs/adr/0003-homebrew-formula-not-cask.md`.

## winget

On Windows, once the first manifest pull request to `microsoft/winget-pkgs` is merged:

```powershell
winget install OstendorfCloud.gitcd
gitcd version
```

- Not available yet. The first release has to exist and the owner has to submit the manifest
  first, see "Submitting to winget" below. Until then use a release archive.
- The package is the `zip` archive of the release for `x64` or `arm64`. winget installs the
  portable `gitcd.exe` and adds the alias `gitcd` to the `PATH`. The manifest also lists the
  moniker `gitcd`.
- The manifest declares `Git.Git` as a dependency, since the commands that touch repositories
  shell out to git.
- `winget upgrade OstendorfCloud.gitcd` follows each version the owner submits.

### Shell integration on PowerShell

The manifest cannot change the shell, so `cd` does not change the directory after a plain
install. Add this line to the PowerShell profile (`notepad $PROFILE`) to turn the integration on:

```powershell
Invoke-Expression (gitcd shell-init pwsh | Out-String)
```

- `pwsh` is PowerShell 7. Windows PowerShell 5.1 uses `powershell`, which prints the identical
  snippet. See `shell-init` in `docs/commands.md`.
- The snippet defines a `gitcd` function that changes the directory for `clone`, `path` and
  `cd`, and registers tab completion for `cd`.
- `shell-init` knows no Command Prompt (`cmd.exe`). There `gitcd path <query>` still prints
  the path, but nothing changes the directory.

## Release archives

Every release on GitHub carries a `tar.gz` per Linux and macOS system and a `zip` for Windows,
for `amd64` and `arm64`, with a checksums file and its signature bundle. It also carries
`gitcd.rb`, the formula the build generated. The pipeline pushes that file to the tap, `brew`
does not read it from there. Three more files are the winget manifest set the build generated
(`OstendorfCloud.gitcd.yaml`, `OstendorfCloud.gitcd.installer.yaml` and
`OstendorfCloud.gitcd.locale.en-US.yaml`). Nothing reads them from the release automatically,
the owner submits them. That makes twelve assets.

## Verify a download

Every release carries a signature bundle for its checksums file. It shows that this repository's
release workflow produced the checksums, and the checksums cover the archives. You need the
`cosign` CLI.

```sh
tag=v1.2.3
archive=gitcd_linux_amd64.tar.gz
version="${tag#v}"
checksums="gitcd_${version}_checksums.txt"
base="https://github.com/ostendorf-cloud/gitcd/releases/download/${tag}"

curl -fsSLO "${base}/${checksums}" &&
  curl -fsSLO "${base}/${checksums}.sigstore.json" &&
  curl -fsSLO "${base}/${archive}" &&
  cosign verify-blob "${checksums}" \
    --bundle "${checksums}.sigstore.json" \
    --certificate-identity "https://github.com/ostendorf-cloud/gitcd/.github/workflows/release.yaml@refs/heads/main" \
    --certificate-oidc-issuer "https://token.actions.githubusercontent.com" &&
  test -s "${archive}" &&
  line="$(grep -F "  ${archive}" "${checksums}")" &&
  printf '%s\n' "${line}" | sha256sum -c -
```

- On macOS use `shasum -a 256 -c -` in place of `sha256sum -c -`. Do not use `sha256sum
  --ignore-missing` there: the `sha256sum` that macOS ships exits 0 when it verified nothing.
- The chain stops at the first failure: a failed download, a signature that does not verify, a
  missing archive, no line for the archive in the checksums file, or a hash that differs.
- `cosign verify-blob` exits non-zero when the bundle does not match the checksums file or the
  identity. Do not go on in that case.
- A `grep` that finds no line for the archive fails the chain before the hash check runs.

What the check proves:

- The checksums file was signed by a run of `release.yaml` of this repository on `main`.
- The archive you downloaded matches the hash in that signed file.
- The signature is recorded in the public Rekor transparency log.

What it does not prove:

- It does not tie the files to a version. The archive names carry no version and the checksums
  file lists none. Only the file name of the checksums file does, and a name is not signed.
- A genuine older release passes too. Serving the files of an older release in place of the one
  you asked for, or a downgrade, is not prevented.
- `cosign verify-blob` has an optional `--certificate-github-workflow-sha` flag, the commit the
  run was based upon. The run is a push to `main`, so it is usually the commit the tag points
  to. The exception is a draft resumed by a later run: a later push to `main` then signs, and
  the commit is that later one. Pass the flag with the tag's commit to narrow the check, and
  expect it to fail for such a release. This was not tried on a real release.
- A compromised workflow signs whatever it builds, and the check says nothing about the code.
- The identity contains the workflow path and the branch. If `release.yaml` is renamed or
  moved, releases made after that carry the new path.

## Signing identity

For maintainers and the infrastructure repository. Keyless signing has no key and adds no secret.

- Issuer: `https://token.actions.githubusercontent.com`.
- Identity: `https://github.com/ostendorf-cloud/gitcd/.github/workflows/release.yaml@refs/heads/main`.
- Log: the public Rekor instance of Sigstore.
- Permission: `id-token: write` on the `release` job only.
- Decision and alternatives: `docs/adr/0004-keyless-signing.md`.

## Publisher identity for winget

For maintainers and the infrastructure repository. winget has no account and no secret in this
pipeline.

- Package identifier: `OstendorfCloud.gitcd`. Moniker: `gitcd`.
- Publisher name: `Ostendorf Cloud`. The identifier and the folder
  `manifests/o/OstendorfCloud/gitcd/` in `microsoft/winget-pkgs` are claimed by the first merged
  pull request.
- Submitter: the owner's personal GitHub account, by hand. No organisation account, app or
  token for `microsoft/winget-pkgs` exists, and none is needed.
- Secrets and permissions: none added. The release workflow never contacts that repository.
- Nothing for the infrastructure repository to create or rotate. It records the identifier and
  the publisher name above.
- Decision and alternatives: `docs/adr/0005-winget-manifest-semi-automatic.md`.

## Submitting to winget

For the owner, on Windows. The release attaches the manifest set, the owner files the pull
request. The manifest names published release assets, so wait for the release to be public.

Submit only stable releases. A release of a pre-release tag (`rc`, `beta`, `alpha`) carries a
manifest too, since the release script treats every tag alike. Never submit that one. This
mirrors the `homebrew` job, which skips pre-releases. The policy of `microsoft/winget-pkgs` for
pre-release versions was not looked up.

First version:

1. Download the three `OstendorfCloud.gitcd*.yaml` assets of the release into one empty folder,
   and the checksums file with its signature bundle. Verify the checksums file with
   `cosign verify-blob`, see "Verify a download". The manifest files are not signed
   themselves. Then run the check on the three files from a checkout of the same tag, in a bash
   such as the one Git for Windows ships, and do not submit unless it exits 0:

   ```sh
   scripts/check-winget-manifest.sh <downloaded folder> <tag> <verified checksums file>
   ```

   It compares every line with the generated template, every URL with this repository's
   release download of the tag and every `sha256` with the verified checksums file. It was not
   run on Windows.
2. Fork `microsoft/winget-pkgs` with the personal account and clone it. Create a branch.
3. Copy the three files unchanged into `manifests/o/OstendorfCloud/gitcd/<version>/`, with
   `<version>` the tag without its `v`. The file names stay as they are.
4. Run `winget validate --manifest manifests/o/OstendorfCloud/gitcd/<version>`. Optionally
   install from the folder with `winget install --manifest <folder>` after enabling local
   manifest files in the winget settings, as the repository's `doc/Authoring.md` describes.
5. Commit, push the branch to the fork and open a pull request against `microsoft/winget-pkgs`.
   One pull request holds one package version. Follow the repository's pull request template.
6. A GitHub App runs the validation pipeline on the manifest and its installers and reports on
   the pull request. The repository's `doc/Validation.md` and `doc/ValidationFailureGuide.md`
   describe the steps and the failures. Fix a failure in the manifest by changing
   `.goreleaser.yaml` and the next release, never by editing the checksums by hand.
7. After the merge, check on a Windows machine that `winget install OstendorfCloud.gitcd` works
   and that `gitcd version` runs. Only then is the issue done.

Later versions, until the submission is automated by a separate decision:

- Do the same with the files of the new release, check included. Winget's tooling wants all
  three files, and these carry the checksums of the published archives.
- Or run `wingetcreate update OstendorfCloud.gitcd --version <version> --urls
  "<arm64 zip url>|arm64" "<x64 zip url>|x64"`, which fits once the package is in
  `microsoft/winget-pkgs`: the tool needs as many URLs as the manifest has installers (two) and
  accepts the `<url>|<architecture>` form. It downloads the archives and works the checksums out
  itself. `--submit` and a token of the personal account file the pull request. How
  `wingetcreate` treats a `zip` with a nested portable file is not documented in the pages read
  here and was not run.

## Release pipeline

For maintainers. `release.yaml` runs these in order:

1. `release-me` tags and drafts the release.
2. `scripts/publish-release-assets.sh` builds the archives, the formula and the winget
   manifest in one goreleaser run, which also signs the checksums with cosign. It refuses to go
   on without exactly one non-empty bundle and without the complete manifest set of three files.
   It checks the manifest with `scripts/check-winget-manifest.sh` against the tag and the
   checksums file of the run, and verifies the bundle with the command above, before anything is
   attached. Only then it
   records the formula's sha256 as the `formula-hashes` job output, attaches the files and
   publishes the release. It never contacts `microsoft/winget-pkgs`.
3. The `homebrew` job runs `scripts/publish-homebrew-formula.sh`. It reads the formula back from
   the published release and verifies it against the build: the sha256 must be the recorded one,
   and every line must be part of the generated template
   (`scripts/check-homebrew-formula.sh`). Only then it commits the formula to the tap, with a
   token scoped to the tap alone.
4. The push is skipped when the tap already holds the same file, and refused when the tap holds a
   newer version.

### When the signature check fails

The release stays a draft with no assets, and the `release` job is red. The script verified the
bundle against `release.yaml@refs/heads/main`, so a run from another ref, such as the one a
label change on the release pull request starts, signs with a different identity and stops
here. That is intended: such a release would not pass the command readers run.

- Re-run the failed run of the push to `main` with "Re-run failed jobs", not a label run.
- The draft was tagged in a label run when the push run never tagged it. No failed push run
  exists then. Re-run the latest push run on `main`, or push a new commit to `main`.
- Either way a later run of `release-me run` finds the merged pull request still labeled
  `release: tagged` whose release is a draft, and reports its tag again so the asset step
  retries. Read from release-me's `docs/commands.md` (section on `release`) and
  `docs/action.md`. Not run against a real draft.
- A run that resumes the draft signs on the ref it runs on, `refs/heads/main` for a push, so
  the check passes there. A label run does not.
- Nothing was attached, so nothing needs cleaning up.
- A failure with a message about the bundle itself means goreleaser signed nothing usable.
  Read its log, then re-run as above.

### When the push fails

The release is public and has no formula yet, and the run is red.

- When the `release` job succeeded, use "Re-run failed jobs" on that run. Only the `homebrew`
  job runs again, the `release` job keeps its outputs, nothing is rebuilt, and an up to date tap
  changes nothing.
- Never use "Re-run all jobs". `release-me` then finds nothing left to release, `tags` is empty
  and the `homebrew` job is skipped, so the run ends green with no formula.
- The `homebrew` job also runs, and ends red, when the `release` job failed or was cancelled
  after it published. If the tap push failed as well, "Re-run failed jobs" reruns the `release`
  job too. It finds nothing to release, `tags` is empty and the `homebrew` job is skipped, so the
  run ends green with no formula. Use the manual run below in that case.

### After the run is gone

Run the `release` workflow by hand from the default branch ("Run workflow") with the `tag` input
set to the published release. Only the `homebrew` job runs. No job output exists then, so the
formula is verified differently:

- Every line must still be part of the generated template, as above.
- Every sha256 in `gitcd.rb` must equal the matching line of the release's checksums file.

This rests on trust in the release's assets as they are published now. It shows that the formula
agrees with them, not that they are the files the build produced. Use it only when the recorded
hash is not available. The `allow-downgrade` input lets a tag older than the tap's formula
replace it, and is off by default.
