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
