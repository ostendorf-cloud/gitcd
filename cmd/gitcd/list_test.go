// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunList_PrintsEveryRepositoryUnderRoot(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	first := filepath.Join(root, "github.com", "acme", "widget")
	second := filepath.Join(root, "gitlab.com", "acme", "other")
	initRepository(t, first)
	addRemote(t, first, "https://github.com/acme/widget.git")
	initRepository(t, second)
	addRemote(t, second, "https://gitlab.com/acme/other.git")

	var stdout, stderr bytes.Buffer
	code := run([]string{"list"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("list = %d, want 0, stderr: %s", code, stderr.String())
	}
	lines := strings.Fields(strings.TrimSpace(stdout.String()))
	if len(lines) != 2 || lines[0] != first || lines[1] != second {
		t.Fatalf("list stdout = %v, want %v", lines, []string{first, second})
	}
}

func TestRunList_HidesALinkedWorktreeByDefault(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	main := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, main)
	addRemote(t, main, "https://github.com/acme/widget.git")
	worktree := filepath.Join(root, "github.com", "acme", "widget-feature")
	runGit(t, main, "worktree", "add", worktree)

	var stdout, stderr bytes.Buffer
	code := run([]string{"list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("list = %d, want 0, stderr: %s", code, stderr.String())
	}
	got := strings.TrimSpace(stdout.String())
	if got != main {
		t.Fatalf("list stdout = %q, want only %q, a linked worktree must be hidden by default", got, main)
	}
}

func TestRunList_WorktreesFlagIncludesALinkedWorktree(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	main := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, main)
	addRemote(t, main, "https://github.com/acme/widget.git")
	worktree := filepath.Join(root, "github.com", "acme", "widget-feature")
	runGit(t, main, "worktree", "add", worktree)

	var stdout, stderr bytes.Buffer
	code := run([]string{"list", "--worktrees"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("list --worktrees = %d, want 0, stderr: %s", code, stderr.String())
	}
	lines := strings.Fields(strings.TrimSpace(stdout.String()))
	if len(lines) != 2 || lines[0] != main || lines[1] != worktree {
		t.Fatalf("list --worktrees stdout = %v, want %v", lines, []string{main, worktree})
	}
}

func TestRunList_JSONReportsTheSameRepositoriesStructured(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	main := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, main)
	addRemote(t, main, "https://github.com/acme/widget.git")
	worktree := filepath.Join(root, "github.com", "acme", "widget-feature")
	runGit(t, main, "worktree", "add", worktree)

	var stdout, stderr bytes.Buffer
	code := run([]string{"list", "--worktrees", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("list --worktrees --json = %d, want 0, stderr: %s", code, stderr.String())
	}

	var entries []listEntry
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		t.Fatalf("list --worktrees --json did not print valid JSON: %v: %s", err, stdout.String())
	}
	if len(entries) != 2 {
		t.Fatalf("list --worktrees --json = %d entries, want 2", len(entries))
	}
	byPath := map[string]listEntry{}
	for _, e := range entries {
		byPath[e.Path] = e
	}
	if e, ok := byPath[main]; !ok || e.Worktree {
		t.Fatalf("list --worktrees --json entry for %s = %+v, want worktree=false", main, e)
	}
	if e, ok := byPath[worktree]; !ok || !e.Worktree {
		t.Fatalf("list --worktrees --json entry for %s = %+v, want worktree=true", worktree, e)
	}
}

func TestRunList_SkipsARepositoryWithNoRemoteRatherThanFailing(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	withRemote := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, withRemote)
	addRemote(t, withRemote, "https://github.com/acme/widget.git")

	noRemote := filepath.Join(root, "local-only")
	initRepository(t, noRemote)

	var stdout, stderr bytes.Buffer
	code := run([]string{"list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("list = %d, want 0, stderr: %s", code, stderr.String())
	}
	got := strings.TrimSpace(stdout.String())
	if got != withRemote {
		t.Fatalf("list stdout = %q, want only %q", got, withRemote)
	}
}

func TestRunList_NoArgumentsAccepted(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"list", "extra"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("list extra = %d, want 2", code)
	}
}
