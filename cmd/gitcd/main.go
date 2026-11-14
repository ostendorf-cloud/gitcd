// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Command gitcd clones repositories into a predictable path layout and
// lets the shell jump to any of them. This binary holds flag parsing and
// command dispatch only; every command's logic lives in internal.
package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
)

// Version is set at build time from the release tag.
var Version = "dev"

const usage = `gitcd clones repositories into a predictable path layout and lets the shell jump to any of them.

Usage:
  gitcd <command> [flags]

Commands:
  clone        clone a repository into its rendered path, printing the path
  path         resolve a repository name or host/owner/repo to its path
  list         list every repository under the configured root
  cd           resolve a partial repository name to its path, printing it
  init         add the identity profile include to the global git config
  apply        render and write the identity profile files
  profile      pin, remove or inspect a repository's identity profile
  status       print branch, dirty and ahead or behind state for every repository
  sync         fetch every repository with a bounded pool, never pulling or merging
  open         open a repository's remote in a browser
  doctor       report identity and layout drift across every repository
  adopt        move an existing clone into the layout and fix its config
  shell-init   print the shell integration snippet for zsh, bash or fish
  version      print the version

Every command prints its result on stdout and everything else on stderr.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, Version)
		return 0
	case "clone":
		return runClone(args[1:], stdout, stderr)
	case "path":
		return runPath(args[1:], stdout, stderr)
	case "list":
		return runList(args[1:], stdout, stderr)
	case "cd":
		return runCd(args[1:], os.Stdin, isTerminalFile(os.Stdin), stdout, stderr)
	case "init":
		return runInit(args[1:], os.Stdin, stdout, stderr)
	case "apply":
		return runApply(args[1:], stdout, stderr)
	case "profile":
		return runProfile(args[1:], stdout, stderr)
	case "status":
		return runStatus(args[1:], stdout, stderr)
	case "sync":
		return runSync(args[1:], stdout, stderr)
	case "open":
		return runOpen(args[1:], stdout, stderr)
	case "doctor":
		return runDoctor(args[1:], stdout, stderr)
	case "adopt":
		return runAdopt(args[1:], os.Stdin, stdout, stderr)
	case "shell-init":
		return runShellInit(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "gitcd: unknown command %q\n%s", args[0], usage)
		return 2
	}
}

// newLogger builds the logger every command that does meaningful work
// configures once from its own "--verbose" flag: quiet by default, debug
// logging on stderr when verbose is set.
func newLogger(verbose bool, stderr io.Writer) *slog.Logger {
	level := slog.LevelWarn
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))
}
