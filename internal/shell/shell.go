// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package shell renders the per shell init snippet "gitcd shell-init"
// prints. Each snippet defines a "gitcd" function that shadows the binary:
// it calls through to the real "gitcd" for every command, and for "clone"
// and "path", the two commands that print a path on stdout, changes the
// shell's working directory to it when that path actually exists, since
// "clone --dry-run" prints a path it deliberately never created. Every
// other command's output passes through unchanged, because the binary
// itself already put it on stdout for exactly this purpose.
package shell

import "fmt"

// bashLike is the "gitcd" function body shared by bash and zsh, whose
// command substitution and case syntax agree.
const bashLike = `gitcd() {
  local real
  real="$(command gitcd "$@")" || return $?
  case "$1" in
    clone|path)
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
        case clone path
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

// Bash is the init snippet for bash.
func Bash() string { return bashLike }

// Zsh is the init snippet for zsh.
func Zsh() string { return bashLike }

// Fish is the init snippet for fish.
func Fish() string { return fish }

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
