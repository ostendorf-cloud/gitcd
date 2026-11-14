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

// listEntries scans cfg.Root and reports one listEntry per repository
// scanRepositories found, hiding a linked worktree unless withWorktrees
// is set: the one exception to every other command's rule, since a
// worktree is not itself a distinct clone someone maintains in the fleet
// listing. See scanRepositories's own doc comment.
func listEntries(ctx context.Context, cfg config.Config, managedDir string, withWorktrees bool, logger *slog.Logger) ([]listEntry, error) {
	results, err := scanRepositories(ctx, cfg, managedDir, withWorktrees)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	results = keepInspectable(results, logger)

	entries := make([]listEntry, 0, len(results))
	for _, res := range results {
		entries = append(entries, listEntry{
			Path:     res.Repo.Path,
			Remote:   res.Repo.RemoteURL,
			Worktree: res.Repo.IsWorktree,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}
