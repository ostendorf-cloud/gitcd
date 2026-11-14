// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cloneFromBareRemote creates a bare repository with one commit and
// clones it into dest, so "gitcd sync" has a real, local-only remote to
// fetch from.
func cloneFromBareRemote(t *testing.T, root, dest string) {
	t.Helper()
	bare := dest + "-bare.git"
	source := dest + "-source"
	initRepository(t, source)
	runGit(t, root, "clone", "--bare", source, bare)

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(dest), err)
	}
	runGit(t, root, "clone", bare, dest)
}

// advanceBareRemote commits a second file in source and pushes it to
// bare, the clone's own remote, so a later "git fetch" in the clone has
// something new to bring down.
func advanceBareRemote(t *testing.T, source, bare string) {
	t.Helper()
	writeFile(t, filepath.Join(source, "second.txt"), "second\n")
	runGit(t, source, "add", "second.txt")
	runGit(t, source, "commit", "-m", "second commit")
	runGit(t, source, "push", bare, "HEAD:main")
}

func TestRunSync_FetchesEveryRepository(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	first := filepath.Join(root, "host", "acme", "first")
	second := filepath.Join(root, "host", "acme", "second")
	cloneFromBareRemote(t, home, first)
	cloneFromBareRemote(t, home, second)

	// Advance each remote by one commit after cloning, so the fetch
	// "sync" runs has something to actually bring down, rather than
	// succeeding regardless of whether it ran at all.
	advanceBareRemote(t, first+"-source", first+"-bare.git")
	advanceBareRemote(t, second+"-source", second+"-bare.git")

	var stdout, stderr bytes.Buffer
	code := run([]string{"sync"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("sync = %d, want 0, stderr: %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("sync wrote to stdout: %q, want everything on stderr", stdout.String())
	}

	for _, name := range []string{"host/acme/first", "host/acme/second"} {
		if !strings.Contains(stderr.String(), name) {
			t.Fatalf("sync stderr is missing a line for %q: %s", name, stderr.String())
		}
	}

	for _, repo := range []string{first, second} {
		out := runGitOutput(t, repo, "log", "-1", "--format=%s", "origin/main")
		if out != "second commit" {
			t.Fatalf("%s: origin/main = %q, want the fetch to have brought in the remote's new commit", repo, out)
		}
	}
}

// TestRunSync_ReportsPerRepositoryFailuresWithoutAbortingTheRun guards
// the acceptance criterion directly: a repository whose remote is gone
// must fail on its own, every other repository must still be fetched,
// and the command as a whole must exit non-zero.
func TestRunSync_ReportsPerRepositoryFailuresWithoutAbortingTheRun(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	broken := filepath.Join(root, "host", "acme", "broken")
	ok := filepath.Join(root, "host", "acme", "ok")
	cloneFromBareRemote(t, home, broken)
	cloneFromBareRemote(t, home, ok)
	advanceBareRemote(t, ok+"-source", ok+"-bare.git")

	// Remove the broken repository's remote target so its fetch fails,
	// without touching its working tree: sync never pulls or merges, so
	// nothing about this assertion depends on that.
	if err := os.RemoveAll(broken + "-bare.git"); err != nil {
		t.Fatalf("remove %s: %v", broken+"-bare.git", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"sync"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("sync = 0, want non-zero when a repository's fetch fails")
	}
	if !strings.Contains(stderr.String(), "host/acme/broken") {
		t.Fatalf("sync stderr does not name the failing repository: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "host/acme/ok") {
		t.Fatalf("sync stderr does not report the repository that succeeded: %s", stderr.String())
	}

	out := runGitOutput(t, ok, "log", "-1", "--format=%s", "origin/main")
	if out != "second commit" {
		t.Fatalf("%s: origin/main = %q, want the healthy repository fetched despite the other one failing", ok, out)
	}
}

func TestRunSync_DryRunFetchesNothing(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	repo := filepath.Join(root, "host", "acme", "repo")
	cloneFromBareRemote(t, home, repo)
	advanceBareRemote(t, repo+"-source", repo+"-bare.git")

	var stdout, stderr bytes.Buffer
	code := run([]string{"sync", "--dry-run"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("sync --dry-run = %d, want 0, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "host/acme/repo") {
		t.Fatalf("sync --dry-run stdout = %q, want it to name the repository it would fetch", stdout.String())
	}

	out := runGitOutput(t, repo, "log", "-1", "--format=%s", "origin/main")
	if out == "second commit" {
		t.Fatal("sync --dry-run fetched the remote, want it untouched")
	}
}

// TestRunSync_ExcludesALinkedWorktreeSinceItSharesItsMainCheckoutsRemote
// guards the worktree visibility rule sync settles on: every worktree of
// one repository shares that repository's remote, so fetching one again
// would only repeat the exact same fetch. "--dry-run" naming only the
// main checkout, nested worktree and all, is the directly observable
// proof.
func TestRunSync_ExcludesALinkedWorktreeSinceItSharesItsMainCheckoutsRemote(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	repo := filepath.Join(root, "host", "acme", "repo")
	initRepository(t, repo)
	runGit(t, repo, "remote", "add", "origin", "https://github.com/acme/repo.git")
	worktree := filepath.Join(repo, ".claude", "worktrees", "feat-x")
	runGit(t, repo, "worktree", "add", worktree, "-b", "feat-x")

	var stdout, stderr bytes.Buffer
	code := run([]string{"sync", "--dry-run"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("sync --dry-run = %d, want 0, stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "host/acme/repo\n") {
		t.Fatalf("sync --dry-run stdout = %q, want it to name the main checkout %q", out, "host/acme/repo")
	}
	if strings.Contains(out, filepath.ToSlash(filepath.Join("host", "acme", "repo", ".claude"))) {
		t.Fatalf(
			"sync --dry-run stdout = %q, want no entry naming the linked worktree: it must never be fetched on its own",
			out,
		)
	}
}

func TestRunSync_NoRepositoriesIsNotAFailure(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"sync"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("sync with nothing cloned = %d, want 0, stderr: %s", code, stderr.String())
	}
}

func TestRunSync_RejectsAnArgument(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"sync", "unexpected"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("sync with an argument = %d, want 2", code)
	}
}
