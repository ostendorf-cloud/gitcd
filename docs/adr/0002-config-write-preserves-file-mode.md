# 2. Writing `~/.gitcd.yaml` preserves the file's existing mode

Status: accepted

## Context

- `gitcd init --move-identity` writes `~/.gitcd.yaml` (or its XDG fallback) through a
  temporary file created next to it with `os.CreateTemp`, renamed over it.
- `os.CreateTemp` creates its file `0o600`; the write path then chmodded it to a fixed `0o644`
  before the rename, so a file a user had set to `0o600` came back `0o644`, every time.
- The file carries a name, an email address and a signing key identifier, personal but not a
  secret, so this was not a credential exposure, only gitcd overriding a permission decision
  the user made deliberately, without saying so.
- The mode that matters is the write target's, not a symlink's: the file is commonly
  symlinked into a dotfiles checkout, and a symlink carries no permission bits of its own.

## Decision

The write path stats the resolved write target before creating the temporary file, and
chmods the temporary file to that mode, whatever it is, before the rename carries it over.

- A target that exists keeps its exact mode, preserved silently, including a mode wider than
  gitcd would have chosen itself. The fix exists to stop gitcd silently overriding a choice
  the user made about their own file; flagging that same choice as "surprising" afterwards
  would reintroduce the same kind of silent judgment call in a milder form. A command run with
  `--verbose` already logs the path written; `ls -l` tells the rest.
- A target that does not exist yet, the first run on a machine with no configuration file at
  all, is created `0o600`. The file holds personal identity data, not a secret, but still
  specific enough to one person that an owner only default beats a world readable one; a user
  who wants it wider sets that themselves, the same way any other mode decision stays theirs.
- Ownership is not preserved separately from mode. The file gitcd writes through this path is
  the invoking user's own dotfile; the renamed-in file already carries that same user's uid
  and gid because the process that created it does, with no extra code needed. Changing
  ownership to a different user would need privilege this process does not run with, and
  the one scenario where that would matter, a config file owned by someone other than the
  user running `gitcd`, already cannot write through this path successfully in the first
  place without directory or group permissions broad enough that an explicit `chown` call
  would just as likely fail or mislead.

## Consequences

- A `0o600` (or any other mode) configuration file survives `gitcd init --move-identity=yes`
  unchanged, symlinked into a dotfiles checkout or not.
- A freshly created `~/.gitcd.yaml` is `0o600`, documented here and in `docs/concepts/gitcd.md`.
- A future write path for this same file should stat-then-chmod the same way rather than
  hardcoding a mode, or this decision quietly stops applying to it.

## Alternatives

- Keep the fixed `0o644`: reproduces the bug this ADR exists to fix.
- Preserve mode but warn on one wider than `0o600`: adds a nag about a decision that is the
  user's to make, for a file that carries no secret to protect.
- Preserve ownership too, with an explicit `os.Chown`: adds a call that only matters in a
  cross-user scenario this tool was never designed for, and fails under most of the
  permission conditions that would make it necessary.
