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

```powershell
Invoke-Expression (gitcd shell-init pwsh | Out-String) # the same, in PowerShell's $PROFILE
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

Two profile names that differ only in case are rejected together. A name becomes a file name,
and on a case insensitive filesystem `work` and `Work` are one file, so one profile's identity
would quietly replace the other's.

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

Some git config keys hand git a command to run, take over a credential prompt, change which URL
git contacts, or decide what git trusts. A profile is picked automatically per remote, so these
are rejected unless `allowUnsafeKeys` names the key. A key is restricted for either of two
reasons.

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

A second class is restricted for a different reason, and says so in its own words rather than
borrowing the first class's: a key whose value decides whether git verifies something, where it
gets the material to verify with, where it sends its requests, or what it writes to the
filesystem. None of them runs a program, and the reason is the same as for the `core`
protections — a profile is applied automatically, per remote, to a repository whose contents
came from that remote, so the mechanism that decides who the commit is from should not also
decide what git trusts. It covers `core.protectHFS`, `core.protectNTFS`,
`core.sharedRepository`, `core.symlinks`, `safe.directory`, `safe.bareRepository`,
`protocol.allow`, `credential.protectProtocol`, `credential.sanitizePrompt`,
`transfer.credentialsInUrl`, `transfer.fsckObjects`, `fetch.fsckObjects`,
`receive.fsckObjects`, `gpg.ssh.revocationFile`, `gpg.minTrustLevel`,
`gpg.ssh.allowedSignersFile` and twenty `http.*` keys covering TLS verification, the trust
store and backend, client certificates, cookies, extra headers, redirects and hostname
resolution.

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
so `core.sshCommand = false` runs `/usr/bin/false` and `core.sshCommand = yes` never returns; a
key whose value git resolves to a program is restricted whatever it carries, an empty value
included.

The four protection names are a scope decision, not a command: a profile may not weaken
`core.protectNTFS`, `core.protectHFS`, `core.sharedRepository` or `core.symlinks`. A profile is
applied automatically, per remote, to a repository whose contents came from that remote, and
what git will write to the filesystem should not be decided by the same mechanism that decides
who the commit is from. Naming one in `allowUnsafeKeys` still sets it.

The second is that the key is not one the git this gitcd was built against documents at all.
`internal/config/gitkeys.go` holds that list, generated from `git help -c` by
`task generate:gitkeys`. This is what keeps the families from having to be exhaustive: a
setting git adds later is restricted whatever it is called, so the families can describe what
they really cover instead of being widened until they catch everything. The cost is that a key
from a newer git, and a key in a section whose names git does not enumerate such as
`filter.<driver>.required` or `pager.<command>`, is refused until the list is regenerated or the
key is named in `allowUnsafeKeys`. A short addendum in `internal/config/validate.go` carries the
keys git documents in `git-config(1)` but does not print, `core.longPaths` today; each entry
names where git documents it, and a test fails if the generated list starts carrying it.

The identity and signing keys a profile exists to carry are not restricted: `user.name`,
`user.email`, `user.signingKey`, `gpg.format`, `commit.gpgsign` and `tag.gpgsign`. `gpg.format`
is read against a fixed set of formats and names no command; the program for each format is
`gpg.<format>.program`, which is restricted. A profile that also wants to name the allowed
signers file lists `gpg.ssh.allowedSignersFile` under `allowUnsafeKeys`, since that file decides
which signatures verify rather than which identity signs.

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

### What a root, a host alias and a layout may be

`root` is rendered into the generated directory conditions, so it has to name a directory and
nothing more: an absolute path, holding no glob metacharacter, no quote, no control character
and no `.` or `..` segment. A space and a non-ASCII name are fine, because a home directory may
hold either. A glob here would match repositories outside the root, and a quote would make git
refuse the file your global configuration includes.

A host's `aliases` are written into the generated conditions in the same position the rule's own
host occupies, so each is held to much the same terms as a rule host: no glob, no quote, no path
separator. An alias of `*` would otherwise apply an owner scoped rule on every host. An
underscore is accepted, since an alias is an `ssh_config` `Host` name rather than a DNS name.

A layout template is a path, so unlike a rule field it carries `/` between its segments and
`{host}` style placeholders inside them. Each segment may hold placeholders from the set
`{host}`, `{owner}`, `{org}`, `{project}` and `{repo}`, plus letters, digits, `.`, `-` and `_`.
A glob, a quote and a `.` or `..` segment are rejected: a glob widens the generated condition
past the rule that named it, and an upward segment roots it outside the configured root.

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
