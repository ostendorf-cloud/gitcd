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

// globalConfigPath mirrors setupEnv's own choice of global git configuration
// file, so a test can read and write it directly.
func globalConfigPath(home string) string {
	return filepath.Join(home, "gitconfig-global")
}

func TestRunInit_AddsOneIncludeLineAfterABackup(t *testing.T) {
	home := setupEnv(t)
	globalPath := globalConfigPath(home)

	before, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read %s: %v", globalPath, err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"init"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init = %d, want 0, stderr: %s", code, stderr.String())
	}

	after, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read %s: %v", globalPath, err)
	}
	includePath := filepath.Join(home, ".config", "gitcd", "includes.gitconfig")
	if !strings.Contains(string(after), includePath) {
		t.Fatalf("global config after init = %q, want it to include %q", after, includePath)
	}

	// A backup of the original content must exist somewhere alongside the
	// global config file.
	entries, err := os.ReadDir(filepath.Dir(globalPath))
	if err != nil {
		t.Fatalf("read dir %s: %v", filepath.Dir(globalPath), err)
	}
	var backup string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), filepath.Base(globalPath)+".gitcd-backup-") {
			backup = filepath.Join(filepath.Dir(globalPath), e.Name())
		}
	}
	if backup == "" {
		t.Fatalf("no backup file found next to %s, entries: %v", globalPath, entries)
	}
	backupContent, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("read backup %s: %v", backup, err)
	}
	if string(backupContent) != string(before) {
		t.Fatalf("backup content = %q, want the original global config content %q", backupContent, before)
	}
}

func TestRunInit_IsIdempotent(t *testing.T) {
	home := setupEnv(t)

	var first bytes.Buffer
	if code := run([]string{"init"}, &first, &bytes.Buffer{}); code != 0 {
		t.Fatalf("first init = %d, want 0", code)
	}

	before, err := os.ReadFile(globalConfigPath(home))
	if err != nil {
		t.Fatalf("read global config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"init"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("second init = %d, want 0, stderr: %s", code, stderr.String())
	}

	after, err := os.ReadFile(globalConfigPath(home))
	if err != nil {
		t.Fatalf("read global config: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("second init changed the global config:\nbefore: %q\nafter:  %q", before, after)
	}
}

func TestRunInit_RefusesWhenAnIdentityKeySitsAfterAnExistingInclude(t *testing.T) {
	home := setupEnv(t)
	globalPath := globalConfigPath(home)

	content := "[include]\n\tpath = /some/other/include.gitconfig\n" +
		"[user]\n\tsigningkey = ssh-ed25519 AAAATRAILING\n"
	if err := os.WriteFile(globalPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", globalPath, err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"init"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("init = 0, want a failure when an identity key sits after an existing include")
	}
	if !strings.Contains(stderr.String(), "user.signingkey") {
		t.Fatalf("init stderr = %q, want it to name the offending key", stderr.String())
	}

	after, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read %s: %v", globalPath, err)
	}
	if string(after) != content {
		t.Fatalf("init changed the global config despite refusing: %q", after)
	}
}

func TestRunInit_DryRunChangesNothing(t *testing.T) {
	home := setupEnv(t)
	globalPath := globalConfigPath(home)

	before, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read %s: %v", globalPath, err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"init", "--dry-run"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init --dry-run = %d, want 0, stderr: %s", code, stderr.String())
	}
	if stdout.Len() == 0 {
		t.Fatal("init --dry-run printed nothing")
	}

	after, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read %s: %v", globalPath, err)
	}
	if string(after) != string(before) {
		t.Fatalf("init --dry-run changed the global config:\nbefore: %q\nafter:  %q", before, after)
	}
}

func TestRunInit_NeverWritesToTheRealHomeDirectory(t *testing.T) {
	// A sanity check on this test file's own isolation, not gitcd's
	// behaviour: every init test above must run with os.UserHomeDir()
	// resolving to the isolated, temporary HOME setupEnv sets, never the
	// machine's real home directory, so "init" can never reach the real
	// "~/.gitconfig".
	home := setupEnv(t)
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir(): %v", err)
	}
	if realHome != home {
		t.Fatalf("os.UserHomeDir() = %q, want it to resolve to the isolated %q", realHome, home)
	}
}
