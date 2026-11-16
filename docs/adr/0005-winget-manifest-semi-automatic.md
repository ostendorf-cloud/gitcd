# 5. Generate the winget manifest in the release, submit it by hand

Status: accepted

## Context

- `winget install OstendorfCloud.gitcd` must work on Windows (issue 22). Homebrew moved to
  issue 50 and signing to issue 44.
- winget installs from manifests that live in `microsoft/winget-pkgs`. A package gets there
  through a pull request to that repository.
- GoReleaser has a `winget` entry (<https://goreleaser.com/customization/winget/>). It renders the
  manifest and, unless `skip_upload` is set, commits it to a fork and opens the pull request,
  which needs a token for that repository. With `skip_upload: true` the page says the package
  is "stored on the dist directory only". The entry is not in GoReleaser's deprecation list
  (<https://goreleaser.com/deprecations/>), read on the day of the change.
- GoReleaser 2.18.3, which `mise.toml`'s `goreleaser = "2"` resolves to today, rendered three
  files for a snapshot build, schema version 1.12.0, the latest one the Microsoft manifest page
  names (<https://learn.microsoft.com/en-us/windows/package-manager/package/manifest>): the
  version file, the installer file and the `en-US` default locale file. The installer file
  carries `InstallerType: zip`, `NestedInstallerType: portable`, the nested file `gitcd.exe`
  with the command alias `gitcd`, and both Windows archives with the sha256 of the checksums
  file. The entry still demands `repository.name`, even with `skip_upload`.
- `Git.Git` is the identifier of Git for Windows in `microsoft/winget-pkgs`, read from its
  version file there. The installer schema allows `PackageDependencies`
  (<https://github.com/microsoft/winget-pkgs/blob/master/doc/manifest/schema/1.12.0/installer.md>).
- The owner decided on a semi automatic path: the pipeline generates the manifest and attaches
  it, the owner files the pull request by hand, the submitter is the owner's personal account.
- The repository is public. The release workflow already mints a token scoped to the Homebrew tap
  and signs keyless (ADR 0003, ADR 0004).

## Decision

The release pipeline generates the winget manifest and attaches it. A human submits it.

- `.goreleaser.yaml` has a `winget` entry with `skip_upload: true`. The three files are
  rendered into `dist/winget/` in the same run as the archives, so their checksums are those of
  the published archives.
- `scripts/publish-release-assets.sh` refuses an incomplete set and uploads the three files as
  they are, not as one zip: winget's tooling and the repository layout want the three files,
  and each stays downloadable alone. A release then has twelve assets.
- The package identifier is `OstendorfCloud.gitcd`, the publisher `Ostendorf Cloud`, the
  moniker `gitcd`, the license `Apache-2.0`.
- The manifest declares `Git.Git` as a dependency, since the commands that touch repositories
  shell out to git.
- `scripts/check-winget-manifest.sh` verifies every generated line against a skeleton, the
  URLs against this repository's release downloads and the sha256 values against the checksums
  file. `task test:winget` runs it on a snapshot build, and CI runs that target as its own job.
- No token for `microsoft/winget-pkgs` and no fork push exist in the pipeline. No workflow step
  contacts that repository.
- The `repository` block of the `winget` entry names `microsoft/winget-pkgs` because
  GoReleaser requires a name. It is never contacted.

## Consequences

- Every version needs a manual pull request by the owner, until a later decision automates it.
  The steps are in `docs/install.md`.
- The first pull request waits for the first release: the manifest points at published
  assets.
- A manifest the winget validation pipeline rejects shows up only after the release. The
  snapshot check proves the shape, not that winget accepts it. `winget validate` needs Windows
  and was not run when this was written.
- `ReleaseDate` is the day the build ran, GoReleaser's own value.
- The manifest cannot turn on the shell integration. Windows users add one line to their
  PowerShell profile, see `docs/install.md`.
- `wingetcreate update` is an alternative for later versions. How it handles a zip with a
  nested portable file was not found in the pages read and was not run.
- The manifest files are not signed themselves. Their sha256 values equal the signed
  checksums file, which ADR 0004 covers. Two runs of `scripts/check-winget-manifest.sh` stand
  in for a signature: the release script runs it with the tag and checksums file of its own
  run before it attaches anything, and the owner runs it on the downloaded files against the
  verified checksums file before submitting (`docs/install.md`).
- A pre-release tag gets a manifest attached like any other, the publish script does not treat
  tags differently. The owner submits stable releases only, as the `homebrew` job skips
  pre-releases. The winget-pkgs policy for pre-release versions was not looked up.
- For the infrastructure repository: the identifier and publisher name above. No secret,
  token, app or permission is added.
- Revisit when the number of releases makes the manual pull request a burden.

## Alternatives

- GoReleaser opens the pull request itself (`skip_upload` off): needs a standing token for a
  fork and the foreign repository, the credential of a personal account or a new app. Lost to
  the decision against a credential for `microsoft/winget-pkgs`.
- A third party GitHub Action for winget releases, such as winget-releaser, in the workflow: the same
  credential, plus another third party action in the release path. Not evaluated further.
- Only `wingetcreate` by hand with no generated manifest: the checksums are then worked out from
  a second download, and nothing checks the manifest in CI. Lost.
- One zip of the three files as the asset: one download, but the owner must unpack it, and
  tools that take a file by name get nothing. Lost.

## References

- Issue 22. ADR 0003 for the formula, ADR 0004 for the signature.
- `.goreleaser.yaml`, `scripts/publish-release-assets.sh`, `scripts/check-winget-manifest.sh`.
- Submission steps and the publisher identity: `docs/install.md`.
