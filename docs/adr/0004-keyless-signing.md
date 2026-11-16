# 4. Sign the release checksums keyless with cosign

Status: accepted

## Context

- A release publishes six archives, a checksums file and the Homebrew formula. The checksums
  prove the archives match each other. Nothing proves where the checksums came from (issue 44).
- Homebrew verifies the formula's own checksums, so the tap is covered. A direct archive
  download is not.
- The repository is public. The release workflow runs on GitHub Actions and holds a short lived
  OIDC token when a job asks for `id-token: write`
  (<https://docs.github.com/en/actions/reference/security/oidc>).
- GoReleaser signs through a `signs` entry and documents a cosign example with
  `--bundle=${signature}` and a `.sigstore.json` suffix (<https://goreleaser.com/customization/sign/>).
- cosign recommends the bundle for blobs: "using a bundle is the recommended way of signing a
  blob" (<https://docs.sigstore.dev/cosign/signing/signing_with_blobs/>). The cosign 3.1.1
  release notes say the bundle format became the default for signing and verification
  (<https://github.com/sigstore/cosign/releases>).
- `cosign verify-blob` needs `--certificate-identity` or `--certificate-identity-regexp`, and
  `--certificate-oidc-issuer` or its regexp form, for a keyless signature
  (<https://github.com/sigstore/cosign/blob/main/doc/cosign_verify-blob.md>).
- Fulcio builds the certificate's subject alternative name for GitHub Actions from the template
  `{{ .url }}/{{ .job_workflow_ref }}` with `url` set to `https://github.com`
  (<https://github.com/sigstore/fulcio/blob/main/config/identity/config.yaml>).
- `release.yaml` also runs its release job on `pull_request` label events, and release-me
  resumes a draft whose pull request is labeled `release: tagged`.
- Out of scope: reproducible builds, signing the formula or the winget manifest.

## Decision

The release job signs the checksums file with `cosign sign-blob` in keyless mode, through the
`signs` entry in `.goreleaser.yaml`, verifies the bundle, and publishes it as a release asset.

- No key exists. Fulcio issues a short lived certificate bound to the workflow identity, and the
  signature is recorded in the public Rekor log.
- Only the checksums file is signed. The archives are covered through the hashes it lists.
- cosign is pinned in `mise.toml` as `3.1`, installed through mise like every other tool.
- The identity a verifier expects, with the issuer `https://token.actions.githubusercontent.com`:
  `https://github.com/ostendorf-cloud/gitcd/.github/workflows/release.yaml@refs/heads/main`.
  The run is a push to `main` and release-me creates the tag during it, so the tag is not part
  of the identity.
- `scripts/publish-release-assets.sh` verifies the bundle before it attaches or publishes
  anything, with that exact identity built from `GITHUB_REPOSITORY`, the fixed workflow path
  and the fixed branch. It does not read `GITHUB_WORKFLOW_REF`, which would accept the identity
  of whatever run is executing.
- The script finds `cosign` on `PATH` and offers no variable to skip or redirect the check, so
  no unrelated setting can turn it off. A test puts a fake `cosign` first on `PATH`.
- A run from another ref, such as a pull request label run, signs as
  `refs/pull/<n>/merge`, fails the check and publishes nothing. The draft stays. The failed
  push run is re-run, not a label run (`docs/install.md`).
- No regular expression identity is used, so no other ref or workflow file passes.

## Consequences

- The release job gets `id-token: write`. The `homebrew` job does not.
- That permission makes the token available to every step of the release job:
  `actions/create-github-app-token`, `actions/checkout`, release-me, `jdx/mise-action`,
  goreleaser, go and cosign. All actions are pinned to a commit digest. GoReleaser passes its
  environment on to cosign.
- The mise step installs only `go goreleaser cosign` (`install_args`, an input of
  `jdx/mise-action` at the pinned digest), so the job downloads no other tool from `mise.toml`.
- No secret is added. Nothing for the infrastructure repository to create, store or rotate.
  Record there: the identity above, the issuer, the Rekor log and the job permission.
- The Rekor entry is public. The repository is public as well, so nothing new is disclosed.
- A reader needs the cosign CLI to verify. Without it the checksums stay unproven.
- The checks do not tie the files to a version. The archive names carry no version, the
  checksums file lists none, and only its file name does. A genuine older release passes too.
  Substituting one, or a downgrade, is not prevented.
- `cosign verify-blob` has `--certificate-github-workflow-sha`, the commit the run was based
  upon. It can narrow a check to the commit of a tag, usually the run's commit. A draft resumed
  by a later push to `main` carries that later commit. Not tried on a real release.
- Renaming or moving `release.yaml`, or releasing from another branch, changes the identity.
  Commands for older releases keep the old identity, new releases need the new one, and the
  script's check and the documented command change together.
- A signing or verification failure fails the run before any upload. The draft release stays
  and the push run is repeated.
- A compromised workflow signs whatever it builds. The signature proves origin, not safety.
- A snapshot or local build has no OIDC token and runs with `--skip=sign`. A snapshot without
  the skip tries to run cosign, seen with goreleaser 2.18.3 and no cosign installed. Whether a
  real cosign would then fail or start an interactive login is not verified.
- The cosign 3.1.2 release notes say it "may be the last Cosign v3.1 release". 3.1.3 and 3.1.4
  have shipped since, and v4 is announced to remove deprecated flags
  (<https://github.com/sigstore/cosign/releases>). Revisit the flags when v4 arrives.
- Only the first real release shows: the actual identity in the bundle, that the documented
  command passes, that cosign finds the OIDC token inside GoReleaser's subprocess, and that
  mise resolves the `3.1` pin with `install_args`.
- Landing signing does not settle formula versus cask, see ADR 0003.

## Alternatives

- A managed key (cosign key pair or minisign): long lived secret to store, rotate and protect,
  and verifiers need the public key from a trusted channel. Lost to keyless.
- GitHub artifact attestations: ties the verification to the `gh` CLI and GitHub's API, and
  the owner chose cosign. Not evaluated further.
- No signature: leaves direct downloads unverifiable. Lost.
- Separate `.sig` and `.pem` files: older output shape. The bundle is the documented
  recommendation, one file instead of two.
- A regular expression identity: would also accept the label run. Lost to the exact identity.

## References

- Issue 44. ADR 0003 for the Homebrew formula.
- `.goreleaser.yaml`, `.github/workflows/release.yaml`, `scripts/publish-release-assets.sh`.
- Verification steps for readers: `docs/install.md`.
