# gitcd

Clone repositories into a predictable path layout, apply a git identity profile per remote, and
jump to any of them from the shell.

## What it does

- Clones a remote into a path rendered from a template, `~/Code/github.com/owner/repo` by
  default, and does nothing when it is already there.
- Resolves a partial repository name, or a `host/owner/repo` shorthand, to its path. A shell
  function turns that into a `cd`.
- Applies the right `user.name`, `user.email` and signing key per remote through generated git
  configuration, and reports drift from it.
- Lists, inspects and fetches every repository under one root, and moves an existing clone into
  the layout.
- Accepts an SSH URL, an HTTPS URL, the scp style `git@host:owner/repo.git`, the Azure DevOps
  `_git` form and the bare `host/owner/repo` shorthand. A configured host alias resolves to its
  canonical host.

## Install

A release carries the archives, the checksums file and its signature bundle. The archives are
named `gitcd_<os>_<arch>` with `darwin`, `linux` or `windows` and `amd64` or `arm64`, `.tar.gz`
for Linux and macOS and `.zip` for Windows. The full list is in
[docs/install.md](docs/install.md#release-archives). Pick one route:

- Release archive: download the archive for your system, unpack it and put `gitcd` on your
  `PATH`. [docs/install.md](docs/install.md#verify-a-download) has the verification steps.
- Homebrew, on macOS and Linux:

  ```sh
  brew install ostendorf-cloud/tap/gitcd
  ```

- winget, on Windows:

  ```powershell
  winget install OstendorfCloud.gitcd
  ```

  winget needs the package accepted in the winget package repository. The steps are in
  [docs/install.md](docs/install.md#winget).
- From source, with [mise](https://mise.jdx.dev) installed. mise installs `task` and Go:

  ```sh
  mise trust
  mise install
  task build
  ```

  Run `mise x -- task build` when mise is not activated in the shell. `task build` writes
  `bin/gitcd`, built with the Go version `mise.toml` pins. `go build -o bin/gitcd ./cmd/gitcd`
  does the same with any Go that `go.mod` allows, which names the minimum version.
  The release pipeline sets the version, the commit and the build date.
  A binary built from source reports `gitcd dev (commit none, built unknown)`.

gitcd runs `git` for every command that touches a repository. Homebrew and winget install it as
a dependency.

### Shell integration

`clone`, `path` and `cd` print a path. A shell function around the binary turns that into a `cd`.
Add the line for your shell to its startup file:

```sh
eval "$(gitcd shell-init zsh)"    # ~/.zshrc
eval "$(gitcd shell-init bash)"   # ~/.bashrc
gitcd shell-init fish | source    # ~/.config/fish/config.fish
```

```powershell
Invoke-Expression (gitcd shell-init pwsh | Out-String)   # $PROFILE, PowerShell 7
```

- Windows PowerShell 5.1 uses `powershell` in place of `pwsh`.
- A winget install does not change the shell. Add the PowerShell line to `$PROFILE`
  (`notepad $PROFILE`).
- zsh, fish and PowerShell also get tab completion for `cd`.

## Use

```sh
gitcd clone github.com/acme/widget   # clone into ~/Code/github.com/acme/widget
gitcd path widget                    # the path of a repository cloned by its exact name
gitcd cd widget                      # the path of a partial name, a picker when it is ambiguous
gitcd status                         # branch, dirty and ahead or behind state of every clone
```

```console
$ gitcd status
NAME                    BRANCH  STATE  SYNC
github.com/acme/widget  main    clean  no upstream
```

Every command prints its result on stdout and everything else, including every log line, on
stderr. `--verbose` enables debug logging. `--dry-run` previews a command that writes.

| Command | Does |
| --- | --- |
| `clone <remote>` | Clones into the rendered path and prints it. A second run prints the path again. |
| `path <query>` | Prints the path of one cloned repository. |
| `cd <query>` | Matches a partial name and prints the path. |
| `list` | Prints every repository under the root. `--json` and `--worktrees` change the output. |
| `status` | Prints one row per repository. `--strict` exits non zero when any is dirty. |
| `sync` | Fetches every repository. It never pulls, rebases or merges. |
| `open [query]` | Opens the remote in a browser. `--print` prints the URL. |
| `init` | Adds the include line to the global git configuration. |
| `apply` | Renders the profile files and the include file. |
| `profile set\|unset\|show` | Pins, removes or inspects the profile of one repository. |
| `doctor` | Reports identity and layout drift. `--fix` repairs what is mechanical. |
| `adopt <path>` | Moves an existing clone into the layout. |
| `shell-init <shell>` | Prints the shell integration snippet. |
| `version` | Prints the version. `gitcd --version` does the same. |

Flags, exit codes and examples for each command: [docs/commands.md](docs/commands.md).

### Identity profiles

Name an identity in `~/.gitcd.yaml`, say which remotes use it, then run two commands:

```sh
gitcd init    # once per machine
gitcd apply   # after every change to the profiles or rules
```

- `init` adds one `include.path` line to the global git configuration. The line points at
  `~/.config/gitcd/includes.gitconfig`.
- `apply` renders that include file and one file per profile. The include file picks a profile by
  the repository path and by the remote URL.
- `gitcd profile set <name>` pins a profile in one repository's local configuration instead.
- `gitcd profile show` prints the profile a repository should carry and where its identity comes
  from.
- `user.useConfigOnly` only stops git from guessing a name and an email. It does not suppress an
  identity set directly in the global git configuration. Such a value still applies where no
  profile sets the key, and everywhere when it sits after the include line. `init` reports such
  keys and moves them into a profile on request.

[docs/configuration.md](docs/configuration.md#identity-git-config-and-apply) explains the
mechanism in full.

## Configuration

`~/.gitcd.yaml`, or `$XDG_CONFIG_HOME/gitcd/config.yaml` when that file does not exist. Every
key is optional. An unknown key anywhere in the file is rejected.

```yaml
root: ~/Code

layout:
  default: "{host}/{owner}/{repo}"

hosts:
  github.com:
    aliases: [github-work]
    protocol: ssh

profiles:
  private:
    user.name: Jane Doe
    user.email: jane@example.com
  work:
    user.name: Jane Doe
    user.email: jane@acme.example

rules:
  - host: github.com
    owner: acme
    profile: work

defaultProfile: private
```

| Key | Purpose | Default |
| --- | --- | --- |
| `root` | Directory every repository is cloned under. | `~/Code` |
| `layout.default` | Path template for a host without its own. | `{host}/{owner}/{repo}` |
| `hosts.<host>.layout` | This host's own path template. | `layout.default`, `{host}/{org}/{project}/{repo}` for `dev.azure.com` |
| `hosts.<host>.aliases` | Further host names, an SSH host alias included. | none, `ssh.dev.azure.com` and `vs-ssh.visualstudio.com` for `dev.azure.com` |
| `hosts.<host>.protocol` | Protocol `clone` builds for a shorthand: `ssh` or `https`. | `https` |
| `profiles.<name>` | Git config keys the profile sets. | none |
| `rules` | Maps `host`, `owner` and `repo` to a profile. The most specific rule wins. | none |
| `defaultProfile` | Profile for a remote no rule matches. | none |
| `useConfigOnly` | Writes `user.useConfigOnly = true` into the generated files. | `true` |
| `allowUnsafeKeys` | Profile keys that run a command or decide what git trusts, opted in by name. | none |

- A profile key that runs a command or decides what git trusts is refused until
  `allowUnsafeKeys` names it. The refusal names the key and the remedy.
- The reference for every key, the rules for names and values, and the gate on profile keys:
  [docs/configuration.md](docs/configuration.md).

## Development

```sh
mise trust     # once per clone
mise install   # the pinned toolchain, task included
task --list    # one target per check
task check     # every check, one after another
task run -- clone --dry-run github.com/acme/widget
```

- One target per check, one CI job per target. `check` only calls the small targets.
- Supported systems are Linux, macOS and Windows. CI runs the Go tests on all three. Lint, vet
  and the shell script checks run on Linux only.
- Every commit needs a DCO sign-off. See [CONTRIBUTING.md](CONTRIBUTING.md).
- To report a vulnerability, see [SECURITY.md](SECURITY.md).

## License

Apache-2.0. See [LICENSE](LICENSE).
