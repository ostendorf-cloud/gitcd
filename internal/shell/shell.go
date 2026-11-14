// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package shell renders the per shell init snippet "gitcd shell-init"
// prints. Each snippet defines a "gitcd" function that shadows the binary:
// it calls through to the real "gitcd" for every command, and for "clone",
// "path" and "cd", the commands that print a path on stdout, changes the
// shell's working directory to it when that path actually exists, since
// "clone --dry-run" prints a path it deliberately never created. Every
// other command's output passes through unchanged, because the binary
// itself already put it on stdout for exactly this purpose. The zsh,
// fish and PowerShell snippets also register tab completion for "cd",
// shelling out to "gitcd list --worktrees" fresh on every request rather
// than caching it, since a repository just cloned should complete
// immediately.
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

// powerShell is the "gitcd" function body for PowerShell, targeting both
// Windows PowerShell 5.1, which ships with Windows, and PowerShell 7
// ("pwsh"), which does not: every construct it uses, Get-Command,
// Test-Path, Set-Location, $LASTEXITCODE and the switch statement, is
// available unchanged on 5.1.
//
// PowerShell has no builtin equivalent of the POSIX "command" that
// bashLike and fish use to call through to the real binary bypassing a
// same named function: PowerShell's own command precedence puts a
// function ahead of an external executable (learn.microsoft.com,
// "about_Command_Precedence"), exactly like bash puts a function ahead of
// a PATH binary, so the "gitcd" function below would recurse into itself
// without a way around that precedence. "Get-Command -CommandType
// Application" is that way around it: it is the same type filter the
// command precedence article itself uses to resolve a name hidden by a
// function (its "where.exe" example), so it names only the real binary.
//
// A native command's exit code lands in $LASTEXITCODE
// (learn.microsoft.com, "about_Automatic_Variables"), and nothing a
// cmdlet such as Test-Path or Set-Location does afterward touches it, so
// the caller can still read it once this function returns. $? is a
// different story: a plain function that returns normally, without
// throwing, reports True to its caller regardless of what happened to $?
// inside it, verified here by calling a function with this exact
// early-return shape against a deliberately failing native command and
// reading $? immediately afterward. $LASTEXITCODE is therefore the one
// signal this function actually preserves for the caller, the PowerShell
// equivalent of bashLike's "|| return $?"; returning immediately on a non
// zero exit, before Test-Path or anything else runs, keeps the real
// binary's own call the last thing that could have set it.
const powerShell = `function gitcd {
    $gitcdPath = (Get-Command gitcd -CommandType Application | Select-Object -First 1).Source
    $real = & $gitcdPath @args
    if ($LASTEXITCODE -ne 0) {
        return
    }
    switch ($args[0]) {
        { $_ -in 'clone', 'path', 'cd' } {
            if (Test-Path -LiteralPath $real -PathType Container) {
                Set-Location -LiteralPath $real
            } else {
                $real
            }
        }
        default {
            $real
        }
    }
}
`

// powerShellCompletion is zshCompletion's and fishCompletion's PowerShell
// equivalent. "Register-ArgumentCompleter -Native" matches a command name
// as plain text, which is how it still completes "gitcd" once the
// function above has shadowed the real binary under that same name
// (learn.microsoft.com, "Register-ArgumentCompleter"); unlike compadd or
// fish's own completion matching, a native completer's scriptblock is
// responsible for its own prefix filtering, which is what the "-like"
// line against wordToComplete does. The scriptblock signature, resolving
// the real binary the same way the function body does, and the
// CompletionResult construction all follow that same reference page's own
// native completer example.
const powerShellCompletion = `Register-ArgumentCompleter -Native -CommandName gitcd -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    if ($commandAst.CommandElements.Count -lt 2 -or $commandAst.CommandElements[1].Extent.Text -ne 'cd') {
        return
    }
    $gitcdPath = (Get-Command gitcd -CommandType Application | Select-Object -First 1).Source
    & $gitcdPath list --worktrees 2>$null | ForEach-Object {
        Split-Path -Leaf $_
    } | Where-Object {
        $_ -like "$wordToComplete*"
    } | ForEach-Object {
        [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
    }
}
`

// Bash is the init snippet for bash. Bash has no tab completion snippet
// here: only zsh, fish and PowerShell do, per the "cd" completion
// acceptance criterion.
func Bash() string { return bashLike }

// Zsh is the init snippet for zsh: the shared wrapper function plus "cd"
// tab completion.
func Zsh() string { return bashLike + zshCompletion }

// Fish is the init snippet for fish: the wrapper function plus "cd" tab
// completion.
func Fish() string { return fish + fishCompletion }

// PowerShell is the init snippet for PowerShell, both Windows PowerShell
// 5.1 and PowerShell 7: the wrapper function plus "cd" tab completion.
func PowerShell() string { return powerShell + powerShellCompletion }

// Init returns the init snippet for shell, one of "bash", "zsh", "fish",
// "pwsh" or "powershell". "pwsh" names PowerShell 7 and "powershell"
// names Windows PowerShell 5.1; both get the identical snippet, since
// nothing in it needs the two editions to differ.
func Init(shell string) (string, error) {
	switch shell {
	case "bash":
		return Bash(), nil
	case "zsh":
		return Zsh(), nil
	case "fish":
		return Fish(), nil
	case "pwsh", "powershell":
		return PowerShell(), nil
	default:
		return "", fmt.Errorf("shell: %q is not bash, zsh, fish, pwsh or powershell", shell)
	}
}
