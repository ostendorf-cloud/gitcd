// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package git_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/git"
)

// porcelainFixture is "git worktree list --porcelain" output for a main
// checkout, two linked worktrees, a detached one, a locked one and a prunable
// one, in the shape git writes it.
const porcelainFixture = `worktree /home/me/Code/github.com/acme/widget
HEAD 1111111111111111111111111111111111111111
branch refs/heads/main

worktree /home/me/Code/github.com/acme/widget/.claude/worktrees/feat/20-menu
HEAD 2222222222222222222222222222222222222222
branch refs/heads/feat/20-menu

worktree /home/me/Code/github.com/acme/widget/.claude/worktrees/fix/31-crash
HEAD 3333333333333333333333333333333333333333
branch refs/heads/fix/31-crash
locked pinned by a release run

worktree /home/me/Code/github.com/acme/widget/.claude/worktrees/probe
HEAD 4444444444444444444444444444444444444444
detached

worktree /home/me/Code/github.com/acme/widget/.claude/worktrees/gone
HEAD 5555555555555555555555555555555555555555
branch refs/heads/chore/gone
prunable gitdir file points to non-existent location
`

func TestParseWorktrees(t *testing.T) {
	t.Parallel()

	const base = "/home/me/Code/github.com/acme/widget"
	tests := []struct {
		name  string
		input string
		want  []git.Worktree
	}{
		{name: "empty output", input: "", want: nil},
		{
			name:  "a main checkout, linked, detached, locked and prunable worktrees",
			input: porcelainFixture,
			want: []git.Worktree{
				{Path: base, Head: "1111111111111111111111111111111111111111", Branch: "main"},
				{Path: base + "/.claude/worktrees/feat/20-menu", Head: "2222222222222222222222222222222222222222", Branch: "feat/20-menu"},
				{Path: base + "/.claude/worktrees/fix/31-crash", Head: "3333333333333333333333333333333333333333", Branch: "fix/31-crash", Locked: true},
				{Path: base + "/.claude/worktrees/probe", Head: "4444444444444444444444444444444444444444", Detached: true},
				{Path: base + "/.claude/worktrees/gone", Head: "5555555555555555555555555555555555555555", Branch: "chore/gone", Prunable: true},
			},
		},
		{
			name:  "a bare repository entry",
			input: "worktree /srv/git/widget.git\nbare\n\nworktree /srv/wt/main\nHEAD 1111111111111111111111111111111111111111\nbranch refs/heads/main\n",
			want: []git.Worktree{
				{Path: "/srv/git/widget.git", Bare: true},
				{Path: "/srv/wt/main", Head: "1111111111111111111111111111111111111111", Branch: "main"},
			},
		},
		{
			name:  "a lock without a reason",
			input: "worktree /srv/wt/x\nHEAD 1111111111111111111111111111111111111111\nbranch refs/heads/x\nlocked\n",
			want:  []git.Worktree{{Path: "/srv/wt/x", Head: "1111111111111111111111111111111111111111", Branch: "x", Locked: true}},
		},
		{
			name:  "a label this version does not know is skipped",
			input: "worktree /srv/wt/x\nHEAD 1111111111111111111111111111111111111111\nbranch refs/heads/x\nfuture value\n",
			want:  []git.Worktree{{Path: "/srv/wt/x", Head: "1111111111111111111111111111111111111111", Branch: "x"}},
		},
		{
			name:  "a path with a space and windows line endings",
			input: "worktree /srv/my wt/x\r\nHEAD 1111111111111111111111111111111111111111\r\nbranch refs/heads/x\r\n\r\n",
			want:  []git.Worktree{{Path: "/srv/my wt/x", Head: "1111111111111111111111111111111111111111", Branch: "x"}},
		},
		{
			name:  "no trailing blank line",
			input: "worktree /srv/wt/x\nHEAD 1111111111111111111111111111111111111111\ndetached",
			want:  []git.Worktree{{Path: "/srv/wt/x", Head: "1111111111111111111111111111111111111111", Detached: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := git.ParseWorktrees(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseWorktrees() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestListWorktrees_ReadsARealRepositoryFromAnyOfItsWorktrees(t *testing.T) {
	setupGitEnv(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temporary directory: %v", err)
	}
	main := filepath.Join(root, "widget")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatalf("create %s: %v", main, err)
	}
	initRepository(t, main)
	linked := filepath.Join(root, "widget-feat")
	run(t, main, "worktree", "add", "-b", "feat/x", linked)

	for _, from := range []string{main, linked} {
		got, err := git.ListWorktrees(context.Background(), from)
		if err != nil {
			t.Fatalf("ListWorktrees(%s) returned an unexpected error: %v", from, err)
		}
		if len(got) != 2 {
			t.Fatalf("ListWorktrees(%s) = %+v, want two entries", from, got)
		}
		if got[0].Path != main || got[0].Branch != "main" {
			t.Fatalf("ListWorktrees(%s)[0] = %+v, want the main checkout first", from, got[0])
		}
		if got[1].Path != linked || got[1].Branch != "feat/x" {
			t.Fatalf("ListWorktrees(%s)[1] = %+v, want the linked worktree on feat/x", from, got[1])
		}
	}
}

func TestListWorktrees_FailsOutsideARepository(t *testing.T) {
	setupGitEnv(t)

	if _, err := git.ListWorktrees(context.Background(), t.TempDir()); err == nil {
		t.Fatal("ListWorktrees outside a repository returned no error")
	}
}

func TestCheckBranchName(t *testing.T) {
	setupGitEnv(t)

	tests := []struct {
		name    string
		branch  string
		wantErr bool
	}{
		{name: "a plain name", branch: "main"},
		{name: "a name with a slash", branch: "feat/20-menu"},
		{name: "an empty name", branch: "", wantErr: true},
		{name: "a name with a space", branch: "my branch", wantErr: true},
		{name: "a parent segment", branch: "a/../b", wantErr: true},
		{name: "a leading dash", branch: "-force", wantErr: true},
		{name: "the previous branch shorthand", branch: "@{-1}", wantErr: true},
		{name: "a trailing slash", branch: "feat/", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := git.CheckBranchName(context.Background(), tt.branch)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CheckBranchName(%q) error = %v, wantErr %v", tt.branch, err, tt.wantErr)
			}
		})
	}
}

func TestAddWorktree(t *testing.T) {
	setupGitEnv(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temporary directory: %v", err)
	}
	main := filepath.Join(root, "widget")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatalf("create %s: %v", main, err)
	}
	initRepository(t, main)
	run(t, main, "branch", "existing")

	t.Run("a new branch lands in a nested directory and keeps stdout out of progress", func(t *testing.T) {
		target := filepath.Join(root, "wt", "feat", "20-menu")
		var progress bytes.Buffer
		if err := git.AddWorktree(context.Background(), main, target, "feat/20-menu", true, "", &progress); err != nil {
			t.Fatalf("AddWorktree returned an unexpected error: %v", err)
		}
		if !git.BranchExists(context.Background(), main, "feat/20-menu") {
			t.Fatal("the new branch does not exist after AddWorktree")
		}
		if _, err := os.Stat(filepath.Join(target, "README.md")); err != nil {
			t.Fatalf("the worktree holds no checkout: %v", err)
		}
		if progress.Len() == 0 {
			t.Fatal("AddWorktree wrote git's own progress nowhere")
		}
	})

	t.Run("an existing branch is checked out, not recreated", func(t *testing.T) {
		target := filepath.Join(root, "wt", "existing")
		if err := git.AddWorktree(context.Background(), main, target, "existing", false, "", &bytes.Buffer{}); err != nil {
			t.Fatalf("AddWorktree returned an unexpected error: %v", err)
		}
		got, err := git.ListWorktrees(context.Background(), main)
		if err != nil {
			t.Fatalf("ListWorktrees returned an unexpected error: %v", err)
		}
		found := false
		for _, w := range got {
			if w.Path == target && w.Branch == "existing" {
				found = true
			}
		}
		if !found {
			t.Fatalf("ListWorktrees = %+v, want a worktree at %s on the existing branch", got, target)
		}
	})

	t.Run("a new branch starts at the given base", func(t *testing.T) {
		run(t, main, "branch", "release")
		target := filepath.Join(root, "wt", "from-release")
		if err := git.AddWorktree(context.Background(), main, target, "topic", true, "release", &bytes.Buffer{}); err != nil {
			t.Fatalf("AddWorktree returned an unexpected error: %v", err)
		}
		if !git.BranchExists(context.Background(), main, "topic") {
			t.Fatal("the new branch does not exist after AddWorktree")
		}
	})

	t.Run("an unknown base fails", func(t *testing.T) {
		target := filepath.Join(root, "wt", "bad-base")
		if err := git.AddWorktree(context.Background(), main, target, "bad-base", true, "no-such-ref", &bytes.Buffer{}); err == nil {
			t.Fatal("AddWorktree with an unknown base returned no error")
		}
	})
}

func TestBranchExists(t *testing.T) {
	setupGitEnv(t)
	main := filepath.Join(t.TempDir(), "widget")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatalf("create %s: %v", main, err)
	}
	initRepository(t, main)

	if !git.BranchExists(context.Background(), main, "main") {
		t.Fatal("BranchExists(main) = false, want true")
	}
	if git.BranchExists(context.Background(), main, "nope") {
		t.Fatal("BranchExists(nope) = true, want false")
	}
}
