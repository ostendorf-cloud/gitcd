// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/config"
)

func TestRunOpen_PrintResolvesAnExactRepositoryName(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	dest := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, dest)
	runGit(t, dest, "remote", "add", "origin", "https://github.com/acme/widget.git")

	var stdout, stderr bytes.Buffer
	code := run([]string{"open", "--print", "widget"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("open --print widget = %d, want 0, stderr: %s", code, stderr.String())
	}
	want := "https://github.com/acme/widget"
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Fatalf("open --print widget stdout = %q, want %q", got, want)
	}
}

func TestRunOpen_PrintResolvesAHostOwnerRepoShorthandAlreadyCloned(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	dest := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, dest)
	runGit(t, dest, "remote", "add", "origin", "https://github.com/acme/widget.git")

	var stdout, stderr bytes.Buffer
	code := run([]string{"open", "--print", "github.com/acme/widget"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("open --print github.com/acme/widget = %d, want 0, stderr: %s", code, stderr.String())
	}
	want := "https://github.com/acme/widget"
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Fatalf("open --print stdout = %q, want %q", got, want)
	}
}

// TestRunOpen_PrintResolvesALinkedWorktreeNestedInsideItsOwnRepository
// guards the worktree visibility rule "open" settles on: a linked
// worktree, even nested at "<repo>/.claude/worktrees/<branch>", is as
// valid a destination as its main checkout, resolved by name exactly as
// "cd" resolves one. Its own remote is shared with its main checkout's,
// read from the common git configuration regardless of which checkout
// "git" is asked from.
func TestRunOpen_PrintResolvesALinkedWorktreeNestedInsideItsOwnRepository(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	main := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, main)
	runGit(t, main, "remote", "add", "origin", "https://github.com/acme/widget.git")
	worktree := filepath.Join(main, ".claude", "worktrees", "feat-x")
	runGit(t, main, "worktree", "add", worktree, "-b", "feat-x")

	var stdout, stderr bytes.Buffer
	code := run([]string{"open", "--print", "feat-x"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("open --print feat-x = %d, want 0, stderr: %s", code, stderr.String())
	}
	want := "https://github.com/acme/widget"
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Fatalf("open --print feat-x stdout = %q, want %q", got, want)
	}
}

func TestRunOpen_PrintDefaultsToTheCurrentDirectory(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	dest := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, dest)
	runGit(t, dest, "remote", "add", "origin", "git@github.com:acme/widget.git")
	t.Chdir(dest)

	var stdout, stderr bytes.Buffer
	code := run([]string{"open", "--print"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("open --print = %d, want 0, stderr: %s", code, stderr.String())
	}
	want := "https://github.com/acme/widget"
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Fatalf("open --print stdout = %q, want %q", got, want)
	}
}

// TestRunOpen_PrintFallsBackToAGenericURLForAnUnknownHostShape guards the
// decision that a remote whose host carries no special case still gets a
// plausible web URL, rather than an error: the generic owner/repo shape
// most forges share.
func TestRunOpen_PrintFallsBackToAGenericURLForAnUnknownHostShape(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	dest := filepath.Join(root, "git.example.test", "acme", "widget")
	initRepository(t, dest)
	runGit(t, dest, "remote", "add", "origin", "https://git.example.test/acme/widget.git")

	var stdout, stderr bytes.Buffer
	code := run([]string{"open", "--print", "git.example.test/acme/widget"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("open --print = %d, want 0, stderr: %s", code, stderr.String())
	}
	want := "https://git.example.test/acme/widget"
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Fatalf("open --print stdout = %q, want %q", got, want)
	}
}

// TestRunOpen_PrintResolvesEveryForgeShapeFromTheCommand covers the forge
// shapes end to end rather than only through the remote package: Azure
// DevOps keeps its "_git" segment, while GitLab takes the generic owner
// and repository path even when the owner carries a subgroup.
func TestRunOpen_PrintResolvesEveryForgeShapeFromTheCommand(t *testing.T) {
	tests := []struct {
		name   string
		path   []string
		remote string
		query  string
		want   string
	}{
		{
			name:   "gitlab subgroup keeps every owner segment",
			path:   []string{"gitlab.com", "acme", "team", "widget"},
			remote: "https://gitlab.com/acme/team/widget.git",
			query:  "gitlab.com/acme/team/widget",
			want:   "https://gitlab.com/acme/team/widget",
		},
		{
			name:   "azure devops keeps its _git segment",
			path:   []string{"dev.azure.com", "acme", "Widget", "widget"},
			remote: "https://dev.azure.com/acme/Widget/_git/widget",
			query:  "widget",
			want:   "https://dev.azure.com/acme/Widget/_git/widget",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := setupEnv(t)
			dest := filepath.Join(append([]string{home, "Code"}, tt.path...)...)
			initRepository(t, dest)
			runGit(t, dest, "remote", "add", "origin", tt.remote)

			var stdout, stderr bytes.Buffer
			if code := run([]string{"open", "--print", tt.query}, &stdout, &stderr); code != 0 {
				t.Fatalf("open --print = %d, want 0, stderr: %s", code, stderr.String())
			}
			if got := strings.TrimSpace(stdout.String()); got != tt.want {
				t.Fatalf("open --print stdout = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunOpen_NoMatchFails(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"open", "--print", "nowhere"}, &stdout, &stderr)

	if code == 0 {
		t.Fatal("open --print nowhere = 0, want a failure for a name that matches nothing")
	}
	if stdout.Len() != 0 {
		t.Fatalf("open --print nowhere wrote to stdout: %q", stdout.String())
	}
}

func TestRunOpen_RejectsMoreThanOneArgument(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"open", "one", "two"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("open with two arguments = %d, want 2", code)
	}
}

// TestOpenRepo_NeverLaunchesARealBrowser exercises the non "--print" path
// through a fake launcher instead of the real one: a test must never
// actually open a browser, and openRepo's launch parameter is exactly
// what lets this test prove that without touching launchBrowser at all.
func TestOpenRepo_NeverLaunchesARealBrowser(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	dest := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, dest)
	runGit(t, dest, "remote", "add", "origin", "https://github.com/acme/widget.git")

	var launched string
	fakeLaunch := func(_ context.Context, url string) error {
		launched = url
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() returned an unexpected error: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := openRepo(context.Background(), &stdout, &stderr, cfg, dest, false, fakeLaunch)

	if code != 0 {
		t.Fatalf("openRepo() = %d, want 0, stderr: %s", code, stderr.String())
	}
	if launched != "https://github.com/acme/widget" {
		t.Fatalf("launch was called with %q, want %q", launched, "https://github.com/acme/widget")
	}
	if stdout.Len() != 0 {
		t.Fatalf("openRepo() wrote to stdout: %q, want everything on stderr", stdout.String())
	}
}

func TestOpenRepo_ReportsALaunchFailure(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	dest := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, dest)
	runGit(t, dest, "remote", "add", "origin", "https://github.com/acme/widget.git")

	boom := errors.New("no display available")
	fakeLaunch := func(_ context.Context, _ string) error { return boom }

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() returned an unexpected error: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := openRepo(context.Background(), &stdout, &stderr, cfg, dest, false, fakeLaunch)

	if code == 0 {
		t.Fatal("openRepo() = 0, want non-zero when the launcher fails")
	}
	if !strings.Contains(stderr.String(), "no display available") {
		t.Fatalf("openRepo() stderr = %q, want it to name the launcher's error", stderr.String())
	}
}
