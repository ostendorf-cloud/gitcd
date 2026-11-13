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

## Development

```sh
task setup
task --list
task check
```

One target per check, one CI job per target. `check` only calls the small targets.

## License

Apache-2.0. See `LICENSE`.
