// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"io"
)

// runVersion implements "gitcd version": it prints the version this binary
// was built with on stdout. It takes no argument and no flag, and carries a
// flag set for that reason alone: parsing rejects an unknown flag, so a
// typed flag that does not exist is named rather than silently dropped.
func runVersion(args []string, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("gitcd version", flag.ContinueOnError)
	fset.SetOutput(stderr)
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() != 0 {
		fmt.Fprintln(stderr, "gitcd version: no arguments are accepted")
		fset.Usage()
		return 2
	}

	fmt.Fprintln(stdout, Version)
	return 0
}
