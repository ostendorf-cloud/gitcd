// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package scan walks the configured clone root into the repository values
// "list", "doctor" and "status" consume. A directory carrying a ".git"
// entry, file or directory alike, is a repository boundary: scan reports it
// and does not descend any further, so a linked worktree, whose own ".git"
// is a file pointing back at its main checkout, is never mistaken for a
// nested repository to walk into, the same rule that also keeps a
// submodule from being reported twice.
package scan

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/git"
	"github.com/ostendorf-cloud/gitcd/internal/profile"
)

// Result is one repository boundary Walk found under root, inspected
// against cfg. Err is set, with Repo carrying only Path, when
// profile.Inspect could not resolve the repository, most commonly because
// it carries no "remote.origin.url" at all: Walk still reports it rather
// than dropping it silently, so a caller can decide whether to skip it or
// fail.
type Result struct {
	Repo profile.Repo
	Err  error
}

// Walk walks root for every repository boundary under it and reports one
// Result per match, in the deterministic lexical order filepath.WalkDir
// already visits directories in. managedDir is profile.Inspect's own
// "gitcd apply" managed directory, passed through unchanged. A root that
// does not exist yet yields no results and no error: a clone root nothing
// has ever been cloned into is not a failure.
func Walk(ctx context.Context, root string, cfg config.Config, managedDir string) ([]Result, error) {
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan: stat %s: %w", root, err)
	}

	var results []Result
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("scan: walk %s: %w", path, err)
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() == ".git" {
			// Never descend into a ".git" directory's own contents: it is
			// never itself a repository boundary one level further down.
			return filepath.SkipDir
		}
		if path == root {
			return nil
		}
		if !git.IsRepository(path) {
			return nil
		}

		repo, inspectErr := profile.Inspect(ctx, path, cfg, managedDir)
		if inspectErr != nil {
			results = append(results, Result{Repo: profile.Repo{Path: path}, Err: inspectErr})
		} else {
			results = append(results, Result{Repo: repo})
		}
		// This path is a repository boundary: whatever it holds beneath
		// it, a linked worktree's private metadata, a submodule or a
		// nested checkout, is never a second, separate match.
		return filepath.SkipDir
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}
