# Commands

Full reference for every `gitcd` command beyond the one-line descriptions in the
[README](../README.md#use).

Every command uses the same three exit codes: `0` success, `1` a runtime failure the error
message names, and `2` a usage error, a bad flag or the wrong number of arguments, printed
alongside the command's own usage line. `doctor`, `status --strict` and `sync` read `1` as
"something here needs attention", not only "gitcd itself failed": that is what lets a shell
prompt or a CI step gate on them without parsing a line of output.

A command writes nothing but its own result to stdout. Every log line, every progress message
and every error goes to stderr, so `clone`, `path` and `cd`, each of which prints a path on
success, can be wrapped by the shell function `shell-init` emits without that function ever
seeing anything on stdout but the path itself. `--verbose` adds debug logging on stderr; it
never changes what a command writes on stdout.

## clone

Clones a remote into the path the configured layout renders for it, printing that path on
stdout. `clone` is idempotent: a target that already exists with the same remote, compared by
its parsed fields rather than its literal text, prints that path and exits 0 without cloning
again; one that exists with a different remote fails, naming both.

- `--profile <name>` pins an identity profile at clone time, through `git clone -c`, rather than
  relying on the `includeIf` chain `gitcd apply` renders. Either way, the effective identity is
  read back and logged on stderr once the clone finishes: `expected profile`, `user.email` and
  `user.signingkey`, each with where the value came from.
- `--dry-run` prints the target path without cloning, skipped entirely when the target already
  exists, since an existing clone has nothing left to preview.
- `--verbose` enables debug logging on stderr.

```sh
$ gitcd clone github.com/acme/widget
/home/me/Code/github.com/acme/widget
```

Exit codes:

- `0`: cloned, or already cloned with a matching remote.
- `1`: the remote could not be parsed, the named `--profile` does not exist, the target exists
  with a different remote, or `git clone` itself failed.
- `2`: not exactly one remote or shorthand was given.

## path

Resolves one query to the path of a repository already cloned, printing it on stdout. A query
holding a `/` is read as a `host/owner/repo` shorthand, resolved by rendering the path it would
clone into and checking it already exists; any other query is an exact match, case sensitive,
against the final directory name of every repository under the configured root, a linked
worktree included, erroring when none or more than one matches rather than guessing. Fuzzy and
case insensitive matching belongs to `cd`.

- `--verbose` enables debug logging on stderr.

```sh
$ gitcd path widget
/home/me/Code/github.com/acme/widget
```

Exit codes:

- `0`: exactly one repository matched.
- `1`: the shorthand has not been cloned yet, the bare name matches none or more than one
  repository, or the configured root could not be searched.
- `2`: not exactly one query was given.

## list

Prints every repository under the configured root, one path per line, hiding a linked worktree
unless `--worktrees` is given: a worktree is not itself a distinct clone someone maintains in
the fleet listing.

- `--worktrees` includes a linked worktree, hidden by default.
- `--json` prints the same repositories as a JSON array instead, each entry carrying `path`,
  `remote` (omitted when it could not be read) and `worktree`.
- `--verbose` enables debug logging on stderr.

```sh
$ gitcd list
/home/me/Code/github.com/acme/widget
/home/me/Code/github.com/acme/other
```

A repository `list` could not fully inspect, most commonly one with no configured remote, is
skipped with a warning on stderr rather than failing the whole run.

Exit codes:

- `0`: printed, even when the root holds nothing to list.
- `1`: the configured root could not be searched.
- `2`: an argument was given; `list` takes none.

## cd

Resolves one query against every managed repository, a linked worktree included, and prints the
match on stdout: the command the shell function `shell-init` emits wraps to actually change
directory.

Resolution runs in two tiers. An exact match, against a repository's absolute path, its path
relative to the configured root, or its final directory name, case insensitively, always wins
outright, even when the query also substring-matches other repositories; when more than one
repository matches exactly, that exact set becomes the ambiguous candidates rather than falling
back further. Lacking any exact match, every repository whose path relative to root contains
the query as a case insensitive substring, anywhere, is a fuzzy match. An ambiguous result,
either tier, is resolved by a numbered picker printed on stderr: with a terminal on stdin, it
prompts for and reads a selection; without one, it exits non zero, the candidates already
listed on stderr.

- `--verbose` enables debug logging on stderr.

```sh
$ gitcd cd widget
/home/me/Code/github.com/acme/widget
```

Exit codes:

- `0`: exactly one repository matched, directly or through the picker.
- `1`: no repository matched the query, or more than one did and no selection could be read, an
  empty read, an out of range choice, or stdin was not an interactive terminal.
- `2`: not exactly one query was given.

## init

Adds the `include.path` line to the user's global git configuration that points at the include
file `gitcd apply` renders, so every identity profile rule actually takes effect. Before that,
it detects every identity key, `user.name`, `user.email`, `user.signingkey`, `gpg.format` and
`commit.gpgsign`, the global configuration sets directly, and reports each one: see
[the identity mechanism](configuration.md#identity-git-config-and-apply) for why that matters.

- `--move-identity yes|ask|no` decides what happens to a detected global identity key: `yes`
  moves it into a profile without asking, `ask` prompts on stderr and reads the answer from
  stdin, and `no`, the default, only reports it.
- `--profile <name>` names the profile a moved key is written into, default `global`. `init`
  refuses to use a name that is already `defaultProfile` or an existing profile, since either
  would silently fold the leaked identity into something it was not meant to join; it names the
  conflict so the run can be repeated with a different name.
- `--dry-run` prints what would change without writing anything.
- `--verbose` enables debug logging on stderr.

A move backs up both files it touches, the global git configuration and `~/.gitcd.yaml` (or its
XDG fallback), before writing either, with the backup path printed on stdout. `init` refuses
outright, before writing anything, when a global identity key already sits after the last
existing `[include]` section: `git config --global --add` inserts a new key into that section
rather than at the end of the file, and a key sitting after it would keep beating every
`includeIf` value `gitcd apply` adds, silently.

```sh
$ gitcd init
/home/me/.gitconfig sets user.name, user.email directly, outside any profile:
  user.name = Jane Doe
  user.email = jane@example.com
...
not moving them: pass --move-identity=yes to move them without a prompt, or --move-identity=ask to be asked
added include.path = /home/me/.config/gitcd/includes.gitconfig to /home/me/.gitconfig
run "gitcd apply" to render the profile files this include reads
```

Exit codes:

- `0`: the include line was added, already present, or a detected key was only reported.
- `1`: the chosen `--profile` name conflicts with `defaultProfile` or an existing profile, a
  global identity key already sits after an existing `[include]` section, or a file could not
  be read, backed up or written.
- `2`: `--move-identity` was not `yes`, `ask` or `no`, or an argument was given.

## apply

Renders every identity profile file and the include file `init` points the global git
configuration at, then writes them.

- `--dry-run` prints the rendered files instead of writing them.
- `--verbose` enables debug logging on stderr.

```sh
$ gitcd apply
/home/me/.config/gitcd/includes.gitconfig
/home/me/.config/gitcd/profiles/work.gitconfig
```

Exit codes:

- `0`: every file rendered and written (or printed, with `--dry-run`).
- `1`: a profile or the include file could not be rendered, or a file could not be written.
- `2`: an argument was given; `apply` takes none.

## profile

Pins, removes or inspects a repository's identity profile, through its three subcommands below.
Running `gitcd profile` with no subcommand, or an unrecognised one, prints the subcommand usage
on stderr and exits `2`.

### profile set

`gitcd profile set <name> [path]` writes profile `name`'s git config keys to `path`'s local
configuration, defaulting `path` to `.`, through `git -C path config --local`, plus the marker
`gitcd.profile = <name>` and the `gitcd.keys` list of keys it wrote, so `profile unset` later
knows exactly what to remove.

- `--dry-run` prints the keys that would be written without writing them.
- `--verbose` enables debug logging on stderr.

```sh
$ gitcd profile set work
work
```

Exit codes:

- `0`: the profile was pinned.
- `1`: no profile named `name` is configured, or a key could not be written.
- `2`: not one or two arguments were given.

### profile unset

`gitcd profile unset [path]` removes exactly the keys `profile set` recorded under `gitcd.keys`
for `path`, defaulting `path` to `.`, then the `gitcd.profile` and `gitcd.keys` markers
themselves. A recorded key that no longer carries a local value, config having drifted since
`profile set` wrote it, is reported as a leftover on stderr rather than failing the command
outright, though the command still exits non zero.

- `--dry-run` prints the keys that would be removed without removing them.
- `--verbose` enables debug logging on stderr.

```sh
gitcd profile unset
```

Exit codes:

- `0`: every recorded key, and both markers, were removed.
- `1`: `path` has no profile pinned through gitcd at all, a recorded key was already missing
  (reported as a leftover), or a key could not be removed.
- `2`: more than one argument was given.

### profile show

`gitcd profile show [path]` prints the profile the configured rules expect for `path`'s remote,
the specific rule that matched, if any, and the identity git actually resolves right now for
`user.email` and `user.signingkey`, each with its origin: pinned locally, applied through
`gitcd apply`, set elsewhere (outside any profile gitcd manages), or unset.

- `--verbose` enables debug logging on stderr.

```sh
$ gitcd profile show
remote: git@github.com:acme/widget.git
expected profile: work (rule: host="github.com" owner="acme")
user.email: work@example.com (a profile applied through gitcd apply)
user.signingkey: (unset)
```

Exit codes:

- `0`: the repository's identity state was read and printed.
- `1`: `path` is not a repository, or its remote or git metadata could not be read.
- `2`: more than one argument was given.

## status

Prints one row per repository under the configured root, a linked worktree included, since a
worktree carries its own branch and its own dirty state: `NAME`, `BRANCH` (`(detached)` for a
detached `HEAD`), `STATE` (`unborn`, `clean`, `dirty` or `error`) and `SYNC` (`up to date`,
`+<ahead> -<behind>`, `no upstream`, or `-` when no comparison could be made at all, an unborn
`HEAD` or a repository that failed to inspect).

- `--strict` makes `status` exit non zero when any repository is dirty, by a staged change, an
  unstaged change or an untracked file, so a script can gate on a clean fleet without parsing
  the table.
- `--json` prints the same information as a JSON array instead of the table, one object per
  repository, each carrying the fields above plus `detached`, `unborn`, `upstream`,
  `hasSyncComparison`, `inSync`, `ahead`, `behind`, `staged`, `unstaged`, `untracked`, `dirty`
  and `error` (present only when that repository failed to inspect).
- `--verbose` enables debug logging on stderr.

```sh
$ gitcd status
NAME                    BRANCH  STATE  SYNC
github.com/acme/widget  main    clean  up to date
```

A repository `status` could not inspect still gets a row, `STATE` `error` and `SYNC` `-`, with
the error also printed on stderr; this is the one failure mode that does not depend on
`--strict` at all.

Exit codes:

- `0`: every repository was inspected, and, with `--strict`, none was dirty.
- `1`: one or more repositories could not be inspected at all, or, with `--strict`, one or more
  was dirty.
- `2`: an argument was given; `status` takes none.

## sync

Fetches every repository under the configured root, excluding a linked worktree, through a
bounded pool of four workers, never pulling, rebasing or merging. One repository's fetch
failure is reported on stderr and does not stop the others.

- `--dry-run` prints the repositories that would be fetched, by name, on stdout, instead of
  fetching anything.
- `--verbose` enables debug logging on stderr.

```sh
$ gitcd sync
gitcd sync: github.com/acme/widget: fetched
gitcd sync: 0 of 1 repositories failed
```

Every result, success or failure, is logged on stderr as it finishes, followed by that summary
line, `<N> of <M> repositories failed`.

Exit codes:

- `0`: every repository fetched successfully, or the root holds none to fetch.
- `1`: one or more repositories failed to fetch.
- `2`: an argument was given; `sync` takes none.

## open

Opens one repository's remote in the platform's own browser command (`open` on macOS, `xdg-open`
on Linux, `start` through `cmd` on Windows); nothing it launches is itself a dependency gitcd
carries. `query`, defaulting to `.`, resolves exactly as `path` resolves one: a literal path or
`.` is used as given, a query holding a `/` is a `host/owner/repo` shorthand that must already
be cloned, and anything else is an exact repository name search, a linked worktree included.

- `--print` prints the URL on stdout instead of launching a browser. This is the only path a
  test exercises; launching a real browser is never something a test does.
- `--verbose` enables debug logging on stderr.

```sh
$ gitcd open --print widget
https://github.com/acme/widget
```

Exit codes:

- `0`: the URL was opened (or printed, with `--print`).
- `1`: the query could not be resolved to a cloned repository, its remote could not be read or
  parsed, or launching the browser failed.
- `2`: more than one argument was given.

## doctor

Reports identity and layout drift across every repository under the configured root, excluding
a linked worktree, which shares its main checkout's remote and local git configuration, plus the
managed files `gitcd apply` writes and the global git configuration. It always prints both
configuration homes, the configuration file's path and the managed directory, text output and
`--json` alike, so neither surprises whoever reads the report.

`doctor` reports: identity drift (a repository's effective identity differs from the profile its
rules imply), a repository at a path the configured layout would not render for its own remote,
including a case-only mismatch and an ancestor directory two repositories disagree about the
case of, a missing or stale managed file, and a global git identity key, which
`user.useConfigOnly` cannot suppress.

- `--fix` repairs only what is mechanical: a stale or missing managed file, re-rendered exactly
  as `gitcd apply` would, and a path that differs from the layout only in character case,
  renamed in place. Every other finding, identity drift, a stale pin, a structural path
  mismatch and every global identity key, is always reported and never touched: a value
  `doctor` did not write is never one it overwrites.
- `--dry-run`, with `--fix`, prints what would change without writing or moving anything.
- `--json` prints a structured object instead of the text report: `configPath`, `managedDir`,
  `fixed` and `findings`, each finding carrying its kind, the repository or path it is about,
  and a message. With `--fix --json`, every fix progress line that would otherwise go to stdout
  goes to stderr instead, so stdout carries nothing but the one encoded object.
- `--verbose` enables debug logging on stderr.

```sh
$ gitcd doctor
config file: /home/me/.gitcd.yaml
managed directory: /home/me/.config/gitcd
no findings
```

Exit codes:

- `0`: no finding is outstanding, after `--fix` repaired what it could when it was given.
- `1`: one or more findings remain outstanding.
- `2`: an argument was given; `doctor` takes none.

## adopt

Moves one existing clone, not necessarily made by gitcd, into the path the configured layout
renders for its own remote, fixes a stale `gitcd.profile` pin, and prints the old and the new
path, one per line, on stdout.

`adopt` refuses outright, before asking anything: a repository with `extensions.worktreeConfig`
enabled, since that setting lets a worktree carry its own copy of a config key gitcd's identity
writes normally assume are shared, and nothing in `adopt` resolves which of the two a given key
would need; a linked worktree itself, and a main checkout that still has one, since moving
either end without the other breaks the pair; and a bare repository.

- `--yes` moves without asking first. Without it, `adopt` asks on stderr and reads the answer
  from stdin; a closed or empty stdin, or anything other than `y` or `yes`, declines.
- `--dry-run` prints the old and the new path without moving or writing anything.
- `--verbose` enables debug logging on stderr.

Only one path is accepted per run; adopting a whole tree is running this in a loop.

```sh
$ gitcd adopt --yes ~/old/widget
/home/me/old/widget
/home/me/Code/github.com/acme/widget
gitcd adopt: expected profile: work (default profile, no rule matched)
gitcd adopt: user.email: work@example.com (a profile applied through gitcd apply)
gitcd adopt: user.signingkey: (unset)
```

Exit codes:

- `0`: the repository was adopted (or was already at the path the layout renders), including
  when it was already at the target with nothing left to move.
- `1`: the path is not a git repository, is bare, is a linked worktree or still has one, has
  `extensions.worktreeConfig` enabled, the target already exists, the move was declined or
  failed, or a configuration fix after the move failed.
- `2`: not exactly one path was given.

## shell-init

Prints the shell integration snippet for one shell on stdout: a `gitcd` function that shadows
the binary, calling through to the real one for every command and changing the shell's working
directory for `clone`, `path` and `cd` when the printed path actually exists. The zsh, fish and
PowerShell snippets also register tab completion for `cd`.

The one required argument is `zsh`, `bash`, `fish`, `pwsh` (PowerShell 7) or `powershell`
(Windows PowerShell 5.1); the two PowerShell editions get an identical snippet.

```sh
eval "$(gitcd shell-init zsh)"
```

```powershell
Invoke-Expression (gitcd shell-init pwsh | Out-String)
```

Exit codes:

- `0`: the snippet was printed.
- `1`: the argument was not one of the five recognised shells.
- `2`: not exactly one argument was given.

## version

Prints gitcd's own version on stdout, one line, and exits 0. It takes no flags.

The line carries the version, the commit it was built from and the build date, so a bug report
identifies exactly what was built:

```console
$ gitcd version
gitcd 0.3.0 (commit 9f2c1ab, built 2026-11-15T08:52:48Z)
```

All three are set at build time by the release pipeline. A binary built straight from source,
through `go build` or `task run -- version`, has none of them and says so rather than inventing
a value:

```console
$ gitcd version
gitcd dev (commit none, built unknown)
```
