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

## Release archives

Every release on GitHub carries a `tar.gz` per Linux and macOS system and a `zip` for Windows,
for `amd64` and `arm64`, with a checksums file. It also carries `gitcd.rb`, the formula the
build generated. The pipeline pushes that file to the tap, `brew` does not read it from there.

## Release pipeline

For maintainers. `release.yaml` runs these in order:

1. `release-me` tags and drafts the release.
2. `scripts/publish-release-assets.sh` builds the archives and the formula in one goreleaser
   run, records the formula's sha256 as the `formula-hashes` job output, attaches the files,
   then publishes the release.
3. The `homebrew` job runs `scripts/publish-homebrew-formula.sh`. It reads the formula back from
   the published release and verifies it against the build: the sha256 must be the recorded one,
   and every line must be part of the generated template
   (`scripts/check-homebrew-formula.sh`). Only then it commits the formula to the tap, with a
   token scoped to the tap alone.
4. The push is skipped when the tap already holds the same file, and refused when the tap holds a
   newer version.

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
