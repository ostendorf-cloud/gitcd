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
- `internal/shell/`: the per shell init snippet `shell-init` prints.

`list`, `cd`, `doctor`, `adopt`, `status` and `sync` are later milestones, tracked as issues;
their own packages land with them.

## Commands

```sh
task setup           # mise install
task check           # every check, one after another
task --list          # one target per check: lint:*, test:*, build
task run -- clone github.com/acme/widget   # run the CLI from source
```

- One task target per check and one CI job per target. `check` aggregates them and holds no
  command of its own. CI calls the small targets, never `check`.

## Constraints

- Standard library plus `gopkg.in/yaml.v3`, the parser `release-me` already uses. Every other
  dependency needs a reason in the pull request.
- No CLI framework. `flag` and hand written dispatch, as in `release-me`.
- A command writes nothing but its result to stdout; everything else, including every log
  line, goes to stderr. `clone` and `path` print the resolved path on stdout, so a shell
  function can wrap them.
- Every command that writes supports `--dry-run`.
- Tests: table driven, git repositories generated under a temporary directory, `HOME`,
  `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_NOSYSTEM=1` controlled, no network.
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

- None.
