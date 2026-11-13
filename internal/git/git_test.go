// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package git_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/git"
)

// setupGitEnv isolates every git invocation in the test from the real
// user's configuration and from the network: a dedicated global config
// file, no system config, and a controlled HOME so "~" in a pattern
// expands from a path the test owns.
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

// initRepository creates a git repository under dir with one committed
// file, so a test has a source to clone from without reaching the network.
func initRepository(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("example\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	run(t, dir, "add", "README.md")
	run(t, dir, "commit", "-m", "add README")
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func TestClone(t *testing.T) {
	setupGitEnv(t)

	t.Run("clones a local repository into a new destination", func(t *testing.T) {
		root := t.TempDir()
		source := filepath.Join(root, "source")
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Fatalf("create %s: %v", source, err)
		}
		initRepository(t, source)

		dest := filepath.Join(root, "nested", "dest")
		if err := git.Clone(context.Background(), source, dest, io.Discard); err != nil {
			t.Fatalf("Clone(%q, %q) returned an unexpected error: %v", source, dest, err)
		}

		if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
			t.Fatalf("cloned repository is missing its content: %v", err)
		}
		if !git.IsRepository(dest) {
			t.Fatalf("IsRepository(%q) = false, want true after a successful clone", dest)
		}
	})

	t.Run("wraps the underlying error for a source that does not exist", func(t *testing.T) {
		root := t.TempDir()
		dest := filepath.Join(root, "dest")

		err := git.Clone(context.Background(), filepath.Join(root, "missing"), dest, io.Discard)
		if err == nil {
			t.Fatal("Clone() succeeded, want an error for a missing source")
		}
	})

	// TestClone/streams_the_clone_s_own_progress_to_the_given_writer guards
	// against "CombinedOutput", which buffers a clone's progress until the
	// command finishes and, before this fix, discarded it entirely on
	// success: a caller had no way to show the user anything while a large
	// clone was still running. Clone must now write git's own progress to
	// the given writer unconditionally, not only fold it into an error.
	t.Run("streams the clone's own progress to the given writer", func(t *testing.T) {
		root := t.TempDir()
		source := filepath.Join(root, "source")
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Fatalf("create %s: %v", source, err)
		}
		initRepository(t, source)

		dest := filepath.Join(root, "dest")
		var progress bytes.Buffer
		if err := git.Clone(context.Background(), source, dest, &progress); err != nil {
			t.Fatalf("Clone(%q, %q) returned an unexpected error: %v", source, dest, err)
		}

		if progress.Len() == 0 {
			t.Fatal("Clone() wrote nothing to the given writer, want git's own clone progress streamed to it")
		}
	})
}

func TestCloneWithConfig(t *testing.T) {
	setupGitEnv(t)

	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatalf("create %s: %v", source, err)
	}
	initRepository(t, source)

	dest := filepath.Join(root, "dest")
	pin := map[string]string{"user.email": "[email protected]", "user.name": "Pinned User"}
	if err := git.CloneWithConfig(context.Background(), source, dest, pin, io.Discard); err != nil {
		t.Fatalf("CloneWithConfig() returned an unexpected error: %v", err)
	}

	value, ok, err := git.ShowOrigin(context.Background(), dest, "user.email")
	if err != nil {
		t.Fatalf("ShowOrigin() returned an unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("ShowOrigin() ok = false, want true: the clone should have pinned user.email")
	}
	if value.Value != "[email protected]" {
		t.Fatalf("ShowOrigin() value = %q, want %q, the value CloneWithConfig pinned", value.Value, "[email protected]")
	}
}

func TestRemoteURL(t *testing.T) {
	setupGitEnv(t)

	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatalf("create %s: %v", source, err)
	}
	initRepository(t, source)

	dest := filepath.Join(root, "dest")
	if err := git.Clone(context.Background(), source, dest, io.Discard); err != nil {
		t.Fatalf("Clone(%q, %q) returned an unexpected error: %v", source, dest, err)
	}

	got, err := git.RemoteURL(context.Background(), dest)
	if err != nil {
		t.Fatalf("RemoteURL(%q) returned an unexpected error: %v", dest, err)
	}
	if got != source {
		t.Fatalf("RemoteURL(%q) = %q, want %q", dest, got, source)
	}
}

// TestRemoteURL_IsNotResolvedThroughAnInsteadOfRewrite guards against
// "git remote get-url", which expands a configured "url.<base>.insteadOf"
// rewrite: RemoteURL must return the literal value "git clone" stored,
// the one that was actually requested, not the rewritten one a corporate
// mirror or an SSH host alias might point at instead. gitcd compares this
// value, parsed, against the remote it was just asked to clone, and a
// rewritten target is not guaranteed to parse at all.
func TestRemoteURL_IsNotResolvedThroughAnInsteadOfRewrite(t *testing.T) {
	setupGitEnv(t)

	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatalf("create %s: %v", source, err)
	}
	initRepository(t, source)

	requested := "https://example.test/acme/widget.git"
	run(t, root, "config", "--global", "url."+"file://"+source+".insteadOf", requested)

	dest := filepath.Join(root, "dest")
	if err := git.Clone(context.Background(), requested, dest, io.Discard); err != nil {
		t.Fatalf("Clone(%q, %q) returned an unexpected error: %v", requested, dest, err)
	}

	got, err := git.RemoteURL(context.Background(), dest)
	if err != nil {
		t.Fatalf("RemoteURL(%q) returned an unexpected error: %v", dest, err)
	}
	if got != requested {
		t.Fatalf("RemoteURL(%q) = %q, want the literal requested %q, not the rewritten target", dest, got, requested)
	}
}

func TestIsRepository(t *testing.T) {
	setupGitEnv(t)

	t.Run("an ordinary clone is a repository", func(t *testing.T) {
		root := t.TempDir()
		source := filepath.Join(root, "source")
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Fatalf("create %s: %v", source, err)
		}
		initRepository(t, source)

		if !git.IsRepository(source) {
			t.Fatalf("IsRepository(%q) = false, want true", source)
		}
	})

	t.Run("an ordinary directory without a dot git entry is not a repository", func(t *testing.T) {
		dir := t.TempDir()
		if git.IsRepository(dir) {
			t.Fatalf("IsRepository(%q) = true, want false for a plain directory", dir)
		}
	})

	t.Run("a linked worktree is a repository even though its dot git is a file", func(t *testing.T) {
		root := t.TempDir()
		source := filepath.Join(root, "source")
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Fatalf("create %s: %v", source, err)
		}
		initRepository(t, source)

		worktree := filepath.Join(root, "worktree")
		run(t, source, "worktree", "add", worktree)

		info, err := os.Stat(filepath.Join(worktree, ".git"))
		if err != nil {
			t.Fatalf("stat the worktree's dot git entry: %v", err)
		}
		if info.IsDir() {
			t.Fatalf("worktree's dot git entry is a directory, want a file, to exercise the case IsRepository must also cover")
		}
		if !git.IsRepository(worktree) {
			t.Fatalf("IsRepository(%q) = false, want true for a linked worktree", worktree)
		}
	})
}
