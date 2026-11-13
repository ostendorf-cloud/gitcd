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
