# gitcd

Go CLI that clones repositories into a predictable path layout, applies a git identity
profile per remote, and lets the shell jump to any of them. The baseline rules apply and are
not repeated here.

## Layout

- `cmd/gitcd/`: the entry point, flags and command dispatch only.
- `internal/config/`: loads `~/.gitcd.yaml` or its XDG fallback, unknown keys rejected; also the
  identity profiles, rules and default profile the file carries.
- `internal/remote/`: parses a git remote into host, owner, project and repository, host
  aliases resolved.
- `internal/layout/`: renders a path template against a remote and maps a path back to one.
- `internal/git/`: the git CLI wrapper: clone, reading back a clone's own remote, and every
  identity related git config read and write.
- `internal/profile/`: resolves a remote to the profile its rules expect, and inspects a
  repository on disk for the profile it actually carries.
- `internal/gitconfig/`: renders the profile files and the include file `apply` writes; it
  never writes a file itself.
- `internal/status/`: reads one repository's branch, dirty and ahead or behind state out of
  `git status --porcelain=v2 --branch`, an unborn HEAD and a branch with no upstream rendered
  as an explicit state rather than a missing field.
- `internal/gitsync/`: fetches a set of repositories through a bounded pool of workers; never
  pulls, rebases or merges. Named apart from `sync` so no caller shadows the standard library
  package of that name.
- `internal/doctor/`: detects identity drift, a repository at a path the layout would not
  render, a missing or stale managed file and a global identity key. Detection only; the
  command layer writes.
- `internal/scan/`: the one walk over the configured root every command that takes a
  repository query consumes, stopping at a repository boundary, a `.git` entry, file or
  directory alike, and asking git for the linked worktrees that boundary skips past.
- `internal/shell/`: the per shell init snippet `shell-init` prints, including the `cd` tab
  completion registered for zsh and fish.
- `scripts/`: CI glue specific to this repository's own workflows, such as attaching and
  publishing release assets after `release.yaml` drafts a release.

Whether a linked worktree counts as one of the repositories a command reports is a single,
stated rule, not a separate choice per command: `cmd/gitcd/repos.go`'s `scanRepositories` doc
comment names it. `list` hides a worktree unless `--worktrees` is given; `cd`, `path` and
`open` resolve a query to one just as readily as to its main checkout; `status` gives one its
own row, since its branch and dirty state are its own; `sync` and `doctor` skip one, since it
shares its main checkout's remote and git configuration and checking or fetching it again
would only repeat that pass.

## Commands

```sh
task setup           # mise install
task check           # every check, one after another
task --list          # one target per check: lint:*, test:*, build
task run -- clone github.com/acme/widget   # run the CLI from source
```

- One task target per check and one CI job per target. `check` aggregates them and holds no
  command of its own. CI calls the small targets, never `check`.
- Supported systems: Linux, macOS and Windows. The `test` job in `checks.yaml` runs
  `task test:go` on all three. Every other job runs on Linux only.
- Two license checks, both in `check`:
  - `lint:license` runs `addlicense -check` over every `.go` and `.sh` file, the generated
    `internal/config/gitkeys.go` included, since its generator writes the header. It catches a
    missing or malformed header. It cannot catch a header that names another license.
  - `lint:license-spdx` runs `scripts/check-license-spdx.sh` over the same files. It compares
    each file's SPDX identifier with the license `LICENSE` holds, so a wrong identifier or a
    swapped `LICENSE` fails. It cannot catch a missing header, `lint:license` does that, and it
    reads only the first 10 lines. It knows Apache-2.0, Elastic-2.0 and MIT. Another `LICENSE`
    fails it until the script learns the new one.

## Constraints

- Standard library plus `gopkg.in/yaml.v3`, the parser `release-me` already uses. Every other
  dependency needs a reason in the pull request.
- No CLI framework. `flag` and hand written dispatch, as in `release-me`.
- A command writes nothing but its result to stdout; everything else, including every log
  line, goes to stderr. `clone`, `path` and `cd` print the resolved path on stdout, so a shell
  function can wrap them.
- Every command that writes supports `--dry-run`.
- Tests: table driven, git repositories generated under a temporary directory, `HOME`,
  `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_NOSYSTEM=1` controlled, no network. A test sets the home
  directory through `internal/testenv.SetHome`, never `t.Setenv("HOME", ...)` alone: Windows
  reads `USERPROFILE`. Test paths must be absolute on Windows too, and a test asserting Unix
  permission bits or a symlink skips on Windows with a reason.
- Errors wrap with `%w` and name the component.
- Logging through `log/slog`, configured in `main`, quiet by default, `--verbose` for debug.
- Secrets never reach a log line or an error message gitcd constructs itself.
- License header first in every `.go` file, a blank line, then the package comment where one
  exists:

```text
// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0
```

## Deviations

- The `license` job in `checks.yaml` calls `ostendorf-cloud/workflows`' `reusable-license.yaml`
  instead of `task lint:license`. The shared workflow pins addlicense itself. `task
  lint:license` stays and runs the same tool, flags and files locally.
- The `renovate-config` job in `checks.yaml` calls `reusable-renovate-config.yaml` and has no
  local Task target. The shared workflow runs Renovate through npm and needs the network, so a
  local copy would add a second pin to keep in step. Only CI validates `renovate.json`.

- yamllint's line-length limit is 140, not the default 120. A pinned reusable workflow
  reference needs the full line for its commit digest and version comment. Most of the
  organisation's repositories already run at 140.
- `scripts/publish-release-assets.sh` is adapted from release-me's script of the same name and
  stays in this repository instead of the shared scripts repository: a release path must not
  fetch a script from another repository at release time, where a failure burns a version
  number permanently. Keep it in step with release-me's copy; a third consumer moves it to the
  shared repository.
