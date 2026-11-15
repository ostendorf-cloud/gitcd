# Contributing

Thank you for considering a contribution to gitcd.

## Local development

```sh
task setup   # mise install, the pinned toolchain
task check   # every check this repository runs in CI, one after another
```

`task --list` shows the individual targets.

## Developer Certificate of Origin

Every commit needs a `Signed-off-by` trailer. Add it with:

```sh
git commit -s
```

The trailer certifies the Developer Certificate of Origin, version 1.1:
<https://developercertificate.org>. You wrote the change, or have the right to submit it under
this project's license, Apache-2.0.

A commit without the trailer fails the `dco` check. Amend the most recent commit with
`git commit --amend --signoff`, or several with `git rebase --signoff <base branch>`, then push
with `--force-with-lease`. Merge commits and commits authored by a bot are exempt.

## License headers

Every `.go` file carries the Apache-2.0 header, first in the file:

```text
// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0
```
