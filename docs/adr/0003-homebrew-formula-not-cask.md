# 3. Publish a Homebrew formula, not a cask

Status: accepted

## Context

- `brew install ostendorf-cloud/tap/gitcd` must work on macOS and Linux (issue 50).
- GoReleaser marks `brews` deprecated: "Deprecated in v2.10. Homebrew Casks should be used
  instead." (<https://goreleaser.com/customization/homebrew_formulas/>). The deprecations page
  heads the entry "since v2.10 (soft) since v2.16" (<https://goreleaser.com/deprecations/>),
  read as soft since v2.10 and a hard deprecation since v2.16. What the second marker changes
  was not checked further. `mise.toml` pins `goreleaser = "2"`, which resolves to 2.18.3 today.
  That version prints the notice and `goreleaser check` exits 2 for it.
- GoReleaser's own deprecation text says formulas were only needed "for Linuxbrew at the time",
  which is "no longer true". The `homebrew_casks` page never mentions Linux
  (<https://goreleaser.com/customization/homebrew_casks/>).
- Homebrew's Cask Cookbook allows cross platform casks: `binary` "works on either operating
  system", `on_linux` blocks exist (<https://docs.brew.sh/Cask-Cookbook>). A snapshot run with
  `homebrew_casks` renders `on_macos` and `on_linux` blocks for all four archives.
- The `homebrew_casks` page says casks "are supposed to be signed, even if they are coming from
  a tap" and shows a post install hook that removes the macOS quarantine bit. gitcd binaries are
  not signed yet (issue 44).
- Not verified: that `brew install --cask` of such a cask installs on Linux, and how
  Gatekeeper treats the unsigned binary through a cask. Nobody ran either.

## Decision

gitcd publishes a Homebrew formula (`brews`) to `ostendorf-cloud/homebrew-tap`, `Formula/gitcd.rb`.

- A formula installs a plain binary the same way on both systems, with no quarantine hook and no
  signing expectation, which fits an unsigned binary today.
- The Linux path through a formula is the long established one, a cask on Linux is not proven here.
- The premise "casks are macOS only" does not hold, so Linux alone is not the reason. Signing is.
- The deprecation is accepted knowingly, see the consequences.

## Consequences

- `goreleaser check` exits 2 for the deprecated `brews` key. `task lint:goreleaser` accepts
  that exit only when the `brews` notice is the single deprecation reported. Any other
  deprecation or an invalid file still fails.
- GoReleaser removes deprecated options only on major versions ("Deprecated options are only
  removed on major versions of GoReleaser", <https://goreleaser.com/deprecations/>). With the pin
  at the major `2`, `brews` keeps working until a v3 is adopted. The config may then be invalid
  and `task lint:goreleaser` fails visibly.
- The formula depends on `git` (`depends_on "git"`), since the commands that touch repositories
  shell out to it. The formula's own `test do` block only runs `gitcd version` and does not need git.
- Revisit when issue 44 lands: a signed and notarised binary removes the main reason against a
  cask. Moving is a new ADR, a `homebrew_casks` entry and deleting `Formula/gitcd.rb` from the tap.
- Users who run `brew install --cask` get nothing. The formula is the only install path.

## Alternatives

- `homebrew_casks`: GoReleaser's recommendation and renders Linux blocks. Lost because gitcd is
  unsigned, because the Linux install through a cask is unverified, and because the owner chose
  the formula.
- Both a formula and a cask: two files for one tool, `brew install` ambiguity, double upkeep.
- No tap, a hand written formula in the tap: the tap's own rules forbid hand edited formulae.

## References

- The tap's rules: `README.md` and `AGENTS.md` of `ostendorf-cloud/homebrew-tap`.
- Release flow: `.github/workflows/release.yaml`, `scripts/publish-homebrew-formula.sh`.
