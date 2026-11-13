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

func TestRunAdopt_MovesWithConsentAndPrintsBothPaths(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	oldPath := filepath.Join(home, "elsewhere", "widget")
	initRepository(t, oldPath)
	runGit(t, oldPath, "remote", "add", "origin", "https://github.com/acme/widget.git")

	var stdout, stderr bytes.Buffer
	code := run([]string{"adopt", "--yes", oldPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("adopt --yes = %d, want 0, stderr: %s", code, stderr.String())
	}

	wantNewPath := filepath.Join(root, "github.com", "acme", "widget")
	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("adopt stdout = %q, want exactly two lines, the old and the new path", stdout.String())
	}
	if lines[0] != oldPath {
		t.Fatalf("adopt stdout first line = %q, want the old path %q", lines[0], oldPath)
	}
	if lines[1] != wantNewPath {
		t.Fatalf("adopt stdout second line = %q, want the new path %q", lines[1], wantNewPath)
	}

	if _, err := os.Stat(wantNewPath); err != nil {
		t.Fatalf("adopt did not move the repository to %s: %v", wantNewPath, err)
	}
	if _, err := os.Stat(oldPath); err == nil {
		t.Fatalf("adopt left the old path %s behind", oldPath)
	}
}

func TestRunAdopt_WithoutConsentNeverMoves(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	oldPath := filepath.Join(home, "elsewhere", "widget")
	initRepository(t, oldPath)
	runGit(t, oldPath, "remote", "add", "origin", "https://github.com/acme/widget.git")

	var stdout, stderr bytes.Buffer
	// No "--yes", and stdin is empty: the prompt reaches EOF immediately
	// and must decline, the safe default.
	code := run([]string{"adopt", oldPath}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("adopt with no consent and empty stdin = 0, want a non-zero exit")
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("adopt moved the repository despite no consent: %v", err)
	}
	wantNewPath := filepath.Join(root, "github.com", "acme", "widget")
	if _, err := os.Stat(wantNewPath); err == nil {
		t.Fatalf("adopt moved the repository to %s despite no consent", wantNewPath)
	}
}

func TestRunAdopt_DryRunNeverMoves(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	oldPath := filepath.Join(home, "elsewhere", "widget")
	initRepository(t, oldPath)
	runGit(t, oldPath, "remote", "add", "origin", "https://github.com/acme/widget.git")

	var stdout, stderr bytes.Buffer
	code := run([]string{"adopt", "--dry-run", oldPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("adopt --dry-run = %d, want 0, stderr: %s", code, stderr.String())
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("adopt --dry-run moved the repository: %v", err)
	}
	wantNewPath := filepath.Join(root, "github.com", "acme", "widget")
	if _, err := os.Stat(wantNewPath); err == nil {
		t.Fatalf("adopt --dry-run wrote to %s", wantNewPath)
	}
	if !strings.Contains(stdout.String(), oldPath) || !strings.Contains(stdout.String(), wantNewPath) {
		t.Fatalf("adopt --dry-run stdout = %q, want both the old and the new path", stdout.String())
	}
}

func TestRunAdopt_RefusesWorktreeConfig(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	oldPath := filepath.Join(home, "elsewhere", "widget")
	initRepository(t, oldPath)
	runGit(t, oldPath, "remote", "add", "origin", "https://github.com/acme/widget.git")
	runGit(t, oldPath, "config", "--local", "extensions.worktreeConfig", "true")

	var stdout, stderr bytes.Buffer
	code := run([]string{"adopt", "--yes", oldPath}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("adopt on a repository with extensions.worktreeConfig = 0, want a refusal")
	}
	if !strings.Contains(stderr.String(), "worktreeConfig") {
		t.Fatalf("adopt stderr = %q, want it to name extensions.worktreeConfig", stderr.String())
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("adopt moved the repository despite refusing: %v", err)
	}
}

func TestRunAdopt_RefusesAnExistingTargetWithADifferentRemote(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	newPath := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, newPath)
	runGit(t, newPath, "remote", "add", "origin", "https://github.com/acme/other.git")

	oldPath := filepath.Join(home, "elsewhere", "widget")
	initRepository(t, oldPath)
	runGit(t, oldPath, "remote", "add", "origin", "https://github.com/acme/widget.git")

	var stdout, stderr bytes.Buffer
	code := run([]string{"adopt", "--yes", oldPath}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("adopt onto an occupied target = 0, want a refusal")
	}
	// The specific, friendly wording adoptTargetConflict produces, not
	// merely a path mentioned somewhere: a bare directory rename onto an
	// already occupied target fails at the OS level regardless, with its
	// own message naming both paths, so that alone would not prove this
	// refusal runs before ever attempting the move.
	if !strings.Contains(stderr.String(), "already exists with a different remote") {
		t.Fatalf("adopt stderr = %q, want the specific conflict message naming %s", stderr.String(), newPath)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("adopt moved the repository despite refusing: %v", err)
	}
}

func TestRunAdopt_UpdatesAStalePinToTheCurrentlyExpectedProfile(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
root: `+root+`
profiles:
  work:
    user.email: acme-work-identity
  personal:
    user.email: acme-personal-identity
rules:
  - host: github.com
    owner: acme
    profile: work
`)

	oldPath := filepath.Join(home, "elsewhere", "widget")
	initRepository(t, oldPath)
	runGit(t, oldPath, "remote", "add", "origin", "https://github.com/acme/widget.git")
	runGit(t, oldPath, "config", "--local", "user.email", "acme-personal-identity")
	runGit(t, oldPath, "config", "--local", "gitcd.profile", "personal")

	var stdout, stderr bytes.Buffer
	code := run([]string{"adopt", "--yes", oldPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("adopt --yes = %d, want 0, stderr: %s", code, stderr.String())
	}

	newPath := filepath.Join(root, "github.com", "acme", "widget")
	got := runGitOutput(t, newPath, "config", "--local", "--get", "gitcd.profile")
	if got != "work" {
		t.Fatalf("gitcd.profile after adopt = %q, want it updated to %q, the profile the rules now imply", got, "work")
	}
	gotEmail := runGitOutput(t, newPath, "config", "--local", "--get", "user.email")
	if gotEmail != "acme-work-identity" {
		t.Fatalf("user.email after adopt = %q, want %q", gotEmail, "acme-work-identity")
	}
}

func TestRunAdopt_ExactlyOnePathIsRequired(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"adopt"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("adopt with no path = %d, want 2", code)
	}
}
