# gitcd

Clone repositories into a predictable path layout, apply a git identity profile per remote, and
jump to any of them from the shell.

## What it does

- Clones a remote into a path rendered from a template, `~/Code/github.com/owner/repo` by
  default, with the template configurable per host.
- Resolves a partial repository name, or a `host/owner/repo` shorthand, to its path, which a
  shell function turns into a `cd`.
- Accepts an SSH URL, an HTTPS URL, the scp style `git@host:owner/repo.git`, the Azure DevOps
  `_git` form, and the bare `host/owner/repo` shorthand, resolving a configured host alias to
  its canonical host either way.

Applying a git identity profile per remote is a later milestone, tracked as an issue; this
version clones and navigates only.

## Install

```sh
task setup
task build
```

Builds `bin/gitcd` from source. A packaged release is not cut yet; see the repository's issues
for where that stands.

## Use

```sh
gitcd clone github.com/acme/widget         # clone into ~/Code/github.com/acme/widget
gitcd clone git@github.com:acme/widget.git # equally, from any accepted remote form
gitcd path widget                          # the path of a repository cloned by its exact name
gitcd path github.com/acme/widget          # the path of a repository cloned under this host
eval "$(gitcd shell-init zsh)"             # wrap the binary so clone and path change directory
gitcd version
```

Every command prints its result on stdout and everything else, including every log line, on
stderr; `--verbose` enables debug logging and `--dry-run` previews a command that writes
without doing it. `clone` is idempotent: a target that already exists with the same remote
prints its path and exits 0, and one that exists with a different remote fails, naming both.

## Configuration

`~/.gitcd.yaml`, or `$XDG_CONFIG_HOME/gitcd/config.yaml` when that file does not exist.

```yaml
root: ~/Code

layout:
  default: "{host}/{owner}/{repo}"

hosts:
  dev.azure.com:
    layout: "{host}/{org}/{project}/{repo}"
    aliases: [ssh.dev.azure.com, vs-ssh.visualstudio.com]
  github.com:
    aliases: [github-work, github-private]
    protocol: ssh
```

| Key | Purpose | Default | Required |
| --- | --- | --- | --- |
| `root` | Directory every repository is cloned under. | `~/Code` | No |
| `layout.default` | Path template applied to a host without its own. | `{host}/{owner}/{repo}` | No |
| `hosts.<host>.layout` | This host's own path template. | `layout.default` | No |
| `hosts.<host>.aliases` | Further host names, an SSH host alias included, that resolve to this host. | none | No |
| `hosts.<host>.protocol` | Protocol `clone` builds for a `host/owner/repo` shorthand: `ssh` or `https`. | `https` | No |

A template is built from `{host}`, `{owner}` (`{org}` reads the same field, for an Azure DevOps
template), `{project}` and `{repo}`. An unknown key anywhere in the file is rejected.

A host built into the defaults, `dev.azure.com` today, already carries a layout and aliases.
A `hosts` entry for that host merges field by field onto the default entry: setting `aliases`
without `layout` keeps the default layout, and setting `layout` without `aliases` keeps the
default aliases. To clear a default's `aliases` deliberately, set it to an empty list,
`aliases: []`; to clear a default's `layout` deliberately, set it to an empty string,
`layout: ""`, which then falls back to `layout.default` exactly like a host with no `layout`
key at all. See `docs/adr/0001-host-config-merges-field-by-field.md`.

### What a profile name may be

A profile name becomes the name of the file `apply` renders that profile to, under
`~/.config/gitcd/profiles/`, and is written into the include file as part of a path. So it has
to be one file name, on every platform: letters, digits, `.`, `-` and `_`, beginning and ending
with a letter or a digit. A name that is empty, holds a path separator, names the current or a
parent directory, or is a name Windows reserves for a device is rejected.

The class differs from the one a profile key's section uses, in both directions. It allows `_`
and `.`, because a name is also what you type at `--profile work_eu`. It requires a letter or
digit at each end, because a leading `.` would hide the rendered file and a trailing `.` or
space is silently stripped by Windows, which would render two distinct profiles to one file.

A rejected name fails the whole file at load time, before any rule is looked at, so a rule
naming that profile never resolves and no command runs against a partly valid configuration.

### What a profile key may be

A profile key is a git config key in `section.key` or `section.subsection.key` form. The
section is a letter followed by letters, digits and `-`; the subsection and the key name hold
only letters, digits and `-`. The subsection is held to the same narrow class as the key name
because it is written into the `[section "subsection"]` header of the generated file, where a
wider character would put the keys after it under a section the profile does not name.

A value may hold anything but a control character git config has no escape for: a line break,
a tab and a backspace are written escaped and read back unchanged, a carriage return is
rejected.

A key under the `remote` section is rejected: a profile is applied through a condition that
reads the remote URL, so a profile setting one would decide its own condition.

### Keys a profile carries only when named

Some git config keys hand git a command to run, take over a credential prompt, or change which
URL git contacts. A profile is picked automatically per remote, so these are rejected unless
`allowUnsafeKeys` names the key:

- `core.sshCommand`, `core.pager`, `core.editor`, `core.fsmonitor`
- every key under `credential`
- `url.<base>.insteadOf` and `url.<base>.pushInsteadOf`
- any key whose name is `process` or `hook`, under any section

```yaml
allowUnsafeKeys:
  - core.pager

profiles:
  private:
    user.email: simon@example.test
    core.pager: less -FRX
```

Each entry names one key, matched the way git matches a key: the section and the key name
case-insensitively, the subsection exactly. A wildcard is not accepted, and an entry naming a
key that needs no opt in is rejected rather than ignored. `doctor` reports every key applied
this way, so the choice stays visible.

### What a rule field may be

A rule's `host`, `owner` and `repo` are written literally into the conditions of the generated
include file. Each holds only letters, digits, `.`, `-` and `_`, with `/` additionally allowed
in an `owner` so a group and subgroup can be named as one. A glob is not accepted in any of
them: leaving a field out already matches every value of that field, while a `*` in an `owner`
would silently apply the rule's identity and signing key to repositories it does not name.

## Development

```sh
task setup
task --list
task check
```

One target per check, one CI job per target. `check` only calls the small targets.

## License

Apache-2.0. See `LICENSE`.
