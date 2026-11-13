// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDoctor_NoArgumentsAccepted(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "extra"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("doctor with an argument = %d, want 2", code)
	}
}

func TestRunDoctor_ReportsAMissingManagedFileAndExitsNonZero(t *testing.T) {
	home := setupEnv(t)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  work:
    user.email: acme-work-identity
rules:
  - host: github.com
    profile: work
`)

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("doctor = %d, want 1, stdout: %s stderr: %s", code, stdout.String(), stderr.String())
	}

	out := stdout.String()
	includePath := filepath.Join(home, ".config", "gitcd", "includes.gitconfig")
	if !strings.Contains(out, includePath) {
		t.Fatalf("doctor stdout = %q, want it to name the missing include file %s", out, includePath)
	}
	cfgPath := filepath.Join(home, ".gitcd.yaml")
	if !strings.Contains(out, cfgPath) {
		t.Fatalf("doctor stdout = %q, want it to print the config file path %s", out, cfgPath)
	}
	managedDir := filepath.Join(home, ".config", "gitcd")
	if !strings.Contains(out, managedDir) {
		t.Fatalf("doctor stdout = %q, want it to print the managed directory %s", out, managedDir)
	}
}

func TestRunDoctor_NoRepositoriesAndNoManagedFilesNeededIsStillNonZeroOnAGlobalIdentityKey(t *testing.T) {
	// setupEnv itself sets a global user.name and user.email, exactly
	// the finding doctor exists to report: with no profiles configured
	// at all, there is nothing to render, so the only finding left is
	// the global identity key setupEnv's own baseline carries.
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("doctor = %d, want 1 for a global identity key finding, stdout: %s stderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "global-identity-key") {
		t.Fatalf("doctor stdout = %q, want a global-identity-key finding", stdout.String())
	}
}

func TestRunDoctor_FixRerendersAMissingManagedFile(t *testing.T) {
	home := setupEnv(t)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  work:
    user.email: acme-work-identity
rules:
  - host: github.com
    profile: work
`)
	includePath := filepath.Join(home, ".config", "gitcd", "includes.gitconfig")
	if _, err := os.Stat(includePath); err == nil {
		t.Fatalf("%s already exists before --fix", includePath)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--fix"}, &stdout, &stderr)
	// The global identity key setupEnv's own baseline carries is never
	// fixed, so the exit code stays non-zero even after a successful fix
	// of everything mechanical.
	if code != 1 {
		t.Fatalf("doctor --fix = %d, want 1 (the global identity key finding is never fixed), stderr: %s", code, stderr.String())
	}
	if _, err := os.Stat(includePath); err != nil {
		t.Fatalf("doctor --fix did not render %s: %v", includePath, err)
	}
	if !strings.Contains(stdout.String(), "fixed [managed-file-missing]") {
		t.Fatalf("doctor --fix stdout = %q, want it to report the managed file finding as fixed", stdout.String())
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		if strings.HasPrefix(line, "[managed-file-missing]") || strings.HasPrefix(line, "[managed-file-stale]") {
			t.Fatalf("doctor --fix stdout carries an outstanding managed file finding: %q", line)
		}
	}
}

func TestRunDoctor_FixDryRunChangesNothing(t *testing.T) {
	home := setupEnv(t)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  work:
    user.email: acme-work-identity
rules:
  - host: github.com
    profile: work
`)
	includePath := filepath.Join(home, ".config", "gitcd", "includes.gitconfig")

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--fix", "--dry-run"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("doctor --fix --dry-run = %d, want 1, stderr: %s", code, stderr.String())
	}
	if _, err := os.Stat(includePath); err == nil {
		t.Fatalf("doctor --fix --dry-run wrote %s", includePath)
	}
	if !strings.Contains(stdout.String(), "would rerender") {
		t.Fatalf("doctor --fix --dry-run stdout = %q, want it to describe what would change", stdout.String())
	}
}

func TestRunDoctor_JSONOutput(t *testing.T) {
	home := setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--json"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("doctor --json = %d, want 1, stderr: %s", code, stderr.String())
	}

	var out struct {
		ConfigPath string `json:"configPath"`
		ManagedDir string `json:"managedDir"`
		Findings   []struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("doctor --json produced invalid JSON: %v, output: %s", err, stdout.String())
	}
	if out.ConfigPath != filepath.Join(home, ".gitcd.yaml") {
		t.Fatalf("doctor --json configPath = %q, want %q", out.ConfigPath, filepath.Join(home, ".gitcd.yaml"))
	}
	if len(out.Findings) == 0 {
		t.Fatal("doctor --json findings is empty, want at least the global identity key finding")
	}
}

func TestRunDoctor_FixRenamesACaseOnlyPathMismatch(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	// The layout renders lowercase, but this clone sits under an upper
	// cased owner segment: a case only mismatch.
	misCased := filepath.Join(root, "github.com", "ACME", "widget")
	initRepository(t, misCased)
	runGit(t, misCased, "remote", "add", "origin", "https://github.com/acme/widget.git")

	var stdout, stderr bytes.Buffer
	// The global identity key from setupEnv's own baseline is never
	// fixed, so the exit code is not asserted here: TestRunDoctor_FixRerendersAMissingManagedFile
	// already covers that it stays non-zero.
	run([]string{"doctor", "--fix"}, &stdout, &stderr)

	wantPath := filepath.Join(root, "github.com", "acme", "widget")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("doctor --fix did not rename the repository to %s: %v", wantPath, err)
	}
	// os.Stat alone cannot tell the two spellings apart on a case
	// insensitive, case preserving filesystem (APFS, the macOS default):
	// it resolves "ACME" and "acme" to the same entry either way. Reading
	// the parent directory back reports the entry's own, exact spelling.
	entries, err := os.ReadDir(filepath.Join(root, "github.com"))
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(root, "github.com"), err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !containsExact(names, "acme") {
		t.Fatalf("directory entries under github.com = %v, want the exact spelling %q on disk", names, "acme")
	}
	if containsExact(names, "ACME") {
		t.Fatalf("directory entries under github.com = %v, want the old spelling %q gone", names, "ACME")
	}
	if !strings.Contains(stdout.String(), "renamed") {
		t.Fatalf("doctor --fix stdout = %q, want it to report the rename", stdout.String())
	}
}

func containsExact(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}
