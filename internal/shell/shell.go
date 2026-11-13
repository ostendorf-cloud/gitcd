// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package shell renders the per shell init snippet "gitcd shell-init"
// prints. Each snippet defines a "gitcd" function that shadows the binary:
// it calls through to the real "gitcd" for every command, and for "clone",
// "path" and "cd", the commands that print a path on stdout, changes the
// shell's working directory to it when that path actually exists, since
// "clone --dry-run" prints a path it deliberately never created. Every
// other command's output passes through unchanged, because the binary
// itself already put it on stdout for exactly this purpose. The zsh and
// fish snippets also register tab completion for "cd", shelling out to
// "gitcd list --worktrees" fresh on every request rather than caching it,
// since a repository just cloned should complete immediately.
package shell

import "fmt"

// bashLike is the "gitcd" function body shared by bash and zsh, whose
// command substitution and case syntax agree.
const bashLike = `gitcd() {
  local real
  real="$(command gitcd "$@")" || return $?
  case "$1" in
    clone|path|cd)
      if [ -d "$real" ]; then
        cd -- "$real"
      else
        printf '%s\n' "$real"
      fi
      ;;
    *)
      printf '%s\n' "$real"
      ;;
  esac
}
`

// zshCompletion registers tab completion for "gitcd cd": pressing tab
// shells out to "gitcd list --worktrees" fresh every time, trading one
// local process start for never showing a repository cloned, or a
// worktree added, moments ago as stale. "${repos[@]:t}" takes each path's
// final segment, so completion offers short names rather than full paths,
// composing with "cd"'s own name based exact match; a name two
// repositories share completes to either and "cd" itself then falls back
// to its ambiguous match picker.
const zshCompletion = `_gitcd() {
  local -a repos
  case "${words[2]}" in
    cd)
      repos=("${(@f)$(command gitcd list --worktrees 2>/dev/null)}")
      compadd -- "${repos[@]:t}"
      ;;
  esac
}
compdef _gitcd gitcd
`

// fishCompletion is zshCompletion's fish equivalent: "string replace -r"
// strips every path down to its final "/" separated segment, since fish
// has no direct basename expansion modifier, and the command substitution
// fish writes inside "(...)" is re-run on every completion request, the
// same "no caching" choice zshCompletion makes.
const fishCompletion = `complete -c gitcd -n '__fish_seen_subcommand_from cd' -f -a "(command gitcd list --worktrees 2>/dev/null | string replace -r '^.*/' '')"
`

// fish is the "gitcd" function body for fish, whose syntax for a local
// variable, a command substitution and a switch differs from bashLike's.
// "set -l code $status" reads the exit status of the command substitution
// on the line above, not of "set" itself: fish documents that a command
// substitution's exit status lands in $status when the substitution
// happens in the context of "set" (fishshell.com/docs/current/language.html,
// "Command substitution"), and that value is still the one $status holds
// when the next line reads it, since the right hand side of "set -l code
// $status" is expanded before that "set" runs and could overwrite $status
// again. The guard below is still needed regardless: "cd" with no argument
// goes to $HOME (fishshell.com/docs/current/cmds/cd.html), so an empty
// "$real" must never reach it unguarded.
const fish = `function gitcd
    set -l real (command gitcd $argv)
    set -l code $status
    if test $code -ne 0
        return $code
    end
    switch $argv[1]
        case clone path cd
            if test -d "$real"
                cd $real
            else
                printf '%s\n' $real
            end
        case '*'
            printf '%s\n' $real
    end
end
`

// Bash is the init snippet for bash. Bash has no tab completion snippet
// here: only zsh and fish do, per the "cd" completion acceptance
// criterion.
func Bash() string { return bashLike }

// Zsh is the init snippet for zsh: the shared wrapper function plus "cd"
// tab completion.
func Zsh() string { return bashLike + zshCompletion }

// Fish is the init snippet for fish: the wrapper function plus "cd" tab
// completion.
func Fish() string { return fish + fishCompletion }

// Init returns the init snippet for shell, one of "bash", "zsh" or "fish".
func Init(shell string) (string, error) {
	switch shell {
	case "bash":
		return Bash(), nil
	case "zsh":
		return Zsh(), nil
	case "fish":
		return Fish(), nil
	default:
		return "", fmt.Errorf("shell: %q is not bash, zsh or fish", shell)
	}
}
