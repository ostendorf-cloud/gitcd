# gitcd

## Problem

- Cloning the same remote twice, in two different places, happens because there is no
  predictable location to clone into.
- A commit lands under the wrong `user.email` when a personal and a work identity share one
  machine, and nobody notices until it reaches a remote that cares.
- Two accounts on one forge need two SSH keys, which only a per-host SSH alias can select, and
  nothing ties that alias back to the right git identity today.
- Finding a clone again means remembering where it was put, or searching the filesystem by
  hand.
- The workaround today is a personal, undocumented set of shell aliases and `includeIf` blocks
  per machine, which does not travel and silently drifts.

## Goals

- A remote renders to one predictable path, configurable per host, and cloning the same remote
  twice is a no-op that lands on that path.
- Jumping to any clone by name, or by `host/owner/repo`, takes one shell command.
- The right `user.name`, `user.email` and signing key apply automatically, based on the
  remote, without a manual `git config` per clone.
- A repository whose identity cannot be determined refuses to commit rather than inventing one.
- A fleet of clones can be inspected for drift, branch state and staleness from one command
  each.

## Non goals

- Not a general git porcelain: no wrapping of `commit`, `push`, `merge` or any command git
  already does well.
- No fuzzy discovery of remotes that were never cloned through gitcd; it manages what it put
  somewhere, plus what a user explicitly adopts.
- No secret or token storage; gitcd writes paths and git config keys only.
- No GPG-specific code: signing is SSH signing, a plain git config key gitcd already writes.
- No daemon or background process; every command runs once and exits.

## Approach

- `internal/remote` parses the five accepted remote forms into host, owner, project and
  repository, resolving a configured host alias to its canonical host.
- `internal/layout` renders a path template against a parsed remote, and maps a path back to
  one, per host with a shared default.
- `internal/config` loads `~/.gitcd.yaml`, the file a person edits, separate from
  `~/.config/gitcd/`, the directory gitcd owns and regenerates.
- `internal/git` shells out to the git CLI for clone, fetch, status and config reads and
  writes, rather than linking a git implementation.
- `internal/gitconfig` renders the managed config files gitcd writes; `internal/profile`
  resolves which profile and rule apply to a repository. Two independent mechanisms apply an
  identity: a generated `includeIf` config for the common case, and a pinned local config
  write for the explicit override, so a repository always has one clear, inspectable reason
  for the identity it carries.
- `internal/scan` walks the configured root into repository values for `list`, `doctor` and
  `status` to consume, treating a `.git` file, not only a `.git` directory, as a repository
  boundary, so a linked worktree is never descended into by mistake.
- `internal/shell` renders the per shell snippet that wraps the binary, so `clone` and `path`
  change the shell's directory instead of only printing it.
- Every command runs locally, reads its configuration once, and performs its own writes; the
  core packages themselves never write anything, only render what the command layer writes.

## Alternatives

- An existing clone manager, `ghq` and similar: handles the path layout, carries no notion of
  git identity, and adding one would mean patching a tool not meant for it.
- Hand written `includeIf` blocks per machine: works, but is not reviewable, does not travel
  between machines, and drifts the moment one block is edited by hand.
- GPG signing instead of SSH signing: would need its own key management gitcd does not
  otherwise require, where SSH signing reuses a key most of this tool's users already carry.
- A background daemon watching the filesystem for new clones: adds an operational component,
  a crash mode and a permissions question for no benefit over running `gitcd clone` once.

## Open questions

- Repositories outside `root` are reachable through `hasconfig` regardless of being managed.
  Should `list` and `status` find them too, and from what index. Next step: revisit once `list`
  ships in M2.
- Should `cd` prefer an installed `fzf` over the built in picker. Next step: deferred until the
  built in picker has seen real use.
- Is a `migrate` command for a changed layout template worth it, or is running `adopt` in a
  loop enough. Next step: decide while building M3's `adopt`.
- Does a profile need a per host `core.sshCommand` to pin an identity file, or do SSH host
  aliases already cover it. Next step: settle while building M1b's profile resolver.
- Should gitcd manage an allowed signers file for SSH signature verification. Next step: open
  during M1b if a user asks for verification, not before.
- Whether `url.<base>.insteadOf` rewriting happens before `hasconfig` matching is unverified.
  Next step: probe it in M1b, since it changes which URL forms a rule must emit.
- Azure DevOps URL forms, SSH host `ssh.dev.azure.com` with path `v3/{org}/{project}/{repo}`,
  and HTTPS `dev.azure.com/{org}/{project}/_git/{repo}`, are implemented but unverified against
  a real remote. Next step: confirm against an actual Azure DevOps repository.
