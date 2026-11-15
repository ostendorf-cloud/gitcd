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

// remotelessWorktree creates a repository with no remote and a linked
// worktree named norepo-wt beside it, returning the worktree's path.
func remotelessWorktree(t *testing.T) (worktree string) {
	t.Helper()
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	main := filepath.Join(root, "local", "norepo")
	initRepository(t, main)
	worktree = filepath.Join(root, "local", "norepo-wt")
	runGit(t, main, "worktree", "add", worktree, "-b", "wt")
	return worktree
}

// TestRunCommands_ReachAWorktreeOfARepositoryWithNoRemote pins the rule
// scanRepositories states: a linked worktree is reachable by "cd" and
// "path" and listed by "status" even when its repository has no remote.
func TestRunCommands_ReachAWorktreeOfARepositoryWithNoRemote(t *testing.T) {
	worktree := remotelessWorktree(t)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"cd resolves the worktree", []string{"cd", "norepo-wt"}, worktree},
		{"path resolves the worktree", []string{"path", "norepo-wt"}, worktree},
		{"status lists the worktree", []string{"status"}, "local/norepo-wt"},
		{"status lists the main checkout", []string{"status"}, "local/norepo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)

			if code != 0 {
				t.Fatalf("run(%v) = %d, want 0, stderr: %s", tt.args, code, stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.want) {
				t.Fatalf("run(%v) stdout = %q, want it to contain %q", tt.args, stdout.String(), tt.want)
			}
		})
	}
}

// TestRunOpen_AWorktreeOfARepositoryWithNoRemoteHasNothingToOpen checks
// "open" resolves the worktree and then fails with the one typed error,
// printing nothing on stdout.
func TestRunOpen_AWorktreeOfARepositoryWithNoRemoteHasNothingToOpen(t *testing.T) {
	worktree := remotelessWorktree(t)

	t.Run("the command exits 1 with nothing on stdout", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"open", "--print", "norepo-wt"}, &stdout, &stderr)

		if code != 1 {
			t.Fatalf("open --print norepo-wt = %d, want 1", code)
		}
		if stdout.Len() != 0 {
			t.Fatalf("open --print norepo-wt wrote to stdout: %q", stdout.String())
		}
	})

	t.Run("the failure is the typed no remote error", func(t *testing.T) {
		_, err := webURL(context.Background(), config.Config{}, worktree)
		if !errors.Is(err, errNoRemoteToOpen) {
			t.Fatalf("webURL() error = %v, want errNoRemoteToOpen", err)
		}
	})
}
