// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ostendorf-cloud/gitcd/internal/config"
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

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd path: %v\n", err)
		return 1
	}
	logger.Debug("loaded configuration", "root", cfg.Root)

	if strings.Contains(query, "/") {
		return pathFromShorthand(stdout, stderr, cfg, query)
	}
	return pathFromName(stdout, stderr, cfg, query)
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
// every repository directly under root, erroring when none or more than
// one matches rather than guessing: fuzzy matching belongs to "cd", the
// next milestone's command.
func pathFromName(stdout, stderr io.Writer, cfg config.Config, name string) int {
	matches, err := findByName(cfg.Root, name)
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

// findByName reports every repository under root whose own directory
// name is exactly name, built on discoverRepos's walk.
func findByName(root, name string) ([]string, error) {
	repos, err := discoverRepos(root)
	if err != nil {
		return nil, err
	}

	var matches []string
	for _, path := range repos {
		if filepath.Base(path) == name {
			matches = append(matches, path)
		}
	}
	return matches, nil
}
