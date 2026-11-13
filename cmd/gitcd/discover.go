// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/ostendorf-cloud/gitcd/internal/git"
)

// discoverRepos walks root for every repository under it, stopping its
// descent the moment it finds one, so a submodule or a linked worktree
// nested inside a match is never itself reported as a second one. It
// treats a ".git" entry, a directory for an ordinary clone or a file for
// a linked worktree, as a repository boundary, the same rule
// git.IsRepository applies everywhere else in this command. A root that
// does not exist yet yields no repositories, not an error: a fleet with
// nothing cloned into it yet is an empty fleet, not a failure.
//
// This is deliberately small and local to cmd/gitcd: issue #3 introduces
// internal/scan, a walk built for "list", "doctor" and "adopt" with a
// richer Repo value than "status" and "sync" need here. The two walks
// cover the same ground and should be reconciled into one once both have
// landed, rather than carried as two copies of the same traversal.
//
// findByName already walked root this same way for "gitcd path", filtered
// to one name; it is rewritten here to call discoverRepos instead of
// repeating the walk a second time within this package.
func discoverRepos(root string) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("discover: stat %s: %w", root, err)
	}

	var repos []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() == ".git" {
			return filepath.SkipDir
		}
		if path == root {
			return nil
		}
		if git.IsRepository(path) {
			repos = append(repos, path)
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover: walk %s: %w", root, err)
	}
	sort.Strings(repos)
	return repos, nil
}

// repoName renders path as the name "status", "sync" and "open" report
// it under: path relative to root, "/" separated regardless of platform,
// so a long host/owner/repo path still reads as one recognisable unit
// rather than an ambiguous bare repository name two different owners
// could both carry.
func repoName(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}
