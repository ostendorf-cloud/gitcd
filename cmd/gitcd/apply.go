// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
)

// runApply implements "gitcd apply": it renders every profile file and the
// include file "gitcd init" points the user's global git configuration at,
// and writes them. "--dry-run" prints the rendered files instead of
// writing them.
func runApply(args []string, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("gitcd apply", flag.ContinueOnError)
	fset.SetOutput(stderr)
	dryRun := fset.Bool("dry-run", false, "print the rendered files without writing them")
	verbose := fset.Bool("verbose", false, "enable debug logging on stderr")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() != 0 {
		fmt.Fprintln(stderr, "gitcd apply: no arguments are accepted")
		fset.Usage()
		return 2
	}
	logger := newLogger(*verbose, stderr)

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd apply: %v\n", err)
		return 1
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd apply: %v\n", err)
		return 1
	}

	rendered, err := gitconfig.Render(cfg, home)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd apply: %v\n", err)
		return 1
	}

	paths := make([]string, 0, len(rendered))
	for path := range rendered {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	if *dryRun {
		for _, path := range paths {
			fmt.Fprintf(stdout, "%s:\n%s\n", path, rendered[path])
		}
		return 0
	}

	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fmt.Fprintf(stderr, "gitcd apply: create %s: %v\n", filepath.Dir(path), err)
			return 1
		}
		if err := os.WriteFile(path, []byte(rendered[path]), 0o644); err != nil {
			fmt.Fprintf(stderr, "gitcd apply: write %s: %v\n", path, err)
			return 1
		}
		logger.Debug("wrote a managed file", "path", path)
		fmt.Fprintln(stdout, path)
	}
	return 0
}
