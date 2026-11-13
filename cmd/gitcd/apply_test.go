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

func TestRunApply_WritesTheManagedFiles(t *testing.T) {
	home := setupEnv(t)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  private:
    user.name: Simon Ostendorf
    user.email: simon@example.test

rules:
  - host: github.com
    profile: private

defaultProfile: private
`)

	var stdout, stderr bytes.Buffer
	code := run([]string{"apply"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("apply = %d, want 0, stderr: %s", code, stderr.String())
	}

	profilePath := filepath.Join(home, ".config", "gitcd", "profiles", "private.gitconfig")
	content, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("apply did not write %s: %v", profilePath, err)
	}
	if !strings.Contains(string(content), "simon@example.test") {
		t.Fatalf("%s = %q, want it to carry the configured email", profilePath, content)
	}

	includePath := filepath.Join(home, ".config", "gitcd", "includes.gitconfig")
	if _, err := os.Stat(includePath); err != nil {
		t.Fatalf("apply did not write %s: %v", includePath, err)
	}

	if !strings.Contains(stdout.String(), profilePath) {
		t.Fatalf("apply stdout = %q, want it to list %s", stdout.String(), profilePath)
	}
}

func TestRunApply_DryRunPrintsWithoutWriting(t *testing.T) {
	home := setupEnv(t)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  private:
    user.email: simon@example.test
defaultProfile: private
`)

	var stdout, stderr bytes.Buffer
	code := run([]string{"apply", "--dry-run"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("apply --dry-run = %d, want 0, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "simon@example.test") {
		t.Fatalf("apply --dry-run stdout = %q, want it to print the rendered content", stdout.String())
	}

	managedDir := filepath.Join(home, ".config", "gitcd")
	if _, err := os.Stat(managedDir); err == nil {
		t.Fatalf("apply --dry-run created %s, want it untouched", managedDir)
	}
}

// TestRunApply_HostOnlyRuleMatchesAnScpOwnerRepoRemote guards against a
// host only rule's scp style "hasconfig" glob matching only a single path
// segment: git's own wildmatch only treats "**" as crossing a "/" when a
// "/" sits right in front of it, and the scp form's host only tier has a
// bare "**" straight after the literal ":", with no "/" there. Proven
// here through "git config", the same way the defect itself was found,
// against a repository placed outside the layout root so only the
// "hasconfig" block, never the "gitdir/i" one, can be the one matching.
func TestRunApply_HostOnlyRuleMatchesAnScpOwnerRepoRemote(t *testing.T) {
	home := setupEnv(t)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  private:
    user.email: simon@private.example.test
  work:
    user.email: simon@work.example.test

rules:
  - host: github.com
    profile: work

defaultProfile: private
`)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"init"}, &stdout, &stderr); code != 0 {
		t.Fatalf("init = %d, want 0, stderr: %s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"apply"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apply = %d, want 0, stderr: %s", code, stderr.String())
	}

	repo := filepath.Join(home, "elsewhere", "thing")
	initRepository(t, repo)
	runGit(t, repo, "remote", "add", "origin", "git@github.com:someone/thing.git")

	if got := runGitOutput(t, repo, "config", "--get", "user.email"); got != "simon@work.example.test" {
		t.Fatalf(
			"user.email for an scp remote under a host only rule = %q, want %q (the host only hasconfig block never matched)",
			got, "simon@work.example.test",
		)
	}
}

func TestRunApply_NoProfilesStillWritesAnEmptyIncludeFile(t *testing.T) {
	home := setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"apply"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("apply = %d, want 0, stderr: %s", code, stderr.String())
	}

	includePath := filepath.Join(home, ".config", "gitcd", "includes.gitconfig")
	if _, err := os.Stat(includePath); err != nil {
		t.Fatalf("apply did not write %s: %v", includePath, err)
	}
}
