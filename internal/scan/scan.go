// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package scan walks the configured clone root into the repository values
// "list", "doctor" and "status" consume. A directory carrying a ".git"
// entry, file or directory alike, is a repository boundary: scan reports it
// and does not descend any further, so a linked worktree, whose own ".git"
// is a file pointing back at its main checkout, is never mistaken for a
// nested repository to walk into, the same rule that also keeps a
// submodule from being reported twice. That boundary rule also means the
// walk itself can never see a linked worktree that lives inside its own
// repository's working tree, such as this organisation's own
// "<repo>/.claude/worktrees/<branch>" convention: once a repository
// boundary is found, scan asks git itself which worktrees it has
// registered for it, rather than looking any further inside a path the
// boundary rule always skips past.
package scan

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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

	// git reports a worktree's own path resolved through any symlink,
	// while filepath.WalkDir never resolves one in root itself: root is
	// resolved once here purely to recognise a path git reports as lying
	// under root, never to change a Result's own Path, which always stays
	// expressed against root exactly as given.
	resolvedRoot := root
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		resolvedRoot = resolved
	}

	var results []Result
	seen := make(map[string]bool)
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

		if err := inspectBoundary(ctx, path, root, resolvedRoot, cfg, managedDir, &results, seen); err != nil {
			return err
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

// inspectBoundary inspects the repository boundary at path, appending its
// Result to results unless path was already reported. When path is a
// repository's main checkout, rather than a linked worktree itself, it
// also asks git which worktrees it has registered for that repository and
// recurses into every one of them that sits under root and was not
// already seen, so a linked worktree nested inside its own repository's
// working tree is still reported even though filepath.WalkDir's own
// boundary rule, applied by the caller, never looks inside path to find
// it. seen is shared across the whole walk, keyed by path, so a sibling
// worktree WalkDir also visits independently is never reported twice.
// resolvedRoot is root with any symlink resolved, matching the form git
// itself reports a worktree's path in, used only to recognise which
// worktree sits under root; every path actually inspected and stored
// stays expressed against root exactly as given.
func inspectBoundary(ctx context.Context, path, root, resolvedRoot string, cfg config.Config, managedDir string, results *[]Result, seen map[string]bool) error {
	if seen[path] {
		return nil
	}
	seen[path] = true

	repo, inspectErr := profile.Inspect(ctx, path, cfg, managedDir)
	if inspectErr != nil {
		*results = append(*results, Result{Repo: profile.Repo{Path: path}, Err: inspectErr})
		return nil
	}
	*results = append(*results, Result{Repo: repo})

	if repo.IsWorktree {
		// A linked worktree shares its main checkout's registry: asking
		// it again for the same list would only repeat work already done
		// from the main checkout, found either earlier or later in this
		// same walk.
		return nil
	}

	worktrees, err := git.Worktrees(ctx, path)
	if err != nil {
		return fmt.Errorf("scan: list the worktrees of %s: %w", path, err)
	}
	for _, worktree := range worktrees {
		candidate, ok := relocateUnderRoot(filepath.Clean(worktree), root, resolvedRoot)
		if !ok || candidate == path || seen[candidate] {
			continue
		}
		if err := inspectBoundary(ctx, candidate, root, resolvedRoot, cfg, managedDir, results, seen); err != nil {
			return err
		}
	}
	return nil
}

// relocateUnderRoot reports whether worktree, a path git reported with any
// symlink in root already resolved, lies under resolvedRoot, and, when it
// does, returns it rejoined onto root exactly as given: the same literal
// form every other Result's Path already carries, since root's own callers
// configure and compare it unresolved.
func relocateUnderRoot(worktree, root, resolvedRoot string) (path string, ok bool) {
	rel, err := filepath.Rel(resolvedRoot, worktree)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	if rel == "." {
		return root, true
	}
	return filepath.Join(root, rel), true
}
