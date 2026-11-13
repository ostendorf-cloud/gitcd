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

func TestRunClone_DryRunPrintsThePathWithoutCloning(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	var stdout, stderr bytes.Buffer
	code := run([]string{"clone", "--dry-run", "https://example.test/acme/widget.git"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("clone --dry-run = %d, want 0, stderr: %s", code, stderr.String())
	}
	want := filepath.Join(root, "example.test", "acme", "widget")
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Fatalf("clone --dry-run stdout = %q, want %q", got, want)
	}
	if _, err := os.Stat(want); err == nil {
		t.Fatalf("clone --dry-run created %s, want it untouched", want)
	}
}

func TestRunClone_ClonesAFreshTargetThroughARewrittenOfflineURL(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	source := filepath.Join(home, "upstream")
	initRepository(t, source)

	input := "https://example.test/acme/widget.git"
	// The URL rewrite git itself applies before ever touching the network,
	// pointed at the local source instead: this clone never leaves the
	// machine.
	runGit(t, home, "config", "--global", "url.file://"+source+".insteadOf", input)

	var stdout, stderr bytes.Buffer
	code := run([]string{"clone", input}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("clone %q = %d, want 0, stderr: %s", input, code, stderr.String())
	}
	want := filepath.Join(root, "example.test", "acme", "widget")
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Fatalf("clone %q stdout = %q, want %q", input, got, want)
	}
	if _, err := os.Stat(filepath.Join(want, "README.md")); err != nil {
		t.Fatalf("cloned repository is missing its content: %v", err)
	}

	// A second clone of the same, rewritten input must still be idempotent:
	// gitcd must compare against the remote exactly as git stored it, not
	// against the rewritten target the insteadOf rule actually fetched
	// from, which "git remote get-url" would report instead.
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"clone", input}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("second clone %q = %d, want 0 for an idempotent clone, stderr: %s", input, code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Fatalf("second clone %q stdout = %q, want %q", input, got, want)
	}
}

func TestRunClone_ExistingTargetWithTheSameRemoteIsIdempotent(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	input := "https://example.test/acme/widget.git"
	dest := filepath.Join(root, "example.test", "acme", "widget")
	initRepository(t, dest)
	runGit(t, dest, "remote", "add", "origin", input)

	var stdout, stderr bytes.Buffer
	code := run([]string{"clone", input}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("clone %q = %d, want 0 for an idempotent clone, stderr: %s", input, code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != dest {
		t.Fatalf("clone %q stdout = %q, want %q", input, got, dest)
	}
}

func TestRunClone_ExistingTargetWithADifferentRemoteFailsNamingBoth(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	input := "https://example.test/acme/widget.git"
	otherRemote := "https://example.test/acme/other.git"
	dest := filepath.Join(root, "example.test", "acme", "widget")
	initRepository(t, dest)
	runGit(t, dest, "remote", "add", "origin", otherRemote)

	var stdout, stderr bytes.Buffer
	code := run([]string{"clone", input}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("clone %q = 0, want a failure for a conflicting remote", input)
	}
	if !strings.Contains(stderr.String(), input) || !strings.Contains(stderr.String(), otherRemote) {
		t.Fatalf("clone %q stderr = %q, want it to name both remotes", input, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("clone %q wrote to stdout on failure: %q", input, stdout.String())
	}
}

func TestRunClone_InvalidRemoteFails(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"clone", "not-a-remote"}, &stdout, &stderr)

	if code == 0 {
		t.Fatal("clone with an invalid remote = 0, want a failure")
	}
	if stdout.Len() != 0 {
		t.Fatalf("clone with an invalid remote wrote to stdout: %q", stdout.String())
	}
}

func TestRunClone_RequiresExactlyOneArgument(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"clone"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("clone with no argument = %d, want 2", code)
	}
}
