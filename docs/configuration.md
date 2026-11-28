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

### A root behind a symlink

`root` may be a symlink, or sit below one, for example a code directory moved to another volume
and linked back. git matches a `gitdir` condition against the path it sees for a repository. On
Linux and macOS that is the resolved path when a command starts in the real directory, and the
path through the link when the shell's working directory names it, so a condition for one form
never matches the other. On Windows git matches the resolved path whichever way the directory was
entered, so the resolved condition is the one that counts there.

`gitcd apply` therefore renders every rule's `gitdir` condition for `root` as written and, when
resolving the symlinks in `root` gives a different path, a second one for the resolved path.
Both include the same profile file. A part of `root` that does not exist yet stays as written
after the resolved part that does.

The resolved path has to pass the same check `root` does: no glob character, quote or control
character, no `.` or `..` segment. A link target such as `a"b` or `[l]` would break the include
file or match something else, so no condition is rendered for it. The rules then do not apply to
a repository entered through the real path, and on Windows not to any repository under that
root. `gitcd apply` then warns on stderr and still writes
the files. Rename the target or point `root` at the real path.

`gitcd doctor` reports `root-symlink` when `root` reaches its directory through a symlink, in
either of two cases:

- The include file on disk has no condition for the resolved path. `gitcd apply` or
  `gitcd doctor --fix` renders it.
- The resolved path cannot be rendered, as above. This one is not fixable by a re-render.

Symlinks below the repository level, inside a repository's own directory tree, are not handled.

## `worktreeDir`

Where `gitcd worktree add` creates a worktree. Unset by default, and `worktree add` then refuses
to run. The picker `gitcd worktree` does not need it.

```yaml
worktreeDir: .claude/worktrees
```

- A relative value is joined onto the repository's main checkout: `.claude/worktrees` puts
  `feat/20-menu` at `<repo>/.claude/worktrees/feat/20-menu`.
- An absolute value is used as written, and a leading `~` is expanded against the loading user's
  home directory. `{repo}` becomes the repository's directory name, so `~/Worktrees/{repo}` keeps
  every repository's worktrees apart.
- `{repo}` is the only placeholder. A segment `..`, a control character or any other
  placeholder is an error when the file loads, so every command fails on it, not only
  `worktree add`.
- The branch name is appended below it, a slash nesting directories. A branch name that is
  absolute, holds a backslash or a colon, or has an empty, `.` or `..` segment is refused.

`list` shows worktrees by the rule it already has: a linked worktree is hidden unless
`--worktrees` is given, wherever `worktreeDir` puts it. gitcd walks `root`, so a worktree whose
directory is outside `root` is not found by `list`, `cd` or `status` at all, only by
`gitcd worktree`, which asks git.

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

Two profile names that differ only in case are rejected together. A name becomes a file name,
and on a case insensitive filesystem `work` and `Work` are one file, so one profile's identity
would quietly replace the other's.

A rejected name fails the whole file at load time, before any rule is looked at, so a rule
naming that profile never resolves and no command runs against a partly valid configuration.

### What a profile key may be

A profile key is a git config key in `section.key` or `section.subsection.key` form. The
section is a letter followed by letters, digits and `-`. The subsection and the key name hold
only letters, digits and `-`. The subsection is held to the same narrow class as the key name
because it is written into the `[section "subsection"]` header of the generated file, where a
wider character would put the keys after it under a section the profile does not name.

A value may hold anything but a control character git config has no escape for: a line break,
a tab and a backspace are written escaped and read back unchanged, a carriage return is
rejected.

## `allowUnsafeKeys`

A profile is applied automatically, per remote, to a repository whose contents came from that
remote. So gitcd refuses a profile key that hands git a command to run, changes what git
trusts, or is not a key it knows. A refused key fails the whole file at load time, in every
command, before anything is written. The list opts single keys back in, default empty.

```yaml
allowUnsafeKeys:
  - core.pager

profiles:
  private:
    user.email: jane@example.com
    core.pager: less -FRX
```

- An entry is one git config key, written like a profile key.
- An entry carries a key and never a value. The value stays in the profile.
- An entry matches the section and the key name case insensitively and the subsection exactly:
  `CORE.PAGER` opts in `core.pager`, and `url.X.insteadOf` does not opt in `url.x.insteadOf`.
- A wildcard is not accepted.
- An entry naming a key nothing restricts is an error, not a no-op:
  `allowUnsafeKeys names "user.email", which a profile may carry without being opted in`.
- `gitcd doctor` reports every key applied this way as an `opted-in-key` finding, so the choice
  stays visible. The finding keeps `doctor` at exit code `1` for as long as the entry exists.

### Finding out whether a key is restricted

There is no list to consult. Put the key into a profile and run any command, `gitcd apply
--dry-run` for one. A restricted key is refused with a message that names the key, the reason and
the remedy:

```text
gitcd apply: config: parse /home/me/.gitcd.yaml: profile "work": config: profile key needs an explicit opt in: "core.sshCommand" names a command git runs, or a path git resolves one from; list it under "allowUnsafeKeys" to apply it anyway
```

The reason is one of these. Each wording was produced by the binary:

| The refusal says the key | Which keys |
| --- | --- |
| `names a command git runs, or a path git resolves one from` | A key name that ends in a word like `command`, `cmd`, `pager`, `path`, `program` or `askPass`, such as `core.sshCommand`, `core.pager` and `core.hooksPath`. |
| `names a program git runs for this filter` or `for this submodule` | `filter.<driver>.clean`, `smudge`, `process` and `submodule.update`. |
| `sets a command git runs, or rewrites the URL git contacts` | Every key under `alias`, `pager` and `url`. |
| `makes git read configuration from a file no gate has seen` | Every key under `include` and `includeIf`. |
| `decides what git verifies, trusts, contacts or writes, rather than naming a command` | Keys such as `http.sslVerify`, `core.protectNTFS` and `safe.directory`. |
| `decides whether one of git's object checks still applies` | The message severities `fsck.<msg-id>`, `fetch.fsck.<msg-id>` and `receive.fsck.<msg-id>`, and `fsck.skipList`. |
| `decides what git trusts, writes or resolves something from` | Keys such as `core.fsmonitor` and `credential.helper`. |
| `is not a setting the git this gitcd was built against documents` | A key git does not document in the version gitcd was built against. |

The families behind these rows are described under
[what the gate restricts](#what-the-gate-restricts).

### What the gate restricts

Some git config keys hand git a command to run, take over a credential prompt, change which URL
git contacts, or decide what git trusts. They are rejected unless `allowUnsafeKeys` names the
key. A key is restricted for either of two reasons.

The first is what the key does. These are families, not a list, and the subsection never
decides a match, so `diff.command` and `diff.<driver>.command` are the same setting to the gate:

- a key name ending in `askPass`, `cmd`, `command`, `driver`, `editor`, `external`, `hook`,
  `httpd`, `pager`, `path`, `process`, `program`, `proxy`, `smtpServer`, `target`,
  `templateDir`, `textconv`, `tunnel` or `variant`, under any section. Three `*path` keys name
  no path git resolves and are excepted: `core.quotePath`, `credential.useHttpPath` and
  `advice.updateSparsePath`
- the key names `browser`, `fsmonitor`, `helper`, `insteadOf`, `pushInsteadOf` and `viewer`,
  under any section
- `clean`, `smudge` and `process` under `filter`, and `update` under `submodule`, a form of
  which git documents as a custom command and refuses to copy out of `.gitmodules` itself
- every key under `alias`, `include`, `includeIf`, `pager` and `url`

`include` and `includeIf` are whole sections because their value makes git read configuration
from somewhere else, so one of them would hand every key in that file to git with no gate at
all. They are the only two keys in git that do that.

A second class is restricted for a different reason: a key whose value decides whether git
verifies something, where it gets the material to verify with, where it sends its requests, or
what it writes to the filesystem. None of them runs a program. The reason is the same as for the
`core` protections: a profile is applied automatically, per remote, to a repository whose
contents came from that remote, so the mechanism that decides who the commit is from should not
also decide what git trusts. It covers `core.protectHFS`, `core.protectNTFS`,
`core.sharedRepository`, `core.symlinks`, `safe.directory`, `safe.bareRepository`,
`protocol.allow`, `credential.protectProtocol`, `credential.sanitizePrompt`,
`transfer.credentialsInUrl`, `transfer.fsckObjects`, `fetch.fsckObjects`,
`receive.fsckObjects`, `gpg.ssh.revocationFile`, `gpg.minTrustLevel`,
`gpg.ssh.allowedSignersFile` and 21 `http.*` keys. They cover TLS verification, the trust store
and backend, client certificates, cookies, extra headers, redirects, hostname resolution,
`http.proxy` with its TLS keys, the auth keys `http.delegation`, `http.emptyAuth` and
`http.proactiveAuth`, and `http.pinnedPubkey`.

`gpg.ssh.allowedSignersFile` is in that class because it is the trust store an ssh signature is
checked against, which is the other half of the same decision as `gpg.ssh.revocationFile`.

The fsck message severities are restricted too, in all three spellings git documents:
`fsck.<msg-id>`, `fetch.fsck.<msg-id>` and `receive.fsck.<msg-id>`, with `fsck.skipList` beside
them. A severity of `ignore` or a skip list turns off the same object checks the `fsckObjects`
switches turn on, so restricting the switch and allowing the per message setting would cover
one spelling of the decision and not the other. The message ids come from the generated key
list, so a newer git's ids are covered by regenerating it. The cost is a refusal in the safe
direction: the rule reads the key and not the value, so a profile raising a severity rather
than lowering it needs the same opt in.

A third class is deliberately **not** restricted: a key naming a file git reads as data.
`core.attributesFile`, `core.excludesFile`, `mailmap.file`, `diff.orderFile`,
`blame.ignoreRevsFile` and `format.signatureFile` all name a file whose contents git
interprets, and none grants trust or decides a verification. An attributes file can name a
filter or a diff driver, but only by a name some config key has to define, and gitcd will not
write such a definition without an opt in. `mailmap.file` and `blame.ignoreRevsFile` change
what git shows about authorship, not what it verifies, and `format.signatureFile` is the text
appended to a patch mail rather than anything cryptographic.

A match is set aside only in `advice`, `color` and `column`, the sections where git resolves no
key's value to a program or a path, so a match there is a collision of spelling:
`advice.ignoredHook` and `color.pager` read like commands and are plain switches.

The value is never what sets a match aside. `true`, `false` and `yes` are programs on `PATH`,
so `core.sshCommand = false` runs `/usr/bin/false` and `core.sshCommand = yes` never returns. A
key whose value git resolves to a program is restricted whatever it carries, an empty value
included.

The four protection names are a scope decision, not a command: a profile may not weaken
`core.protectNTFS`, `core.protectHFS`, `core.sharedRepository` or `core.symlinks`. A profile is
applied automatically, per remote, to a repository whose contents came from that remote, and
what git will write to the filesystem should not be decided by the same mechanism that decides
who the commit is from. Naming one in `allowUnsafeKeys` still sets it.

The second reason is that the key is not one the git this gitcd was built against documents at
all. `internal/config/gitkeys.go` holds that list, generated from `git help -c` by
`task generate:gitkeys`. This is what keeps the families from having to be exhaustive: a
setting git adds later is restricted whatever it is called, so the families can describe what
they really cover instead of being widened until they catch everything. The cost is that a key
from a newer git, and a key in a section whose names git does not enumerate such as
`filter.<driver>.required`, is refused until the list is regenerated or the key is named in
`allowUnsafeKeys`. A short addendum in `internal/config/validate.go` carries the
keys git documents in `git-config(1)` but does not print, `core.longPaths` today. Each entry
names where git documents it, and a test fails if the generated list starts carrying it.

The identity and signing keys a profile exists to carry are not restricted: `user.name`,
`user.email`, `user.signingKey`, `gpg.format`, `commit.gpgsign` and `tag.gpgsign`. `gpg.format`
is read against a fixed set of formats and names no command. The program for each format is
`gpg.<format>.program`, which is restricted. A profile that also wants to name the allowed
signers file lists `gpg.ssh.allowedSignersFile` under `allowUnsafeKeys`, since that file decides
which signatures verify rather than which identity signs.

## What a root, a host alias and a layout may be

`root` is rendered into the generated directory conditions, so it has to name a directory and
nothing more: an absolute path, holding no glob metacharacter, no quote, no control character
and no `.` or `..` segment. A space and a non-ASCII name are fine, because a home directory may
hold either. A glob here would match repositories outside the root, and a quote would make git
refuse the file your global configuration includes.

A host's `aliases` are written into the generated conditions in the same position the rule's own
host occupies, so each is held to much the same terms as a rule host: no glob, no quote, no path
separator. An alias of `*` would otherwise apply an owner scoped rule on every host. An alias
holds letters, digits, `.`, `-` and `_`, and begins and ends with a letter or a digit. The
underscore is accepted, since an alias is an `ssh_config` `Host` name rather than a DNS name. A
rule host does not accept it.

A layout template is a path, so unlike a rule field it carries `/` between its segments and
`{host}` style placeholders inside them. Each segment may hold placeholders from the set
`{host}`, `{owner}`, `{org}`, `{project}` and `{repo}`, plus letters, digits, `.`, `-` and `_`.
A glob, a quote and a `.` or `..` segment are rejected: a glob widens the generated condition
past the rule that named it, and an upward segment roots it outside the configured root.

## `rules`

Decides which `profiles` entry a remote resolves to. Each rule names a `host`, required, and may
narrow the match further with `owner` and, alongside `owner`, `project` and `repo`; an empty
field matches every value of that field, and the rule's `profile` must name an entry in
`profiles`.

```yaml
rules:
  - host: dev.azure.com
    profile: work
  - host: dev.azure.com
    owner: acme
    project: billing
    profile: billing
  - host: github.com
    owner: acme
    profile: private
  - host: github.com
    owner: acme
    repo: internal-tools
    profile: work
```

### Rule specificity

The most specific matching rule wins. A rule with more fields is more specific, and the shapes
a rule can have form one total order, from least to most specific:

1. `host`
2. `host` and `owner`
3. `host`, `owner` and `project`
4. `host`, `owner` and `repo`
5. `host`, `owner`, `project` and `repo`

A rule that pins a repository outranks one that pins only a project, since a project holds many
repositories and a repository name is the narrower choice. Two rules of the same shape that both
match the same remote are ordered by their field values and then their profile name. Avoid such a
pair, since the later one wins.

This ranking, not the order the file lists rules in, decides which one applies. A general rule
written last in the file never overrides a specific one written earlier, and a specific rule
written last never needs to be: both resolve to whichever rule actually narrows the match
furthest, every time.

The same ranking decides the order gitcd renders the generated `includeIf` blocks in, from least
to most specific, because a later block in that generated file wins over an earlier one, by
git's own behaviour. Rule order in the YAML file never affects this rendering order either.

### What a rule field may be

A rule's `host`, `owner`, `project` and `repo` are written literally into the conditions of the
generated include file. The character class differs per field:

- `host` holds letters, digits, `.` and `-`, and begins and ends with a letter or a digit. An
  underscore is refused, unlike in a host alias.
- `owner` holds letters, digits, `.`, `-` and `_`, with `/` between segments so a group and
  subgroup can be named as one. No segment is empty, `.` or `..`. On `dev.azure.com` the owner is
  the organisation alone: an owner with a `/` there is refused, and the message names `project`.
- `project` holds letters, combining marks, digits and `. - _ ( ) & ! @ ^ ~`, in words joined by
  single spaces, since an Azure DevOps project name may hold a space and these characters. It
  has no leading, trailing or doubled space. It does not start with `_` or `.`, does not end with
  `.`, and is at most 64 characters long, the limits Azure DevOps sets. Only the characters above
  are accepted. `@` and `&` are URL delimiters and are accepted on purpose, since Azure DevOps
  allows them in a name and `apply` renders them both raw and percent encoded. The glob
  characters `* ? [ ] \` are refused, since git would read them as a pattern and the condition
  would match more than the rule names. A `%` is refused too, since it would read as a percent
  escape in a URL. A word may not start with a combining mark, and a variation selector
  (U+FE0E, U+FE0F) or a zero width character (U+200B to U+200D, U+2060, U+FEFF) is refused,
  since they allow names that look alike. The composed and the decomposed spelling of an accented
  letter are both accepted.
- `repo` holds letters, digits, `.`, `-` and `_`, and is not `.` or `..`.

A glob is not accepted in any of them: leaving a field out already matches every value of that
field, while a `*` in an `owner` would silently apply the rule's identity and signing key to
repositories it does not name.

### Naming an Azure DevOps project

An Azure DevOps remote carries an organisation, a project and a repository, and `owner` is the
organisation. A project is its own field:

```yaml
rules:
  - host: dev.azure.com
    owner: acme
    project: billing
    profile: billing
  - host: dev.azure.com
    owner: acme
    project: My Project
    profile: billing
```

`owner: acme/billing` for `dev.azure.com` never matched a remote, since gitcd reads the
organisation `acme` from it. It is refused now, with a message that names `project`, so a
configuration file that holds such a rule fails to load until the rule is split into `owner` and
`project`. A GitLab style owner with a `/`, a group and a subgroup, stays valid on every other
host.

`project` needs `owner` and is refused without it. It is also refused for any host but
`dev.azure.com`. Only a `dev.azure.com` remote, under its aliases `ssh.dev.azure.com` and
`vs-ssh.visualstudio.com` too, carries a project, so on another host the rule could never match.

`apply` renders the project into both kinds of condition:

- The `gitdir/i` condition follows the host's layout. With the default `{host}/{org}/{project}/{repo}`
  it reads `<root>/dev.azure.com/acme/billing/*/**`. The project is written as the layout writes
  it, a space included. A layout with no `{project}` placeholder renders no path condition for a
  rule with a project, since it could not tell one project from another. The URL conditions still
  apply.
- The `hasconfig` conditions cover every Azure DevOps form, for the host and each alias:
  `https://dev.azure.com/acme/billing/...`, the same with user info as in
  `https://acme@dev.azure.com/...`, and `git@ssh.dev.azure.com:v3/acme/billing/...`.
  A project that holds a space, a non ASCII character or a character a URL escapes is rendered
  once as written and once for each percent encoding, because git stores a remote as it was
  typed: minimally encoded (`Billing%20(EU)`) and fully encoded (`Billing%20%28EU%29`). Each
  encoding is rendered with upper and lower case hex digits.

gitcd reads every spelling back as one project, in the HTTPS, the SSH and the bare
`host/owner/project/repo` form. A percent escape in an SSH path or in the bare form is decoded,
so a clone placed at a path that holds the escape literally, as `my%20project`, shows up as a
`path-mismatch` in `gitcd doctor`. Which spelling Azure DevOps itself writes into the SSH URL of
a project with a space or a non ASCII character, and with which hex case, is not verified, so
all of them match. `gitcd clone` builds the SSH and HTTPS URL of a shorthand with the project
percent encoded.

### Case of owner, project and repo

Owner, project and repo match by exact case, in every command and in the `hasconfig`
conditions, even though Azure DevOps treats organisation and project names as case insensitive.
The `gitdir/i` path condition ignores case. A rule that matches a repository except for the case
of one of these fields is never applied by gitcd, so `profile show` and `doctor` expect another
profile. Inside the layout path git may still apply it through the path condition. `gitcd doctor`
then reports both the identity drift and `rule-case-mismatch`. Write the field as the remote
spells it.

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

Neither copy protects against `user.name`, `user.email`, `user.signingkey`, `gpg.format` or
`commit.gpgsign` set directly in the user's own global git configuration, outside every file
gitcd generates: by git's own definition, that is already "configuration", so `useConfigOnly`
has nothing left to suppress. What such a value does depends on where it sits, because git lets
the last value win:

- Before the `include.path` line, as it is when `gitcd init` appended the include after the key:
  the managed include comes later, so a profile that sets the same key wins in every repository
  the profile reaches, by a rule or by `defaultProfile`. The global value still applies to a
  repository no profile reaches and to a key the profile leaves unset.
- After the `include.path` line: the global value wins everywhere, a matching rule or not.
- A pinned local override beats both, since a repository's own configuration comes last.

`gitcd init` exists to catch exactly that case. Before it adds the include line at all, it scans
the global git configuration for `user.name`, `user.email`, `user.signingkey`, `gpg.format` and
`commit.gpgsign`, reports every one it finds, names the profile each would belong in, and, only
when `--move-identity` asks for it, moves each key into that profile and removes it from the
global file, backing up both files first. See [`gitcd init`](commands.md#init) for its flags and
exit codes.
