# gitcd

Clone repositories into a predictable path layout, apply a git identity profile per remote, and
jump to any of them from the shell.

## What it does

- Clones `github.com/owner/repo` to `~/Code/github.com/owner/repo`, with the layout configurable
  per host.
- Applies `user.name`, `user.email` and the signing key from a profile matched on the remote, so
  work and private identities never have to be switched by hand.
- Resolves a partial repository name to a path, which a shell function turns into a `cd`.

## Status

In development. The milestones are tracked as issues.
