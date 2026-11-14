// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/scan"
)

// scanRepositories walks cfg.Root through internal/scan, the one walk
// every command that needs the repositories under root uses, and applies
// this command's own answer to the one question every one of them must
// otherwise decide separately: whether a linked worktree, reported by
// scan.Walk as a repository boundary in its own right, counts as one of
// the repositories this call reports back. A Result scan.Walk could not
// inspect, most commonly one with no configured remote, is never dropped
// here: its own IsWorktree reads false either way, so it always passes
// this filter; keepInspectable is where a caller that cannot act on an
// uninspectable repository drops it instead.
//
// The rule this package applies, by command, stated once here rather than
// answered separately by each caller:
//
//   - "list" passes includeWorktrees only when "--worktrees" was given: a
//     linked worktree is not itself a distinct clone someone maintains in
//     the fleet listing, so it stays hidden by default.
//   - "cd", "path" and "open" always pass true: each resolves a query to
//     one specific repository, and a worktree is exactly as valid a
//     destination as its main checkout, the same way a branch name must
//     resolve to the worktree checked out to it.
//   - "status" always passes true: a linked worktree carries its own
//     branch and its own dirty state, distinct from its main checkout's,
//     so it earns its own row.
//   - "sync" and "doctor" always pass false: every worktree of one
//     repository shares that repository's remote and local git
//     configuration, so fetching or checking one again repeats exactly
//     what its main checkout's own pass already covered.
func scanRepositories(ctx context.Context, cfg config.Config, managedDir string, includeWorktrees bool) ([]scan.Result, error) {
	results, err := scan.Walk(ctx, cfg.Root, cfg, managedDir)
	if err != nil {
		return nil, err
	}

	kept := make([]scan.Result, 0, len(results))
	for _, res := range results {
		if res.Repo.IsWorktree && !includeWorktrees {
			continue
		}
		kept = append(kept, res)
	}
	return kept, nil
}

// keepInspectable drops a Result scanRepositories could not resolve,
// logging it as a warning through logger rather than failing the whole
// command: "list", "cd" and "doctor" each act on the resolved remote and
// identity profile.Inspect provides, so a repository it could not
// resolve, most commonly one with no configured remote, carries nothing
// they can act on. "status", "sync", "path" and "open" need only a
// repository's own path, which a Result carries even when it could not
// be inspected, so they call scanRepositories directly instead and keep
// such a repository rather than dropping it.
func keepInspectable(results []scan.Result, logger *slog.Logger) []scan.Result {
	kept := make([]scan.Result, 0, len(results))
	for _, res := range results {
		if res.Err != nil {
			logger.Warn("skipped a repository scan could not inspect", "path", res.Repo.Path, "error", res.Err)
			continue
		}
		kept = append(kept, res)
	}
	return kept
}

// repoName renders path as the name "status", "sync", "path", "open" and
// "doctor" report it under: path relative to root, "/" separated
// regardless of platform, so a long host/owner/repo path still reads as
// one recognisable unit rather than an ambiguous bare repository name two
// different owners could both carry.
func repoName(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

// findByName reports every repository scanRepositories finds under
// cfg.Root, a linked worktree included, whose own directory name is
// exactly name: "path" and "open" both resolve a bare query this way,
// erroring when none or more than one matches rather than guessing.
// Including a worktree in the candidates searched is deliberate, not an
// oversight: see scanRepositories's own doc comment. A repository
// scanRepositories could not resolve is matched on its bare path all the
// same, exactly as the walk "path" and "open" used before this one
// replaced it: neither needs anything profile.Inspect resolves, only the
// path itself.
func findByName(ctx context.Context, cfg config.Config, managedDir, name string, logger *slog.Logger) ([]string, error) {
	results, err := scanRepositories(ctx, cfg, managedDir, true)
	if err != nil {
		return nil, err
	}

	var matches []string
	for _, res := range results {
		if res.Err != nil {
			logger.Debug("a repository scan could not fully inspect still matched by name", "path", res.Repo.Path, "error", res.Err)
		}
		if filepath.Base(res.Repo.Path) == name {
			matches = append(matches, res.Repo.Path)
		}
	}
	return matches, nil
}
