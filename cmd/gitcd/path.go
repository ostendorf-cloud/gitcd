// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
	"github.com/ostendorf-cloud/gitcd/internal/layout"
	"github.com/ostendorf-cloud/gitcd/internal/remote"
)

// runPath implements "gitcd path <query>", the plumbing command
// "shell-init"'s wrapper function calls: it resolves query, either an
// exact repository name or a "host/owner/repo" shorthand, to the path on
// disk and prints it on stdout.
func runPath(args []string, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("gitcd path", flag.ContinueOnError)
	fset.SetOutput(stderr)
	verbose := fset.Bool("verbose", false, "enable debug logging on stderr")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() != 1 {
		fmt.Fprintln(stderr, "gitcd path: exactly one query is required")
		fset.Usage()
		return 2
	}
	query := fset.Arg(0)
	logger := newLogger(*verbose, stderr)
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd path: %v\n", err)
		return 1
	}
	logger.Debug("loaded configuration", "root", cfg.Root)

	if strings.Contains(query, "/") {
		return pathFromShorthand(stdout, stderr, cfg, query)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd path: %v\n", err)
		return 1
	}
	return pathFromName(ctx, stdout, stderr, cfg, gitconfig.Dir(home), query, logger)
}

// pathFromShorthand resolves a "host/owner/repo" query by rendering the
// path it would clone into and checking it already exists.
func pathFromShorthand(stdout, stderr io.Writer, cfg config.Config, query string) int {
	r, err := remote.Parse(query, cfg.AliasMap())
	if err != nil {
		fmt.Fprintf(stderr, "gitcd path: %v\n", err)
		return 1
	}
	rel, err := layout.Render(cfg.LayoutFor(r.Host), r)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd path: %v\n", err)
		return 1
	}
	dest := filepath.Join(cfg.Root, rel)
	if _, err := os.Stat(dest); err != nil {
		fmt.Fprintf(stderr, "gitcd path: %s has not been cloned yet\n", dest)
		return 1
	}
	fmt.Fprintln(stdout, dest)
	return 0
}

// pathFromName resolves a bare repository name by its exact match against
// every repository under root, a linked worktree included, erroring when
// none or more than one matches rather than guessing: fuzzy matching
// belongs to "cd".
func pathFromName(
	ctx context.Context, stdout, stderr io.Writer, cfg config.Config, managedDir, name string, logger *slog.Logger,
) int {
	matches, err := findByName(ctx, cfg, managedDir, name, logger)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd path: search %s: %v\n", cfg.Root, err)
		return 1
	}

	switch len(matches) {
	case 0:
		fmt.Fprintf(stderr, "gitcd path: no repository named %q under %s\n", name, cfg.Root)
		return 1
	case 1:
		fmt.Fprintln(stdout, matches[0])
		return 0
	default:
		fmt.Fprintf(stderr, "gitcd path: %q matches more than one repository:\n", name)
		for _, m := range matches {
			fmt.Fprintf(stderr, "  %s\n", m)
		}
		return 1
	}
}
