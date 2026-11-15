# Security Policy

## Supported versions

This project has no LTS branches. Only the latest release gets security fixes.

## Reporting a vulnerability

The preferred channel is GitHub's private vulnerability reporting, the "Report a
vulnerability" button under this repository's Security tab, where enabled.

When private reporting is unavailable or unsuitable, report to `security@ostendorf.cloud`.

Do not open a public issue for a vulnerability that is not yet patched.

## What to expect

- An acknowledgement of the report.
- No public issue opened before a fix is available.
- Credit in the fix's changelog entry once resolved, if you want to be credited.

## Credentials and remote URLs

gitcd never stores a credential. Keep credentials out of a remote URL and use a git credential
helper or an SSH key instead. Output that `git` itself writes is passed through as git wrote it.
