// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeProfileConfig(t *testing.T, home string) {
	t.Helper()
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  private:
    user.name: Simon Ostendorf
    user.email: simon@example.test
    user.signingkey: ssh-ed25519 AAAAEXAMPLE
`)
}

func TestRunProfileSet(t *testing.T) {
	t.Run("writes the profile's keys and the gitcd markers to the local config", func(t *testing.T) {
		home := setupEnv(t)
		writeProfileConfig(t, home)
		repo := filepath.Join(home, "repo")
		initRepository(t, repo)

		var stdout, stderr bytes.Buffer
		code := run([]string{"profile", "set", "private", repo}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("profile set = %d, want 0, stderr: %s", code, stderr.String())
		}

		if got := runGitOutput(t, repo, "config", "--local", "--get", "user.email"); got != "simon@example.test" {
			t.Fatalf("user.email = %q, want %q", got, "simon@example.test")
		}
		if got := runGitOutput(t, repo, "config", "--local", "--get", "gitcd.profile"); got != "private" {
			t.Fatalf("gitcd.profile = %q, want %q", got, "private")
		}
		keys := runGitOutput(t, repo, "config", "--local", "--get", "gitcd.keys")
		for _, want := range []string{"user.name", "user.email", "user.signingkey"} {
			if !strings.Contains(keys, want) {
				t.Fatalf("gitcd.keys = %q, want it to list %q", keys, want)
			}
		}
	})

	t.Run("defaults to the current directory when no path is given", func(t *testing.T) {
		home := setupEnv(t)
		writeProfileConfig(t, home)
		repo := filepath.Join(home, "repo")
		initRepository(t, repo)
		t.Chdir(repo)

		var stdout, stderr bytes.Buffer
		code := run([]string{"profile", "set", "private"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("profile set = %d, want 0, stderr: %s", code, stderr.String())
		}
		if got := runGitOutput(t, repo, "config", "--local", "--get", "user.email"); got != "simon@example.test" {
			t.Fatalf("user.email = %q, want %q", got, "simon@example.test")
		}
	})

	t.Run("an unknown profile fails without writing anything", func(t *testing.T) {
		home := setupEnv(t)
		writeProfileConfig(t, home)
		repo := filepath.Join(home, "repo")
		initRepository(t, repo)

		var stdout, stderr bytes.Buffer
		code := run([]string{"profile", "set", "missing", repo}, &stdout, &stderr)
		if code == 0 {
			t.Fatal("profile set missing = 0, want a failure for an undefined profile")
		}
		if localConfigGet(t, repo, "gitcd.profile") == nil {
			t.Fatal("profile set missing wrote gitcd.profile despite failing")
		}
	})

	t.Run("dry run prints the keys without writing them", func(t *testing.T) {
		home := setupEnv(t)
		writeProfileConfig(t, home)
		repo := filepath.Join(home, "repo")
		initRepository(t, repo)

		var stdout, stderr bytes.Buffer
		code := run([]string{"profile", "set", "--dry-run", "private", repo}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("profile set --dry-run = %d, want 0, stderr: %s", code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "simon@example.test") {
			t.Fatalf("profile set --dry-run stdout = %q, want it to print the profile's values", stdout.String())
		}
		if localConfigGet(t, repo, "gitcd.profile") == nil {
			t.Fatal("profile set --dry-run wrote gitcd.profile despite --dry-run")
		}
	})
}

func TestRunProfileUnset(t *testing.T) {
	t.Run("removes exactly the recorded keys and the markers", func(t *testing.T) {
		home := setupEnv(t)
		writeProfileConfig(t, home)
		repo := filepath.Join(home, "repo")
		initRepository(t, repo)
		run([]string{"profile", "set", "private", repo}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run([]string{"profile", "unset", repo}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("profile unset = %d, want 0, stderr: %s", code, stderr.String())
		}

		for _, key := range []string{"user.email", "user.name", "user.signingkey", "gitcd.profile", "gitcd.keys"} {
			if localConfigGet(t, repo, key) == nil {
				t.Fatalf("profile unset left %s set", key)
			}
		}
	})

	t.Run("reports a recorded key that config drift already removed", func(t *testing.T) {
		home := setupEnv(t)
		writeProfileConfig(t, home)
		repo := filepath.Join(home, "repo")
		initRepository(t, repo)
		run([]string{"profile", "set", "private", repo}, &bytes.Buffer{}, &bytes.Buffer{})
		runGit(t, repo, "config", "--local", "--unset", "user.signingkey")

		var stdout, stderr bytes.Buffer
		code := run([]string{"profile", "unset", repo}, &stdout, &stderr)
		if code == 0 {
			t.Fatal("profile unset = 0, want a non-zero exit reporting the leftover")
		}
		if !strings.Contains(stdout.String(), "user.signingkey") {
			t.Fatalf("profile unset stdout = %q, want it to report the already missing key", stdout.String())
		}
	})

	t.Run("a repository with no pinned profile fails", func(t *testing.T) {
		home := setupEnv(t)
		repo := filepath.Join(home, "repo")
		initRepository(t, repo)

		var stdout, stderr bytes.Buffer
		code := run([]string{"profile", "unset", repo}, &stdout, &stderr)
		if code == 0 {
			t.Fatal("profile unset = 0, want a failure when nothing is pinned")
		}
	})
}

func TestRunProfileShow(t *testing.T) {
	home := setupEnv(t)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  work:
    user.email: work@example.test

rules:
  - host: github.com
    owner: ostendorf-cloud
    profile: work
`)
	repo := filepath.Join(home, "repo")
	initRepository(t, repo)
	runGit(t, repo, "remote", "add", "origin", "https://github.com/ostendorf-cloud/gitcd.git")

	var stdout, stderr bytes.Buffer
	code := run([]string{"profile", "show", repo}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("profile show = %d, want 0, stderr: %s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "work") {
		t.Fatalf("profile show stdout = %q, want it to name the expected profile %q", out, "work")
	}
	if !strings.Contains(out, "ostendorf-cloud") {
		t.Fatalf("profile show stdout = %q, want it to name the matched rule's owner", out)
	}
	if !strings.Contains(out, "unset") {
		t.Fatalf("profile show stdout = %q, want it to report user.signingkey as unset", out)
	}
}

// localConfigGet is a thin wrapper so a test can assert a key is unset
// without failing the test itself when it is, the way runGitOutput would.
func localConfigGet(t *testing.T, dir, key string) error {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", "-C", dir, "config", "--local", "--get", key)
	return cmd.Run()
}
