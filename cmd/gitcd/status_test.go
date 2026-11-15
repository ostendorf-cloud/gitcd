// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pushToBareRemote creates a bare repository next to repo, pushes
// "main" to it and sets it as "main"'s upstream, so a status test can
// exercise an upstream comparison without reaching the network.
func pushToBareRemote(t *testing.T, repo string) {
	t.Helper()
	bare := repo + "-bare.git"
	runGit(t, filepath.Dir(repo), "init", "--bare", bare)
	runGit(t, repo, "remote", "add", "origin", bare)
	runGit(t, repo, "push", "origin", "main")
	runGit(t, repo, "branch", "--set-upstream-to=origin/main", "main")
}

func TestRunStatus_RendersEveryRepositoryState(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	clean := filepath.Join(root, "host", "acme", "clean")
	initRepository(t, clean)
	pushToBareRemote(t, clean)

	dirty := filepath.Join(root, "host", "acme", "dirty")
	initRepository(t, dirty)
	writeFile(t, filepath.Join(dirty, "untracked.txt"), "x\n")

	ahead := filepath.Join(root, "host", "acme", "ahead")
	initRepository(t, ahead)
	pushToBareRemote(t, ahead)
	writeFile(t, filepath.Join(ahead, "more.txt"), "more\n")
	runGit(t, ahead, "add", "more.txt")
	runGit(t, ahead, "commit", "-m", "add more")

	noUpstream := filepath.Join(root, "host", "acme", "noupstream")
	initRepository(t, noUpstream)

	unborn := filepath.Join(root, "host", "acme", "unborn")
	runGit(t, filepath.Dir(unborn), "init", unborn)

	var stdout, stderr bytes.Buffer
	code := run([]string{"status"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("status = %d, want 0 without --strict, stderr: %s", code, stderr.String())
	}
	out := stdout.String()

	for _, want := range []struct{ name, line string }{
		{"clean", "up to date"},
		{"dirty", "dirty"},
		{"ahead", "+1 -0"},
		{"noupstream", "no upstream"},
		{"unborn", "unborn"},
	} {
		if !strings.Contains(out, want.name) {
			t.Fatalf("status output is missing a line for %q: %s", want.name, out)
		}
		if !strings.Contains(out, want.line) {
			t.Fatalf("status output for %q is missing %q: %s", want.name, want.line, out)
		}
	}
}

// TestRunStatus_UnbornHEADRendersAsAStateNotACrash guards the reported
// and re-probed behaviour that a freshly cloned empty repository leaves
// an unborn HEAD, with no "branch.ab" line at all: status must still
// print a row for it, never fail the whole command.
func TestRunStatus_UnbornHEADRendersAsAStateNotACrash(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	bare := filepath.Join(home, "remote-bare.git")
	runGit(t, home, "init", "--bare", bare)
	clone := filepath.Join(root, "host", "acme", "empty")
	if err := os.MkdirAll(filepath.Dir(clone), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(clone), err)
	}
	runGit(t, filepath.Dir(clone), "clone", bare, clone)

	var stdout, stderr bytes.Buffer
	code := run([]string{"status"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("status = %d, want 0, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "unborn") {
		t.Fatalf("status output does not render the unborn HEAD as a state: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "panic") {
		t.Fatalf("status output contains a panic: %s", stdout.String())
	}
}

// TestRunStatus_BranchWithNoUpstreamRendersAsAState guards the second
// reported behaviour: a branch with no upstream at all also omits a
// porcelain line status must not mistake for "up to date".
func TestRunStatus_BranchWithNoUpstreamRendersAsAState(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	repo := filepath.Join(root, "host", "acme", "lonely")
	initRepository(t, repo)

	var stdout, stderr bytes.Buffer
	code := run([]string{"status"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("status = %d, want 0, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "no upstream") {
		t.Fatalf("status output does not render the missing upstream as a state: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "up to date") {
		t.Fatalf("status output claims a branch with no upstream is up to date: %s", stdout.String())
	}
}

func TestRunStatus_JSONPrintsStructuredOutput(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	dirty := filepath.Join(root, "host", "acme", "dirty")
	initRepository(t, dirty)
	writeFile(t, filepath.Join(dirty, "untracked.txt"), "x\n")

	var stdout, stderr bytes.Buffer
	code := run([]string{"status", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("status --json = %d, want 0, stderr: %s", code, stderr.String())
	}

	var got []repoStatus
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("status --json produced invalid JSON: %v: %s", err, stdout.String())
	}
	if len(got) != 1 {
		t.Fatalf("status --json reported %d repositories, want 1", len(got))
	}
	if !got[0].Dirty || !got[0].Untracked {
		t.Fatalf("status --json entry = %+v, want Dirty and Untracked set", got[0])
	}
	if got[0].Name != "host/acme/dirty" {
		t.Fatalf("status --json name = %q, want %q", got[0].Name, "host/acme/dirty")
	}
}

func TestRunStatus_StrictExitsNonZeroWhenAnyRepositoryIsDirty(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	clean := filepath.Join(root, "host", "acme", "clean")
	initRepository(t, clean)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"status", "--strict"}, &stdout, &stderr); code != 0 {
		t.Fatalf("status --strict with only a clean repository = %d, want 0, stderr: %s", code, stderr.String())
	}

	dirty := filepath.Join(root, "host", "acme", "dirty")
	initRepository(t, dirty)
	writeFile(t, filepath.Join(dirty, "untracked.txt"), "x\n")

	stdout.Reset()
	stderr.Reset()
	code := run([]string{"status", "--strict"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("status --strict = 0, want non-zero with a dirty repository present")
	}
}

// TestRunStatus_IncludesALinkedWorktreeAsItsOwnRow guards the worktree
// visibility rule status settles on: a linked worktree carries its own
// branch and its own dirty state, distinct from its main checkout's, so
// it always gets its own row, even nested at the
// "<repo>/.claude/worktrees/<branch>" layout this organisation's own
// worktree convention produces, the one shape "status" could not see
// before this command moved onto internal/scan.
func TestRunStatus_IncludesALinkedWorktreeAsItsOwnRow(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	main := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, main)
	runGit(t, main, "remote", "add", "origin", "https://github.com/acme/widget.git")
	worktree := filepath.Join(main, ".claude", "worktrees", "feat-x")
	runGit(t, main, "worktree", "add", worktree, "-b", "feat-x")

	var stdout, stderr bytes.Buffer
	code := run([]string{"status"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("status = %d, want 0, stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "main") {
		t.Fatalf("status output is missing the main checkout's own row: %s", out)
	}
	if !strings.Contains(out, "feat-x") {
		t.Fatalf("status output is missing a row for the linked worktree's own branch %q: %s", "feat-x", out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("status output has %d lines, want 3 (the header, the main checkout and the worktree): %s", len(lines), out)
	}
}

// TestRunStatus_OrdersRowsByNameNotByWalkOrder guards the Should finding
// on pull request #23: the deleted discoverRepos sorted its own results,
// and status now relies on scanRepositories to do the same rather than
// on scan.Walk's own order, which is the order git reports a repository's
// worktrees in, not necessarily that repository's name among its
// siblings. "widget"'s own worktree is registered under "other/standalone"
// here, not nested under "widget" itself, exactly the case the review
// found nothing in the code guaranteed: scan.Walk reports it the moment
// "acme/widget" is found, before it ever reaches "acme2/middle", so
// without an explicit sort "other/standalone" would print first even
// though "acme2/middle" sorts before it by name.
func TestRunStatus_OrdersRowsByNameNotByWalkOrder(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	widget := filepath.Join(root, "acme", "widget")
	initRepository(t, widget)
	runGit(t, widget, "remote", "add", "origin", "https://github.com/acme/widget.git")

	middle := filepath.Join(root, "acme2", "middle")
	initRepository(t, middle)
	runGit(t, middle, "remote", "add", "origin", "https://github.com/acme2/middle.git")

	standalone := filepath.Join(root, "other", "standalone")
	runGit(t, widget, "worktree", "add", standalone, "-b", "feat-y")

	var stdout, stderr bytes.Buffer
	code := run([]string{"status"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("status = %d, want 0, stderr: %s", code, stderr.String())
	}

	out := stdout.String()
	middleIdx := strings.Index(out, "acme2/middle")
	standaloneIdx := strings.Index(out, "other/standalone")
	if middleIdx == -1 {
		t.Fatalf("status output is missing a row for %q: %s", "acme2/middle", out)
	}
	if standaloneIdx == -1 {
		t.Fatalf("status output is missing a row for %q: %s", "other/standalone", out)
	}
	if middleIdx > standaloneIdx {
		t.Fatalf("status printed %q before %q, want name order (%q before %q): %s",
			"other/standalone", "acme2/middle", "acme2/middle", "other/standalone", out)
	}
}

func TestRunStatus_NoRepositoriesIsNotAFailure(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"status"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("status with nothing cloned = %d, want 0, stderr: %s", code, stderr.String())
	}
}

func TestRunStatus_RejectsAnArgument(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"status", "unexpected"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("status with an argument = %d, want 2", code)
	}
}
