// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/config"
)

func TestLoad_WorktreeDir(t *testing.T) {
	t.Run("unset by default", func(t *testing.T) {
		withHome(t)

		got, err := config.Load()
		if err != nil {
			t.Fatalf("Load() returned an unexpected error: %v", err)
		}
		if got.WorktreeDir != "" {
			t.Fatalf("Load().WorktreeDir = %q, want it unset", got.WorktreeDir)
		}
	})

	t.Run("a relative directory is kept as written", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), "worktreeDir: .claude/worktrees\n")

		got, err := config.Load()
		if err != nil {
			t.Fatalf("Load() returned an unexpected error: %v", err)
		}
		if got.WorktreeDir != ".claude/worktrees" {
			t.Fatalf("Load().WorktreeDir = %q, want %q", got.WorktreeDir, ".claude/worktrees")
		}
	})

	t.Run("a leading tilde expands to the home directory", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), "worktreeDir: ~/Worktrees/{repo}\n")

		got, err := config.Load()
		if err != nil {
			t.Fatalf("Load() returned an unexpected error: %v", err)
		}
		if want := filepath.Join(home, "Worktrees", "{repo}"); got.WorktreeDir != want {
			t.Fatalf("Load().WorktreeDir = %q, want %q", got.WorktreeDir, want)
		}
	})

	for _, tt := range []struct {
		name string
		yaml string
	}{
		{name: "a parent segment", yaml: "worktreeDir: ../worktrees\n"},
		{name: "a parent segment in the middle", yaml: "worktreeDir: .claude/../../x\n"},
		{name: "an unknown placeholder", yaml: "worktreeDir: ~/wt/{owner}\n"},
	} {
		t.Run("refuses "+tt.name, func(t *testing.T) {
			home := withHome(t)
			writeFile(t, filepath.Join(home, ".gitcd.yaml"), tt.yaml)

			_, err := config.Load()
			if !errors.Is(err, config.ErrInvalidWorktreeDir) {
				t.Fatalf("Load() error = %v, want ErrInvalidWorktreeDir", err)
			}
		})
	}
}

func TestValidateWorktreeDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template string
		wantErr  bool
	}{
		{name: "empty means unset", template: ""},
		{name: "a hidden relative directory", template: ".claude/worktrees"},
		{name: "a second relative directory", template: ".worktrees"},
		{name: "an absolute directory with the repo placeholder", template: "/srv/worktrees/{repo}"},
		{name: "a dot dot at the start", template: "../worktrees", wantErr: true},
		{name: "a dot dot in the middle", template: "a/../b", wantErr: true},
		{name: "a dot dot behind a backslash", template: `a\..\b`, wantErr: true},
		{name: "a dot dot in an absolute path", template: "/srv/../etc", wantErr: true},
		{name: "a dot dot alone", template: "..", wantErr: true},
		{name: "a name that only starts with dots", template: "..worktrees"},
		{name: "an unknown placeholder", template: "wt/{branch}", wantErr: true},
		{name: "an unmatched closing brace", template: "wt/}", wantErr: true},
		{name: "a line break", template: "wt\nx", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := config.ValidateWorktreeDir(tt.template)
			if tt.wantErr {
				if !errors.Is(err, config.ErrInvalidWorktreeDir) {
					t.Fatalf("ValidateWorktreeDir(%q) error = %v, want ErrInvalidWorktreeDir", tt.template, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateWorktreeDir(%q) returned an unexpected error: %v", tt.template, err)
			}
		})
	}
}

func TestResolveWorktreeDir(t *testing.T) {
	t.Parallel()

	abs := func(p string) string {
		if runtime.GOOS == "windows" {
			return "C:" + filepath.FromSlash(p)
		}
		return p
	}
	repo := abs("/home/me/Code/github.com/acme/widget")

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{name: "unset", template: "", want: ""},
		{name: "relative joins onto the main checkout", template: ".claude/worktrees", want: filepath.Join(repo, ".claude", "worktrees")},
		{name: "absolute is used as it stands", template: abs("/srv/worktrees"), want: filepath.Clean(abs("/srv/worktrees"))},
		{name: "the repo placeholder is the checkout's name", template: abs("/srv/worktrees/{repo}"), want: filepath.Clean(abs("/srv/worktrees/widget"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := config.ResolveWorktreeDir(tt.template, repo)
			if err != nil {
				t.Fatalf("ResolveWorktreeDir(%q) returned an unexpected error: %v", tt.template, err)
			}
			if got != tt.want {
				t.Fatalf("ResolveWorktreeDir(%q) = %q, want %q", tt.template, got, tt.want)
			}
		})
	}

	t.Run("an invalid template is refused", func(t *testing.T) {
		t.Parallel()
		if _, err := config.ResolveWorktreeDir("../x", repo); !errors.Is(err, config.ErrInvalidWorktreeDir) {
			t.Fatalf("ResolveWorktreeDir error = %v, want ErrInvalidWorktreeDir", err)
		}
	})
}

func TestWorktreeTarget(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "worktrees")
	tests := []struct {
		name    string
		branch  string
		want    string
		wantErr bool
	}{
		{name: "a plain branch", branch: "main", want: filepath.Join(dir, "main")},
		{name: "a branch with a slash nests", branch: "feat/20-menu", want: filepath.Join(dir, "feat", "20-menu")},
		{name: "a branch with two slashes nests twice", branch: "user/feat/x", want: filepath.Join(dir, "user", "feat", "x")},
		{name: "empty", branch: "", wantErr: true},
		{name: "parent segment", branch: "../escape", wantErr: true},
		{name: "parent segment inside", branch: "feat/../../escape", wantErr: true},
		{name: "parent segment that stays inside", branch: "feat/../other", wantErr: true},
		{name: "dot segment", branch: "feat/./x", wantErr: true},
		{name: "absolute", branch: "/etc/passwd", wantErr: true},
		{name: "empty segment", branch: "feat//x", wantErr: true},
		{name: "trailing slash", branch: "feat/", wantErr: true},
		{name: "backslash", branch: `feat\x`, wantErr: true},
		{name: "drive colon", branch: "C:x", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := config.WorktreeTarget(dir, tt.branch)
			if tt.wantErr {
				if !errors.Is(err, config.ErrInvalidWorktreeBranch) {
					t.Fatalf("WorktreeTarget(%q) = %q, %v, want ErrInvalidWorktreeBranch", tt.branch, got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("WorktreeTarget(%q) returned an unexpected error: %v", tt.branch, err)
			}
			if got != tt.want {
				t.Fatalf("WorktreeTarget(%q) = %q, want %q", tt.branch, got, tt.want)
			}
		})
	}
}
