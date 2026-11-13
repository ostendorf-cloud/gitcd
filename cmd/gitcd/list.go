// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
	"github.com/ostendorf-cloud/gitcd/internal/scan"
)

// listEntry is one repository "gitcd list" reports, the same shape for
// plain text and "--json".
type listEntry struct {
	Path     string `json:"path"`
	Remote   string `json:"remote,omitempty"`
	Worktree bool   `json:"worktree"`
}

// runList implements "gitcd list": it prints every repository scan.Walk
// finds under the configured root, one path per line, hiding a linked
// worktree unless "--worktrees" is given. "--json" prints the same
// repositories as a JSON array instead of plain text.
func runList(args []string, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("gitcd list", flag.ContinueOnError)
	fset.SetOutput(stderr)
	jsonOut := fset.Bool("json", false, "print the result as a JSON array instead of plain text")
	withWorktrees := fset.Bool("worktrees", false, "include a linked worktree, hidden by default")
	verbose := fset.Bool("verbose", false, "enable debug logging on stderr")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() != 0 {
		fmt.Fprintln(stderr, "gitcd list: no arguments are accepted")
		fset.Usage()
		return 2
	}
	logger := newLogger(*verbose, stderr)
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd list: %v\n", err)
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd list: %v\n", err)
		return 1
	}

	entries, err := listEntries(ctx, cfg, gitconfig.Dir(home), *withWorktrees, logger)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd list: %v\n", err)
		return 1
	}

	if *jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(entries); err != nil {
			fmt.Fprintf(stderr, "gitcd list: %v\n", err)
			return 1
		}
		return 0
	}

	for _, e := range entries {
		fmt.Fprintln(stdout, e.Path)
	}
	return 0
}

// listEntries scans cfg.Root and reports one listEntry per repository scan.Walk
// found, skipping a linked worktree unless withWorktrees is set and
// skipping, with a warning on stderr through logger, a repository
// scan.Walk could not inspect, most commonly one with no configured
// remote.
func listEntries(ctx context.Context, cfg config.Config, managedDir string, withWorktrees bool, logger *slog.Logger) ([]listEntry, error) {
	results, err := scan.Walk(ctx, cfg.Root, cfg, managedDir)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}

	entries := make([]listEntry, 0, len(results))
	for _, res := range results {
		if res.Err != nil {
			logger.Warn("skipped a repository scan could not inspect", "path", res.Repo.Path, "error", res.Err)
			continue
		}
		if res.Repo.IsWorktree && !withWorktrees {
			continue
		}
		entries = append(entries, listEntry{
			Path:     res.Repo.Path,
			Remote:   res.Repo.RemoteURL,
			Worktree: res.Repo.IsWorktree,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}
