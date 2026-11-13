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

// runGitOutput is runGit, returning git's trimmed stdout instead of
// discarding it.
func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
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

// goEnv pins the toolchain's own GOPATH, GOCACHE and GOMODCACHE, read once
// here before any test's setupEnv can point HOME at a fresh temporary
// directory. buildGitcd needs them read before that happens: "go build"
// run with a temporary HOME would otherwise compute a fresh, empty module
// cache from it and try to repopulate it over the network.
var goEnv = resolveGoEnv()

func resolveGoEnv() []string {
	var env []string
	for _, key := range []string{"GOPATH", "GOCACHE", "GOMODCACHE"} {
		cmd := exec.CommandContext(context.Background(), "go", "env", key)
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		env = append(env, key+"="+strings.TrimSpace(string(out)))
	}
	return env
}

// buildGitcd compiles the real gitcd binary into a fresh temporary
// directory and returns its path. A test needs the real binary, rather
// than calling run directly, only when it observes gitcd from outside the
// test binary itself, such as a shell wrapper's own "command gitcd" call.
func buildGitcd(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "gitcd")
	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", bin, ".")
	cmd.Env = append(os.Environ(), goEnv...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build gitcd: %v: %s", err, out)
	}
	return bin
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
