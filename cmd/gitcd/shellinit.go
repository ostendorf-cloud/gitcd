// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/ostendorf-cloud/gitcd/internal/shell"
)

// runShellInit implements "gitcd shell-init zsh|bash|fish": it prints the
// snippet that defines the shell function wrapping this binary, meant to
// be sourced with "eval \"$(gitcd shell-init zsh)\"".
func runShellInit(args []string, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("gitcd shell-init", flag.ContinueOnError)
	fset.SetOutput(stderr)
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() != 1 {
		fmt.Fprintln(stderr, "gitcd shell-init: exactly one shell, zsh, bash or fish, is required")
		fset.Usage()
		return 2
	}

	snippet, err := shell.Init(fset.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "gitcd shell-init: %v\n", err)
		return 1
	}

	fmt.Fprint(stdout, snippet)
	return 0
}
