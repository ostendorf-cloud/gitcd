// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
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

func TestRunInit_ReportsGlobalIdentityKeysWithoutMovingByDefault(t *testing.T) {
	home := setupEnv(t)
	globalPath := globalConfigPath(home)

	var stdout, stderr bytes.Buffer
	code := run([]string{"init"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init = %d, want 0, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "user.email") {
		t.Fatalf("init stdout = %q, want it to name user.email, set globally by setupEnv", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"global"`) {
		t.Fatalf("init stdout = %q, want it to name the profile a move would use", stdout.String())
	}

	after, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read %s: %v", globalPath, err)
	}
	if !strings.Contains(string(after), "email") {
		t.Fatalf("init removed the global identity key despite no --move-identity flag: %q", after)
	}
	// The include line is still added: reporting a leaked identity key
	// must not block the rest of "init" from doing its ordinary job.
	includePath := filepath.Join(home, ".config", "gitcd", "includes.gitconfig")
	if !strings.Contains(string(after), includePath) {
		t.Fatalf("global config after init = %q, want it to include %q", after, includePath)
	}

	if _, err := os.Stat(filepath.Join(home, ".gitcd.yaml")); err == nil {
		t.Fatal("init created ~/.gitcd.yaml despite no --move-identity flag, want it left untouched")
	}
}

func TestRunInit_MovesIdentityKeysWhenExplicitlyToldYes(t *testing.T) {
	home := setupEnv(t)
	globalPath := globalConfigPath(home)

	var stdout, stderr bytes.Buffer
	code := runInit([]string{"--move-identity=yes"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init --move-identity=yes = %d, want 0, stderr: %s", code, stderr.String())
	}

	after, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read %s: %v", globalPath, err)
	}
	if strings.Contains(string(after), "Test User") || strings.Contains(string(after), "[email protected]") {
		t.Fatalf("global config after a move = %q, want the identity keys removed", after)
	}

	cfgPath := filepath.Join(home, ".gitcd.yaml")
	cfgContent, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("init did not write %s: %v", cfgPath, err)
	}
	if !strings.Contains(string(cfgContent), "[email protected]") {
		t.Fatalf("%s = %q, want the moved email under profiles.global", cfgPath, cfgContent)
	}

	// The global git config must have a backup of its pre-move content,
	// so the move is reversible. gitcd.yaml did not exist before this
	// run, so no backup of it is expected here; a pre-existing
	// gitcd.yaml getting backed up is covered separately below.
	gitBackupEntries, err := os.ReadDir(filepath.Dir(globalPath))
	if err != nil {
		t.Fatalf("read dir %s: %v", filepath.Dir(globalPath), err)
	}
	var sawGitBackup bool
	for _, e := range gitBackupEntries {
		if strings.HasPrefix(e.Name(), filepath.Base(globalPath)+".gitcd-backup-") {
			sawGitBackup = true
		}
	}
	if !sawGitBackup {
		t.Fatalf("no backup of %s found, entries: %v", globalPath, gitBackupEntries)
	}
}

func TestRunInit_MoveBacksUpAnExistingGitcdYAMLBeforeEditingIt(t *testing.T) {
	home := setupEnv(t)
	cfgPath := filepath.Join(home, ".gitcd.yaml")
	writeFile(t, cfgPath, "profiles:\n  work:\n    user.email: work@example.test\nrules:\n  - host: github.com\n    profile: work\n")

	var stdout, stderr bytes.Buffer
	code := runInit([]string{"--move-identity=yes"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init --move-identity=yes = %d, want 0, stderr: %s", code, stderr.String())
	}

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read %s: %v", cfgPath, err)
	}
	if !strings.Contains(string(after), "work@example.test") {
		t.Fatalf("%s after a move = %q, want the pre-existing profile untouched", cfgPath, after)
	}
	if !strings.Contains(string(after), "[email protected]") {
		t.Fatalf("%s after a move = %q, want the moved email under profiles.global", cfgPath, after)
	}

	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("read dir %s: %v", home, err)
	}
	var backup string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), filepath.Base(cfgPath)+".gitcd-backup-") {
			backup = filepath.Join(home, e.Name())
		}
	}
	if backup == "" {
		t.Fatalf("no backup of the pre-existing %s found, entries: %v", cfgPath, entries)
	}
	backupContent, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("read backup %s: %v", backup, err)
	}
	if !strings.Contains(string(backupContent), "work@example.test") || strings.Contains(string(backupContent), "[email protected]") {
		t.Fatalf("backup content = %q, want it to hold the configuration exactly as it was before the move", backupContent)
	}
}

func TestRunInit_AsksInteractivelyAndMovesOnYesAnswer(t *testing.T) {
	home := setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := runInit([]string{"--move-identity=ask"}, strings.NewReader("y\n"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init --move-identity=ask (y) = %d, want 0, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "[y/N]") {
		t.Fatalf("init --move-identity=ask stderr = %q, want a prompt", stderr.String())
	}

	after, err := os.ReadFile(globalConfigPath(home))
	if err != nil {
		t.Fatalf("read global config: %v", err)
	}
	if strings.Contains(string(after), "[email protected]") {
		t.Fatalf("global config after answering yes = %q, want the identity key removed", after)
	}
}

func TestRunInit_AsksInteractivelyAndSkipsOnNoAnswer(t *testing.T) {
	home := setupEnv(t)
	globalPath := globalConfigPath(home)
	before, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read %s: %v", globalPath, err)
	}

	var stdout, stderr bytes.Buffer
	code := runInit([]string{"--move-identity=ask"}, strings.NewReader("n\n"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init --move-identity=ask (n) = %d, want 0, stderr: %s", code, stderr.String())
	}

	after, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read %s: %v", globalPath, err)
	}
	// The include line is still added on a declined move; only the
	// identity keys must survive untouched.
	if !strings.Contains(string(after), "[email protected]") {
		t.Fatalf("global config after declining = %q, want the identity key left in place", after)
	}
	if string(before) == string(after) {
		t.Fatal("init made no change at all after declining the move, want the include line still added")
	}
}

func TestRunInit_AsksInteractivelyDefaultsToNoOnClosedStdin(t *testing.T) {
	// A script or CI job commonly runs with an empty or closed stdin,
	// never a terminal: "--move-identity=ask" must resolve to the safe
	// "no" answer there instead of blocking forever waiting for a line
	// that will never arrive.
	home := setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := runInit([]string{"--move-identity=ask"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init --move-identity=ask (closed stdin) = %d, want 0, stderr: %s", code, stderr.String())
	}

	after, err := os.ReadFile(globalConfigPath(home))
	if err != nil {
		t.Fatalf("read global config: %v", err)
	}
	if !strings.Contains(string(after), "[email protected]") {
		t.Fatalf("global config after a closed stdin prompt = %q, want the identity key left in place", after)
	}
}

func TestRunInit_DryRunDescribesTheMoveWithoutWriting(t *testing.T) {
	home := setupEnv(t)
	globalPath := globalConfigPath(home)
	before, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read %s: %v", globalPath, err)
	}

	var stdout, stderr bytes.Buffer
	code := runInit([]string{"--dry-run", "--move-identity=yes"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init --dry-run --move-identity=yes = %d, want 0, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "would move") {
		t.Fatalf("init --dry-run stdout = %q, want it to describe the move", stdout.String())
	}

	after, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read %s: %v", globalPath, err)
	}
	if string(after) != string(before) {
		t.Fatalf("init --dry-run changed the global config:\nbefore: %q\nafter:  %q", before, after)
	}
	if _, err := os.Stat(filepath.Join(home, ".gitcd.yaml")); err == nil {
		t.Fatal("init --dry-run created ~/.gitcd.yaml, want it left untouched")
	}
}

// TestRunInit_UnmatchedRepositoryRefusesToCommitAfterMove is the
// end-to-end acceptance proof for issue 8: reproduces the exact scenario
// a global identity defeats "useConfigOnly" for (a global user.name and
// user.email, a rule for one host only, no defaultProfile), moves the
// leaked keys out through "gitcd init --move-identity=yes", runs "gitcd
// apply", and then attempts a real commit, through the actual git
// binary, in a repository whose remote matches no rule. Before the move,
// this same sequence commits successfully under the leaked global
// identity; the assertion below is that it now fails instead.
func TestRunInit_UnmatchedRepositoryRefusesToCommitAfterMove(t *testing.T) {
	home := setupEnv(t)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
rules:
  - host: github.com
    profile: work

profiles:
  work:
    user.email: work@example.test
`)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"init", "--move-identity=yes"}, &stdout, &stderr); code != 0 {
		t.Fatalf("init --move-identity=yes = %d, want 0, stderr: %s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"apply"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apply = %d, want 0, stderr: %s", code, stderr.String())
	}

	// Built by hand rather than through initRepository, which commits as
	// part of its own setup: after the move, that commit would already
	// be the one expected to fail, leaving nothing left for this test's
	// own, deliberate commit attempt to prove.
	repo := filepath.Join(home, "elsewhere", "unmatched")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("create %s: %v", repo, err)
	}
	runGit(t, repo, "init", "--initial-branch=main")
	runGit(t, repo, "remote", "add", "origin", "git@gitlab.com:someone/unmatched.git")
	writeFile(t, filepath.Join(repo, "README.md"), "example\n")
	runGit(t, repo, "add", "README.md")

	cmd := exec.CommandContext(context.Background(), "git", "-c", "core.editor=true", "commit", "-m", "fix: probe an unmatched repository")
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("commit in an unmatched repository succeeded after moving the leaked identity, want it to fail: %s", out)
	}

	effectiveEmail := runGitShowOriginAllowFailure(t, repo, "user.email")
	if effectiveEmail != "" {
		t.Fatalf("user.email resolved to %q in the unmatched repository, want it unset", effectiveEmail)
	}
}

// runGitShowOriginAllowFailure runs "git -C dir config --get key",
// returning its trimmed stdout, or "" when git exits 1 because the key
// carries no value at all, which is the expected outcome here, not a
// test failure.
func runGitShowOriginAllowFailure(t *testing.T, dir, key string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", "-C", dir, "config", "--get", key)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return ""
		}
		t.Fatalf("git -C %s config --get %s: %v", dir, key, err)
	}
	return strings.TrimSpace(string(out))
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
