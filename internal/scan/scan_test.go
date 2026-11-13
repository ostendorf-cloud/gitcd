// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package scan_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/scan"
)

// setupGitEnv isolates every git invocation the test makes from the real
// user's configuration and from the network.
func setupGitEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	globalConfig := filepath.Join(home, ".gitconfig")
	if err := os.WriteFile(globalConfig, []byte("[user]\n\tname = Test User\n\temail = [email protected]\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", globalConfig, err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return home
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

// initRepository creates a git repository under dir with one committed
// file and an origin remote, since profile.Inspect, which Walk calls for
// every match, needs one to resolve.
func initRepository(t *testing.T, dir, remoteURL string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	runGit(t, dir, "init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("example\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "add README")
	runGit(t, dir, "remote", "add", "origin", remoteURL)
}

func paths(results []scan.Result) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Repo.Path
	}
	sort.Strings(out)
	return out
}

func TestWalk_FindsEveryRepositoryUnderRoot(t *testing.T) {
	home := setupGitEnv(t)
	root := filepath.Join(home, "Code")

	first := filepath.Join(root, "github.com", "acme", "widget")
	second := filepath.Join(root, "gitlab.com", "acme", "widget")
	initRepository(t, first, "https://github.com/acme/widget.git")
	initRepository(t, second, "https://gitlab.com/acme/widget.git")

	results, err := scan.Walk(context.Background(), root, config.Config{}, filepath.Join(home, ".config", "gitcd"))
	if err != nil {
		t.Fatalf("Walk() returned an unexpected error: %v", err)
	}

	got := paths(results)
	want := []string{first, second}
	sort.Strings(want)
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Walk() paths = %v, want %v", got, want)
	}
	for _, r := range results {
		if r.Err != nil {
			t.Fatalf("Walk() result for %s carries an unexpected error: %v", r.Repo.Path, r.Err)
		}
		if r.Repo.IsWorktree {
			t.Fatalf("Walk() marked %s as a worktree, want an ordinary clone", r.Repo.Path)
		}
	}
}

func TestWalk_MissingRootYieldsNoResultsAndNoError(t *testing.T) {
	home := setupGitEnv(t)
	root := filepath.Join(home, "never-created")

	results, err := scan.Walk(context.Background(), root, config.Config{}, filepath.Join(home, ".config", "gitcd"))
	if err != nil {
		t.Fatalf("Walk() returned an unexpected error for a missing root: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("Walk() = %v, want no results for a missing root", results)
	}
}

// TestWalk_StopsDescendingAtARepositoryBoundary plants a second,
// independent repository nested inside the working tree of the first, the
// shape a vendored checkout or an unregistered submodule takes on disk, and
// asserts Walk never reports the nested one: the outer repository's own
// ".git" is the boundary, and Walk must not look any further inside it.
func TestWalk_StopsDescendingAtARepositoryBoundary(t *testing.T) {
	home := setupGitEnv(t)
	root := filepath.Join(home, "Code")

	outer := filepath.Join(root, "github.com", "acme", "outer")
	initRepository(t, outer, "https://github.com/acme/outer.git")

	nested := filepath.Join(outer, "vendor", "inner")
	initRepository(t, nested, "https://github.com/acme/inner.git")

	results, err := scan.Walk(context.Background(), root, config.Config{}, filepath.Join(home, ".config", "gitcd"))
	if err != nil {
		t.Fatalf("Walk() returned an unexpected error: %v", err)
	}

	got := paths(results)
	if len(got) != 1 || got[0] != outer {
		t.Fatalf("Walk() paths = %v, want only %v, the nested repository must not be descended into", got, []string{outer})
	}
}

// TestWalk_ReportsALinkedWorktreeWithoutDescendingIntoIt covers the
// acceptance criterion directly: a linked worktree's own ".git" is a file,
// not a directory, and Walk must still treat it as a repository boundary
// rather than silently skipping it or walking into whatever it names.
func TestWalk_ReportsALinkedWorktreeWithoutDescendingIntoIt(t *testing.T) {
	home := setupGitEnv(t)
	root := filepath.Join(home, "Code")

	main := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, main, "https://github.com/acme/widget.git")

	worktree := filepath.Join(root, "github.com", "acme", "widget-feature")
	runGit(t, main, "worktree", "add", worktree)

	info, err := os.Lstat(filepath.Join(worktree, ".git"))
	if err != nil {
		t.Fatalf("stat the worktree's own .git: %v", err)
	}
	if info.IsDir() {
		t.Fatalf("%s/.git is a directory, want a file: the probe this test relies on no longer holds", worktree)
	}

	results, err := scan.Walk(context.Background(), root, config.Config{}, filepath.Join(home, ".config", "gitcd"))
	if err != nil {
		t.Fatalf("Walk() returned an unexpected error: %v", err)
	}

	got := paths(results)
	want := []string{main, worktree}
	sort.Strings(want)
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Walk() paths = %v, want %v", got, want)
	}

	for _, r := range results {
		if r.Err != nil {
			t.Fatalf("Walk() result for %s carries an unexpected error: %v", r.Repo.Path, r.Err)
		}
		if r.Repo.Path == worktree && !r.Repo.IsWorktree {
			t.Fatalf("Walk() did not mark %s as a worktree", worktree)
		}
		if r.Repo.Path == main && r.Repo.IsWorktree {
			t.Fatalf("Walk() marked the main checkout %s as a worktree", main)
		}
	}
}

// TestWalk_ReportsALinkedWorktreeNestedInsideItsOwnRepository covers the
// shape TestWalk_ReportsALinkedWorktreeWithoutDescendingIntoIt does not: a
// linked worktree placed inside its own repository's working tree, at
// "<repo>/.claude/worktrees/<branch>", exactly the layout this
// organisation's own worktree convention produces. The repository
// boundary rule's "filepath.SkipDir" means Walk's own directory walk never
// looks inside the main checkout to find it, so Walk must surface it some
// other way, not by walking.
func TestWalk_ReportsALinkedWorktreeNestedInsideItsOwnRepository(t *testing.T) {
	home := setupGitEnv(t)
	root := filepath.Join(home, "Code")

	main := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, main, "https://github.com/acme/widget.git")

	worktree := filepath.Join(main, ".claude", "worktrees", "feat-x")
	runGit(t, main, "worktree", "add", worktree, "-b", "feat-x")

	results, err := scan.Walk(context.Background(), root, config.Config{}, filepath.Join(home, ".config", "gitcd"))
	if err != nil {
		t.Fatalf("Walk() returned an unexpected error: %v", err)
	}

	got := paths(results)
	want := []string{main, worktree}
	sort.Strings(want)
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Walk() paths = %v, want %v: a worktree nested inside its own repository must still be reported", got, want)
	}

	for _, r := range results {
		if r.Err != nil {
			t.Fatalf("Walk() result for %s carries an unexpected error: %v", r.Repo.Path, r.Err)
		}
		if r.Repo.Path == worktree && !r.Repo.IsWorktree {
			t.Fatalf("Walk() did not mark the nested worktree %s as a worktree", worktree)
		}
		if r.Repo.Path == main && r.Repo.IsWorktree {
			t.Fatalf("Walk() marked the main checkout %s as a worktree", main)
		}
	}
}

// TestWalk_ReportsAnUninspectableRepositoryRatherThanDroppingIt covers a
// repository Walk finds but profile.Inspect cannot resolve, a clone with
// no origin remote configured: Walk must still report it, with Err set,
// rather than silently excluding it from the result.
func TestWalk_ReportsAnUninspectableRepositoryRatherThanDroppingIt(t *testing.T) {
	home := setupGitEnv(t)
	root := filepath.Join(home, "Code")

	noRemote := filepath.Join(root, "local-only")
	if err := os.MkdirAll(noRemote, 0o755); err != nil {
		t.Fatalf("create %s: %v", noRemote, err)
	}
	runGit(t, noRemote, "init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(noRemote, "README.md"), []byte("example\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	runGit(t, noRemote, "add", "README.md")
	runGit(t, noRemote, "commit", "-m", "add README")

	results, err := scan.Walk(context.Background(), root, config.Config{}, filepath.Join(home, ".config", "gitcd"))
	if err != nil {
		t.Fatalf("Walk() returned an unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Walk() = %d results, want 1", len(results))
	}
	if results[0].Repo.Path != noRemote {
		t.Fatalf("Walk() result path = %q, want %q", results[0].Repo.Path, noRemote)
	}
	if results[0].Err == nil {
		t.Fatal("Walk() result carries no error for a repository with no configured remote, want one")
	}
}
