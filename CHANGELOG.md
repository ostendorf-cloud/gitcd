# Changelog

## v0.1.0 (2026-11-16)

### Features

- **release:** generate the winget manifest and attach it to the release ([#65](https://github.com/ostendorf-cloud/gitcd/pull/65))
- **release:** publish a Homebrew formula to the tap after the release ([#60](https://github.com/ostendorf-cloud/gitcd/pull/60))
- validate what a configuration may put into a generated git config ([#24](https://github.com/ostendorf-cloud/gitcd/pull/24))
- **ci:** release the binaries for linux, macos and windows ([#29](https://github.com/ostendorf-cloud/gitcd/pull/29))
- shell-init for PowerShell ([#27](https://github.com/ostendorf-cloud/gitcd/pull/27))
- reject a path component that cannot name a directory ([#25](https://github.com/ostendorf-cloud/gitcd/pull/25))
- doctor and adopt ([#17](https://github.com/ostendorf-cloud/gitcd/pull/17))
- list repositories and cd to one by name ([#13](https://github.com/ostendorf-cloud/gitcd/pull/13))
- status, sync and open across repositories ([#14](https://github.com/ostendorf-cloud/gitcd/pull/14))
- identity profiles applied per remote ([#10](https://github.com/ostendorf-cloud/gitcd/pull/10))
- navigation core, remote parsing, layout and clone ([#6](https://github.com/ostendorf-cloud/gitcd/pull/6))

### Bug Fixes

- **ci:** leave the generated changelog out of markdownlint ([#82](https://github.com/ostendorf-cloud/gitcd/pull/82))
- recognise the gitcd include in an includeIf and in a tilde path ([#80](https://github.com/ostendorf-cloud/gitcd/pull/80))
- make doctor and init name the same target for a global identity key ([#78](https://github.com/ostendorf-cloud/gitcd/pull/78))
- make init --dry-run predict the move cases of the real run ([#79](https://github.com/ostendorf-cloud/gitcd/pull/79))
- **release:** accept the installer entries of the winget manifest in either order ([#76](https://github.com/ostendorf-cloud/gitcd/pull/76))
- announce a backup in init --dry-run only when one is made ([#74](https://github.com/ostendorf-cloud/gitcd/pull/74))
- **git:** match url keys case insensitively in the withholding check ([#64](https://github.com/ostendorf-cloud/gitcd/pull/64))
- **remote:** mask the query and fragment of a displayed remote ([#58](https://github.com/ostendorf-cloud/gitcd/pull/58))
- state what a global identity key really does in init and doctor ([#71](https://github.com/ostendorf-cloud/gitcd/pull/71))
- render the gitdir condition for a symlinked root ([#57](https://github.com/ostendorf-cloud/gitcd/pull/57))
- handle the Azure DevOps clone dialog forms and token URLs ([#56](https://github.com/ostendorf-cloud/gitcd/pull/56))
- harden credentials, file modes, the init backup and path moves ([#52](https://github.com/ostendorf-cloud/gitcd/pull/52))
- three small bugs and a refactor from the release survey ([#51](https://github.com/ostendorf-cloud/gitcd/pull/51))
- **cli:** reject an argument and an unknown flag in version ([#31](https://github.com/ostendorf-cloud/gitcd/pull/31))
- keep the configuration file's own mode ([#18](https://github.com/ostendorf-cloud/gitcd/pull/18))
- stop an unmatched repository inheriting a global identity ([#12](https://github.com/ostendorf-cloud/gitcd/pull/12))
- merge a host config entry field by field ([#11](https://github.com/ostendorf-cloud/gitcd/pull/11))

## v0.1.0-rc.1 (2026-11-16)

### Features

- **release:** generate the winget manifest and attach it to the release ([#65](https://github.com/ostendorf-cloud/gitcd/pull/65))
- **release:** publish a Homebrew formula to the tap after the release ([#60](https://github.com/ostendorf-cloud/gitcd/pull/60))
- validate what a configuration may put into a generated git config ([#24](https://github.com/ostendorf-cloud/gitcd/pull/24))
- **ci:** release the binaries for linux, macos and windows ([#29](https://github.com/ostendorf-cloud/gitcd/pull/29))
- shell-init for PowerShell ([#27](https://github.com/ostendorf-cloud/gitcd/pull/27))
- reject a path component that cannot name a directory ([#25](https://github.com/ostendorf-cloud/gitcd/pull/25))
- doctor and adopt ([#17](https://github.com/ostendorf-cloud/gitcd/pull/17))
- list repositories and cd to one by name ([#13](https://github.com/ostendorf-cloud/gitcd/pull/13))
- status, sync and open across repositories ([#14](https://github.com/ostendorf-cloud/gitcd/pull/14))
- identity profiles applied per remote ([#10](https://github.com/ostendorf-cloud/gitcd/pull/10))
- navigation core, remote parsing, layout and clone ([#6](https://github.com/ostendorf-cloud/gitcd/pull/6))

### Bug Fixes

- recognise the gitcd include in an includeIf and in a tilde path ([#80](https://github.com/ostendorf-cloud/gitcd/pull/80))
- make doctor and init name the same target for a global identity key ([#78](https://github.com/ostendorf-cloud/gitcd/pull/78))
- make init --dry-run predict the move cases of the real run ([#79](https://github.com/ostendorf-cloud/gitcd/pull/79))
- **release:** accept the installer entries of the winget manifest in either order ([#76](https://github.com/ostendorf-cloud/gitcd/pull/76))
- announce a backup in init --dry-run only when one is made ([#74](https://github.com/ostendorf-cloud/gitcd/pull/74))
- **git:** match url keys case insensitively in the withholding check ([#64](https://github.com/ostendorf-cloud/gitcd/pull/64))
- **remote:** mask the query and fragment of a displayed remote ([#58](https://github.com/ostendorf-cloud/gitcd/pull/58))
- state what a global identity key really does in init and doctor ([#71](https://github.com/ostendorf-cloud/gitcd/pull/71))
- render the gitdir condition for a symlinked root ([#57](https://github.com/ostendorf-cloud/gitcd/pull/57))
- handle the Azure DevOps clone dialog forms and token URLs ([#56](https://github.com/ostendorf-cloud/gitcd/pull/56))
- harden credentials, file modes, the init backup and path moves ([#52](https://github.com/ostendorf-cloud/gitcd/pull/52))
- three small bugs and a refactor from the release survey ([#51](https://github.com/ostendorf-cloud/gitcd/pull/51))
- **cli:** reject an argument and an unknown flag in version ([#31](https://github.com/ostendorf-cloud/gitcd/pull/31))
- keep the configuration file's own mode ([#18](https://github.com/ostendorf-cloud/gitcd/pull/18))
- stop an unmatched repository inheriting a global identity ([#12](https://github.com/ostendorf-cloud/gitcd/pull/12))
- merge a host config entry field by field ([#11](https://github.com/ostendorf-cloud/gitcd/pull/11))
