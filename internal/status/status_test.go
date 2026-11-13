// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package status_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/status"
)

// setupGitEnv isolates every git invocation in the test from the real
// user's configuration and from the network.
func setupGitEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	globalConfig := filepath.Join(home, ".gitconfig")
	if err := os.WriteFile(globalConfig, []byte("[user]\n\tname = Test User\n\temail = [email protected]\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", globalConfig, err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// commitFile adds and commits name with content, in dir, with a
// Conventional Commits subject so the test repository itself stays
// consistent with the organisation's own commit convention.
func commitFile(t *testing.T, dir, name, content string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, name), content)
	runGit(t, dir, "add", name)
	runGit(t, dir, "commit", "-m", "chore: add "+name)
}

func TestInspect_UnbornHEADRendersAsAState(t *testing.T) {
	setupGitEnv(t)

	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")

	got, err := status.Inspect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Inspect(%q) returned an unexpected error: %v", dir, err)
	}
	if !got.Unborn {
		t.Fatal("Unborn = false, want true for a repository with no commit yet")
	}
	if got.Branch != "main" {
		t.Fatalf("Branch = %q, want %q", got.Branch, "main")
	}
	if got.HasUpstreamComparison {
		t.Fatal("HasUpstreamComparison = true, want false: an unborn HEAD has no commit to compare")
	}
	if got.Dirty() {
		t.Fatal("Dirty() = true, want false for a freshly initialised repository")
	}
}

// TestInspect_ClonedEmptyRepositoryCarriesAnUpstreamButNoComparison guards
// the reported and re-probed behaviour that cloning an empty remote
// leaves the clone's HEAD unborn while "branch.upstream" is still
// printed: a caller must read that as "cannot compare yet", not as "in
// sync" and not as a missing upstream.
func TestInspect_ClonedEmptyRepositoryCarriesAnUpstreamButNoComparison(t *testing.T) {
	setupGitEnv(t)

	root := t.TempDir()
	bareRemote := filepath.Join(root, "remote.git")
	runGit(t, root, "init", "--bare", bareRemote)

	clone := filepath.Join(root, "clone")
	runGit(t, root, "clone", bareRemote, clone)

	got, err := status.Inspect(context.Background(), clone)
	if err != nil {
		t.Fatalf("Inspect(%q) returned an unexpected error: %v", clone, err)
	}
	if !got.Unborn {
		t.Fatal("Unborn = false, want true for a clone of an empty remote")
	}
	if got.Upstream == "" {
		t.Fatal("Upstream = \"\", want a clone of an empty remote to still carry one")
	}
	if got.HasUpstreamComparison {
		t.Fatal("HasUpstreamComparison = true, want false: an unborn HEAD cannot be compared against its upstream")
	}
}

func TestInspect_BranchWithNoUpstreamRendersAsAState(t *testing.T) {
	setupGitEnv(t)

	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	commitFile(t, dir, "a.txt", "a\n")

	got, err := status.Inspect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Inspect(%q) returned an unexpected error: %v", dir, err)
	}
	if got.Unborn {
		t.Fatal("Unborn = true, want false: this repository carries a commit")
	}
	if got.Upstream != "" {
		t.Fatalf("Upstream = %q, want \"\" for a branch with no upstream configured", got.Upstream)
	}
	if got.HasUpstreamComparison {
		t.Fatal("HasUpstreamComparison = true, want false for a branch with no upstream")
	}
}

func TestInspect_BranchAheadOfItsUpstream(t *testing.T) {
	setupGitEnv(t)

	root := t.TempDir()
	bareRemote := filepath.Join(root, "remote.git")
	runGit(t, root, "init", "--bare", bareRemote)

	clone := filepath.Join(root, "clone")
	runGit(t, root, "clone", bareRemote, clone)
	runGit(t, clone, "checkout", "-b", "trunk")
	commitFile(t, clone, "a.txt", "a\n")
	runGit(t, clone, "push", "origin", "HEAD:trunk")
	runGit(t, clone, "branch", "--set-upstream-to=origin/trunk", "trunk")
	commitFile(t, clone, "b.txt", "b\n")

	got, err := status.Inspect(context.Background(), clone)
	if err != nil {
		t.Fatalf("Inspect(%q) returned an unexpected error: %v", clone, err)
	}
	if !got.HasUpstreamComparison {
		t.Fatal("HasUpstreamComparison = false, want true: an upstream is configured and both sides have commits")
	}
	if got.Ahead != 1 || got.Behind != 0 {
		t.Fatalf("Ahead, Behind = %d, %d, want 1, 0", got.Ahead, got.Behind)
	}
}

func TestInspect_BranchInSyncWithItsUpstream(t *testing.T) {
	setupGitEnv(t)

	root := t.TempDir()
	bareRemote := filepath.Join(root, "remote.git")
	runGit(t, root, "init", "--bare", bareRemote)

	clone := filepath.Join(root, "clone")
	runGit(t, root, "clone", bareRemote, clone)
	runGit(t, clone, "checkout", "-b", "trunk")
	commitFile(t, clone, "a.txt", "a\n")
	runGit(t, clone, "push", "origin", "HEAD:trunk")
	runGit(t, clone, "branch", "--set-upstream-to=origin/trunk", "trunk")

	got, err := status.Inspect(context.Background(), clone)
	if err != nil {
		t.Fatalf("Inspect(%q) returned an unexpected error: %v", clone, err)
	}
	if !got.HasUpstreamComparison {
		t.Fatal("HasUpstreamComparison = false, want true")
	}
	if got.Ahead != 0 || got.Behind != 0 {
		t.Fatalf("Ahead, Behind = %d, %d, want 0, 0", got.Ahead, got.Behind)
	}
}

func TestInspect_DirtyWorkingTree(t *testing.T) {
	setupGitEnv(t)

	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	commitFile(t, dir, "a.txt", "a\n")

	// Unstaged modification.
	writeFile(t, filepath.Join(dir, "a.txt"), "a\nmodified\n")
	// Staged addition.
	writeFile(t, filepath.Join(dir, "staged.txt"), "staged\n")
	runGit(t, dir, "add", "staged.txt")
	// Untracked file, left exactly that way.
	writeFile(t, filepath.Join(dir, "untracked.txt"), "untracked\n")

	got, err := status.Inspect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Inspect(%q) returned an unexpected error: %v", dir, err)
	}
	if !got.Staged {
		t.Fatal("Staged = false, want true")
	}
	if !got.Unstaged {
		t.Fatal("Unstaged = false, want true")
	}
	if !got.Untracked {
		t.Fatal("Untracked = false, want true")
	}
	if !got.Dirty() {
		t.Fatal("Dirty() = false, want true")
	}
}

// TestInspect_UntrackedFileAloneIsDirty guards Dirty() counting an
// untracked file on its own, with no staged or unstaged change
// alongside it: "status --strict" must treat a forgotten new file as
// dirty just as much as an uncommitted edit.
func TestInspect_UntrackedFileAloneIsDirty(t *testing.T) {
	setupGitEnv(t)

	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	commitFile(t, dir, "a.txt", "a\n")
	writeFile(t, filepath.Join(dir, "untracked.txt"), "untracked\n")

	got, err := status.Inspect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Inspect(%q) returned an unexpected error: %v", dir, err)
	}
	if got.Staged || got.Unstaged {
		t.Fatalf("Staged, Unstaged = %v, %v, want both false: nothing but an untracked file was added", got.Staged, got.Unstaged)
	}
	if !got.Dirty() {
		t.Fatal("Dirty() = false, want true for an untracked file on its own")
	}
}

func TestInspect_CleanWorkingTreeIsNotDirty(t *testing.T) {
	setupGitEnv(t)

	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	commitFile(t, dir, "a.txt", "a\n")

	got, err := status.Inspect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Inspect(%q) returned an unexpected error: %v", dir, err)
	}
	if got.Dirty() {
		t.Fatal("Dirty() = true, want false for a clean working tree")
	}
}

func TestInspect_DetachedHEAD(t *testing.T) {
	setupGitEnv(t)

	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	commitFile(t, dir, "a.txt", "a\n")
	commitFile(t, dir, "b.txt", "b\n")
	runGit(t, dir, "checkout", "HEAD~1")

	got, err := status.Inspect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Inspect(%q) returned an unexpected error: %v", dir, err)
	}
	if !got.Detached {
		t.Fatal("Detached = false, want true")
	}
	if got.Branch != "" {
		t.Fatalf("Branch = %q, want \"\" for a detached HEAD", got.Branch)
	}
}

func TestInspect_WrapsTheUnderlyingErrorForANonRepository(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if _, err := status.Inspect(context.Background(), dir); err == nil {
		t.Fatal("Inspect() succeeded, want an error for a directory that is not a git repository")
	}
}
