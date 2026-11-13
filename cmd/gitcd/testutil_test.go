// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupEnv isolates every command under test, and every git invocation it
// makes, from the real user's environment and from the network: a
// dedicated HOME and XDG_CONFIG_HOME, and a dedicated git global config
// with no system config. It returns the home directory, so a test can
// derive the default clone root under it.
func setupEnv(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	globalGitConfig := filepath.Join(home, "gitconfig-global")

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", globalGitConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	runGit(t, home, "config", "--global", "user.name", "Test User")
	runGit(t, home, "config", "--global", "user.email", "[email protected]")

	return home
}

// runGit runs git with args in dir, failing the test on a non-zero exit.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

// initRepository creates a git repository under dir with one committed
// file, a source test commands can clone from without reaching the
// network.
func initRepository(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	runGit(t, dir, "init", "--initial-branch=main")
	writeFile(t, filepath.Join(dir, "README.md"), "example\n")
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "add README")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
