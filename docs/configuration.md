# Configuration reference

Full reference for every `~/.gitcd.yaml` key. See the [README](../README.md#configuration) for
the quick example.

gitcd reads the first of `~/.gitcd.yaml` and `$XDG_CONFIG_HOME/gitcd/config.yaml`
(`~/.config/gitcd/config.yaml` when `XDG_CONFIG_HOME` is unset) that exists. Neither existing
yields the built in default: `root: ~/Code`, the default layout `{host}/{owner}/{repo}`, and one
built in `hosts` entry for `dev.azure.com`. An unknown key anywhere in the file is an error.

This is the file a person edits by hand. `~/.config/gitcd/` is a separate directory gitcd owns
and regenerates through `gitcd apply`; nothing under it is meant to be edited directly, and
`gitcd init --move-identity` is the one command that writes into this file on a person's behalf,
always backing it up first.

The managed directory is private to the user: `gitcd apply` and `gitcd init` create the
directories `0700` and the profile files and the include file `0600`, since they name
identities, signing keys and keys that change what git runs.

Which modes change on an existing installation:

- `gitcd apply` tightens every file it writes, the include file and one file per profile, to
  `0600`, and the managed directory and its `profiles` directory to `0700`. `gitcd init`
  tightens the managed directory the same way. gitcd owns and regenerates those paths, so no
  mode a person set on purpose is lost.
- A file `apply` does not write keeps its mode, for instance a file left behind by a profile
  that was removed. The `0700` directory still keeps others out of it.
- A symlink at a path `apply` writes is refused with an error. gitcd does not write through it,
  so the file it points at is neither changed nor given a new mode. A symlink as the managed
  directory itself is followed.
- This configuration file is not changed by either command.
- On Windows the private modes do not apply: access to the managed directory is governed by the
  ACLs it inherits from the user profile.

## `root`

The clone root every rendered path sits under, default `~/Code`. A leading `~` is expanded
against the loading user's home directory; any other value is used as written.

```yaml
root: ~/Code
```

## `layout.default`

The path template applied to a host with no `layout` key of its own, and the fallback a host's
own `layout` key falls back to when it is cleared, default `{host}/{owner}/{repo}`.

```yaml
layout:
  default: "{host}/{owner}/{repo}"
```

### Layout placeholders

A template is a `/` separated list of literal segments and placeholders: `{host}`, `{owner}`,
`{project}` and `{repo}`. `{org}` reads the same field as `{owner}`; the two names are
interchangeable, and a template may use either, which is why `dev.azure.com`'s own built in
layout, `{host}/{org}/{project}/{repo}`, reads more naturally with `{org}` than `{owner}` would.

`{project}` is set only for a remote whose path actually carries one, Azure DevOps today; a
template rendered against a remote with no project value fails if it uses `{project}`, and a
template with no `{project}` placeholder fails against a remote that does carry one, rather than
silently dropping it, which is how two different remotes could end up sharing one path. Every
other placeholder fails the same way when the field it names is empty on the remote being
rendered.

An owner may hold more than one `/` separated component, for a GitLab subgroup; every other
field must be the one path component it names. A rendered value can never hold `..`, an
absolute path, a reserved character, or anything else that would stop it naming exactly one
predictable directory under `root`.

## `hosts`

`hosts.<host>` configures one canonical host by its lowercased name: `github.com`,
`gitlab.example.com`, `dev.azure.com`. Each entry may set its own `layout`, `aliases` and
`protocol`; a key an entry leaves out keeps the built in default for that host, not the generic
`layout.default` fallback, so setting one field never silently drops another.

```yaml
hosts:
  github.com:
    aliases: [github-work]
    protocol: ssh
  dev.azure.com:
    aliases: [ssh.dev.azure.com, vs-ssh.visualstudio.com, dev.azure.com]
```

### `hosts.<host>.layout`

That host's own path template, overriding `layout.default`. Setting it to `""` explicitly
clears it, which is not the same as leaving the key out: see
[clearing a field deliberately](#clearing-a-field-deliberately).

### `hosts.<host>.aliases`

Further host names, lowercased, that resolve to this canonical host: an SSH config `Host` alias
bound to a distinct identity file, most commonly.

The map is not optional in practice, even though the key itself is. One forge account is reached
through one SSH key; a second account on the same forge needs a second key, which only a
distinct SSH host alias in `~/.ssh/config` can select, since the forge itself is reached at the
same hostname either way. gitcd's own remote parser resolves that alias back to the canonical
host before anything else runs, so a rule written against `github.com` still matches a remote
cloned through the alias, and the rendered path and the applied identity are both the canonical
host's:

```yaml
hosts:
  github.com:
    aliases: [github-work]
```

```text
# ~/.ssh/config
Host github-work
  HostName github.com
  IdentityFile ~/.ssh/id_ed25519_work
```

```sh
gitcd clone github-work/acme/widget
```

An alias list given at all, even `aliases: []`, replaces the host's default aliases outright
rather than adding to them; a left out `aliases` key keeps whatever default aliases that host
already carries, `dev.azure.com`'s built in two among them.

### `hosts.<host>.protocol`

The clone protocol this host prefers when a bare `host/owner/repo` shorthand is cloned, `ssh` or
`https`, default `https`. It has no effect on a remote already given as a full URL: `clone`
only consults it to build a URL out of a shorthand input in the first place.

## `profiles`

Named identities. Each key under a profile name is a git config key in `section.key` or
`section.subsection.key` form, mapped to the string value it should carry; a boolean is written
as the string `"true"` or `"false"`, exactly as it will appear in the rendered file.

```yaml
profiles:
  private:
    user.name: Jane Doe
    user.email: jane@example.com
    user.signingkey: ssh-ed25519 AAAA...
    gpg.format: ssh
    commit.gpgsign: "true"
  work:
    user.name: Jane Doe
    user.email: jane@work.example
```

A key under the `remote` section is rejected: a profile is reached through an `includeIf`
condition that itself depends on the remote's own URL, so a profile setting one would make that
condition depend on its own consequence.

A profile name is a file name: it becomes `~/.config/gitcd/profiles/<name>.gitconfig`, and it is
also what a person types at `gitcd profile set <name>` or in a rule's `profile` field.

## `rules`

Decides which `profiles` entry a remote resolves to. Each rule names a `host`, required, and may
narrow the match further with `owner` and, alongside `owner`, `repo`; an empty field matches
every value of that field, and the rule's `profile` must name an entry in `profiles`.

```yaml
rules:
  - host: dev.azure.com
    profile: work
  - host: github.com
    owner: ostendorf-cloud
    profile: private
  - host: github.com
    owner: acme
    repo: internal-tools
    profile: work
```

### Rule specificity

The most specific matching rule wins, ranked by how many fields narrow it: a host only rule is
least specific, a host and owner rule is more specific, and a host, owner and repo rule is most
specific. This ranking, not the order the file lists rules in, decides which one applies. A
general rule written last in the file never overrides a specific one written earlier, and a
specific rule written last never needs to be: both resolve to whichever rule actually narrows
the match furthest, every time.

The same ranking decides the order gitcd renders the generated `includeIf` blocks in, from least
to most specific, because a later block in that generated file wins over an earlier one, by
git's own behaviour. Rule order in the YAML file never affects this rendering order either.

## `defaultProfile`

The profile applied to a remote no rule matches, left unset by default, in which case such a
remote gets no profile and, with `useConfigOnly` on, refuses to commit under a guessed identity.
Must name an entry in `profiles`.

```yaml
defaultProfile: private
```

## `useConfigOnly`

Adds `user.useConfigOnly = true` to every rendered profile, and once, unconditionally, to the
top of the generated include file itself, default `true`.

```yaml
useConfigOnly: false
```

See [the identity mechanism](#identity-git-config-and-apply) below for exactly what this
setting does and does not protect against.

## Clearing a field deliberately

A host entry merges field by field onto that host's own built in default, when it has one,
rather than replacing it outright, so setting one field never silently drops another the
default carried. That merge needs a way to tell "this key was left out, keep the default" apart
from "this key was set to its empty value, on purpose", which is why the two are written
differently:

- `layout: ""` under a host clears that host's own default layout; the host then falls back to
  `layout.default` exactly as a host with no `layout` key at all already does. Leaving the
  `layout` key out entirely keeps the default layout that host already carries.
- `aliases: []` under a host clears that host's own default aliases outright. Leaving the
  `aliases` key out entirely keeps the default aliases that host already carries.

This matters today for `dev.azure.com`, the one host shipped with a built in `layout` and
`aliases`: writing only one of the two fields under `hosts.dev.azure.com` in `~/.gitcd.yaml`
never drops the other. See
[ADR 0001](adr/0001-host-config-merges-field-by-field.md) for the full reasoning.

## Identity: git config and `apply`

Two independent mechanisms apply an identity, and `apply` renders both every time it runs:

- A generated `includeIf` chain, for the common case. `gitcd apply` writes one file per profile
  under `~/.config/gitcd/profiles/<name>.gitconfig`, and one
  `~/.config/gitcd/includes.gitconfig` tying them to the configured rules: one `gitdir/i:` block
  matching the rendered layout path, and one `hasconfig:remote.*.url:` block per URL form the
  remote could be stored as, per rule, in ascending specificity order. `gitcd init` is the
  command that adds this file to the user's global git configuration, as one `include.path`
  line, so the chain actually takes effect.
- A pinned local override, for one repository at a time. `gitcd profile set <name> [path]`
  writes that profile's keys directly into `path`'s own local configuration, which beats an
  `include` by git's ordinary precedence, plus a `gitcd.profile` marker so `doctor` can tell a
  deliberate pin from stale configuration, and `profile unset` can remove exactly what was
  written.

`user.useConfigOnly` is where people get the mechanism wrong. It only stops git from guessing a
default `user.name` and `user.email` from the account's own username and the machine's
hostname; it never suppresses a value that is already configured, from any source, including one
set directly in the user's global git configuration rather than through either mechanism above.
That is why `apply` writes `user.useConfigOnly = true` twice: once, unconditionally, at the very
top of the generated include file, so a repository no rule matches and no `defaultProfile`
covers still refuses to guess, and again inside every profile file, redundant for a repository
the include chain already reaches, but load bearing wherever a profile file is read on its own,
outside that chain.

Neither copy, nor a pinned local override, protects against `user.name`, `user.email`,
`user.signingkey`, `gpg.format` or `commit.gpgsign` set directly in the user's own global git
configuration, outside every file gitcd generates: by git's own definition, that is already
"configuration", so `useConfigOnly` has nothing left to suppress, and the value applies to every
repository gitcd manages, one some rule matches or not.

`gitcd init` exists to catch exactly that case. Before it adds the include line at all, it scans
the global git configuration for `user.name`, `user.email`, `user.signingkey`, `gpg.format` and
`commit.gpgsign`, reports every one it finds, names the profile each would belong in, and, only
when `--move-identity` asks for it, moves each key into that profile and removes it from the
global file, backing up both files first. See [`gitcd init`](commands.md#init) for its flags and
exit codes.
