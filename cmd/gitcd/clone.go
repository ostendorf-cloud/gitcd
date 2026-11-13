// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/git"
	"github.com/ostendorf-cloud/gitcd/internal/layout"
	"github.com/ostendorf-cloud/gitcd/internal/remote"
)

// runClone implements "gitcd clone <url|shorthand>". It prints the path on
// stdout and is idempotent: a target that already exists with the same
// remote prints that path and exits 0, and one that exists with a
// different remote fails, naming both.
func runClone(args []string, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("gitcd clone", flag.ContinueOnError)
	fset.SetOutput(stderr)
	dryRun := fset.Bool("dry-run", false, "print the target path without cloning")
	verbose := fset.Bool("verbose", false, "enable debug logging on stderr")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() != 1 {
		fmt.Fprintln(stderr, "gitcd clone: exactly one remote or host/owner/repo shorthand is required")
		fset.Usage()
		return 2
	}
	input := fset.Arg(0)
	logger := newLogger(*verbose, stderr)
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd clone: %v\n", err)
		return 1
	}

	r, err := remote.Parse(input, cfg.AliasMap())
	if err != nil {
		fmt.Fprintf(stderr, "gitcd clone: %v\n", err)
		return 1
	}
	logger.Debug("parsed remote", "host", r.Host, "owner", r.Owner, "project", r.Project, "repo", r.Repo)

	rel, err := layout.Render(cfg.LayoutFor(r.Host), r)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd clone: %v\n", err)
		return 1
	}
	dest := filepath.Join(cfg.Root, rel)
	logger.Debug("rendered path", "path", dest)

	if git.IsRepository(dest) {
		return existingClone(ctx, stdout, stderr, cfg, dest, input, r)
	}

	if *dryRun {
		fmt.Fprintln(stdout, dest)
		return 0
	}

	cloneURL := input
	if remote.IsShorthand(input) {
		protocol := cfg.Hosts[strings.ToLower(r.Host)].Protocol
		cloneURL, err = r.URL(protocol)
		if err != nil {
			fmt.Fprintf(stderr, "gitcd clone: %v\n", err)
			return 1
		}
	}
	logger.Debug("cloning", "url", cloneURL, "dest", dest)

	if err := git.Clone(ctx, cloneURL, dest); err != nil {
		fmt.Fprintf(stderr, "gitcd clone: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, dest)
	return 0
}

// existingClone handles "gitcd clone" for a dest that already holds a
// repository: it reads dest's own origin remote and compares it, by its
// parsed fields rather than its literal text, against the one just
// requested, so an SSH and an HTTPS URL for the same repository are not
// mistaken for a conflict.
func existingClone(
	ctx context.Context, stdout, stderr io.Writer, cfg config.Config, dest, input string, want remote.Remote,
) int {
	existingURL, err := git.RemoteURL(ctx, dest)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd clone: %s already exists: %v\n", dest, err)
		return 1
	}

	existing, err := remote.Parse(existingURL, cfg.AliasMap())
	if err != nil {
		fmt.Fprintf(
			stderr, "gitcd clone: %s already exists with a remote gitcd cannot parse: %s\n", dest, existingURL,
		)
		return 1
	}

	if existing == want {
		fmt.Fprintln(stdout, dest)
		return 0
	}

	fmt.Fprintf(
		stderr, "gitcd clone: %s already exists with a different remote: have %s, want %s\n", dest, existingURL, input,
	)
	return 1
}
