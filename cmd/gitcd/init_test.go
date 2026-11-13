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

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/git"
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

// TestRunInit_RefusesToTargetADeliberateProfileThatIsAlsoTheDefault is
// the exact reproduction pull request 12's review returned as a blocker:
// a profile that already carries deliberate values and is also
// defaultProfile, named explicitly with "--profile". Moving the leaked
// global identity there would both overwrite the deliberate values and
// make the leaked identity the default for every repository no rule
// matches, re-arming the bug this command exists to fix in a broader
// form. "init" must refuse instead, and must not change the file.
func TestRunInit_RefusesToTargetADeliberateProfileThatIsAlsoTheDefault(t *testing.T) {
	home := setupEnv(t)
	cfgPath := filepath.Join(home, ".gitcd.yaml")
	writeFile(t, cfgPath, `
profiles:
  everyday:
    user.name: Deliberate Name
    user.email: deliberate@example.com
defaultProfile: everyday
`)
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read %s: %v", cfgPath, err)
	}

	var stdout, stderr bytes.Buffer
	code := runInit([]string{"--move-identity=yes", "--profile=everyday"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("init --move-identity=yes --profile=everyday = 0, want a refusal when the target is the defaultProfile, stdout: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "defaultProfile") {
		t.Fatalf("init stderr = %q, want it to name the defaultProfile conflict", stderr.String())
	}

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read %s: %v", cfgPath, err)
	}
	if string(after) != string(before) {
		t.Fatalf("init changed %s despite refusing:\nbefore: %q\nafter:  %q", cfgPath, before, after)
	}

	globalAfter, err := os.ReadFile(globalConfigPath(home))
	if err != nil {
		t.Fatalf("read global config: %v", err)
	}
	if !strings.Contains(string(globalAfter), "Test User") || !strings.Contains(string(globalAfter), "[email protected]") {
		t.Fatalf("init removed the global identity despite refusing the move: %q", globalAfter)
	}
}

// TestRunInit_RefusesToOverwriteAPreExistingProfileNamedGlobal covers the
// review's second reproduction of the same blocker: no "--profile" flag
// at all, but a profile already sits under the default target name and
// already carries deliberate values. "init" must refuse rather than
// silently overwriting them.
func TestRunInit_RefusesToOverwriteAPreExistingProfileNamedGlobal(t *testing.T) {
	home := setupEnv(t)
	cfgPath := filepath.Join(home, ".gitcd.yaml")
	writeFile(t, cfgPath, `
profiles:
  global:
    user.email: deliberate@example.test
    user.name: Deliberate Name
`)
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read %s: %v", cfgPath, err)
	}

	var stdout, stderr bytes.Buffer
	code := runInit([]string{"--move-identity=yes"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("init --move-identity=yes = 0, want a refusal when profile %q already exists, stdout: %s", defaultMovedProfileName, stdout.String())
	}
	if !strings.Contains(stderr.String(), "already exists") {
		t.Fatalf("init stderr = %q, want it to name the existing profile conflict", stderr.String())
	}

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read %s: %v", cfgPath, err)
	}
	if string(after) != string(before) {
		t.Fatalf("init changed %s despite refusing:\nbefore: %q\nafter:  %q", cfgPath, before, after)
	}
}

// TestRunInit_AbortsTheMoveWhenTheUpdatedConfigWouldNotLoad covers the
// review's "should": a pre-existing ~/.gitcd.yaml that already carries an
// unknown key still carries it once AddProfileKeys merges the moved
// identity in, so the result does not load either. "init" must abort
// before removing anything from the global git configuration, rather
// than exiting 0 with the identity gone from everywhere and a gitcd
// configuration that does not load.
func TestRunInit_AbortsTheMoveWhenTheUpdatedConfigWouldNotLoad(t *testing.T) {
	home := setupEnv(t)
	globalPath := globalConfigPath(home)
	cfgPath := filepath.Join(home, ".gitcd.yaml")
	writeFile(t, cfgPath, "totallyUnknownKey: 1\n")

	beforeCfg, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read %s: %v", cfgPath, err)
	}
	beforeGlobal, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read %s: %v", globalPath, err)
	}

	var stdout, stderr bytes.Buffer
	code := runInit([]string{"--move-identity=yes"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("init --move-identity=yes = 0, want a failure when the updated configuration would not load, stdout: %s", stdout.String())
	}

	afterCfg, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read %s: %v", cfgPath, err)
	}
	if string(afterCfg) != string(beforeCfg) {
		t.Fatalf("init changed %s despite aborting:\nbefore: %q\nafter:  %q", cfgPath, beforeCfg, afterCfg)
	}

	afterGlobal, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read %s: %v", globalPath, err)
	}
	if string(afterGlobal) != string(beforeGlobal) {
		t.Fatalf("init removed the global identity despite aborting:\nbefore: %q\nafter:  %q", beforeGlobal, afterGlobal)
	}

	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("read dir %s: %v", home, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), filepath.Base(cfgPath)+".gitcd-validate-tmp") {
			t.Fatalf("init left a temporary validation file behind: %s", e.Name())
		}
	}
}

// TestRunInit_NeverClaimsSuccessWhileLosingAMoveIntoABareProfilesKey is
// the exact reproduction pull request 12's review returned as this
// round's blocker: a hand written ~/.gitcd.yaml that declares "profiles:"
// and leaves it empty, the shape "profiles:\n" decodes to a "!!null"
// scalar node rather than a mapping node. "init --move-identity=yes" must
// not be able to print that it moved the identity, remove it from the
// global git configuration, exit 0, and leave ~/.gitcd.yaml byte for byte
// unchanged: either the move genuinely lands in the file, or the command
// refuses and leaves everything, including the global identity, exactly
// as it was.
func TestRunInit_NeverClaimsSuccessWhileLosingAMoveIntoABareProfilesKey(t *testing.T) {
	home := setupEnv(t)
	globalPath := globalConfigPath(home)
	cfgPath := filepath.Join(home, ".gitcd.yaml")
	writeFile(t, cfgPath, "root: ~/Code\nprofiles:\n")

	beforeCfg, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read %s: %v", cfgPath, err)
	}
	beforeGlobal, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read %s: %v", globalPath, err)
	}

	var stdout, stderr bytes.Buffer
	code := runInit([]string{"--move-identity=yes"}, strings.NewReader(""), &stdout, &stderr)

	afterCfg, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read %s: %v", cfgPath, err)
	}
	afterGlobal, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read %s: %v", globalPath, err)
	}
	cfgCarriesMove := strings.Contains(string(afterCfg), "[email protected]")
	globalKeysGone := !strings.Contains(string(afterGlobal), "Test User") && !strings.Contains(string(afterGlobal), "[email protected]")

	// The one outcome that must never happen, regardless of exit code:
	// the identity gone from the global git configuration while
	// ~/.gitcd.yaml does not actually carry it.
	if globalKeysGone && !cfgCarriesMove {
		t.Fatalf(
			"init removed the global identity without %s carrying the move: exit=%d stdout=%q stderr=%q cfg after=%q",
			cfgPath, code, stdout.String(), stderr.String(), afterCfg,
		)
	}

	switch code {
	case 0:
		if !cfgCarriesMove {
			t.Fatalf("init exited 0 without %s carrying the moved identity: %q", cfgPath, afterCfg)
		}
		if !globalKeysGone {
			t.Fatalf("init exited 0 without removing the global identity: %q", afterGlobal)
		}
	default:
		if string(afterCfg) != string(beforeCfg) {
			t.Fatalf("init refused but still changed %s:\nbefore: %q\nafter:  %q", cfgPath, beforeCfg, afterCfg)
		}
		if string(afterGlobal) != string(beforeGlobal) {
			t.Fatalf("init refused but still changed the global git configuration:\nbefore: %q\nafter:  %q", beforeGlobal, afterGlobal)
		}
	}
}

// TestRunInit_DoesNotFollowAPreexistingSymlinkAtTheValidationTempPath
// covers the review's third "should": the validation copy used to be
// written to a fixed, predictable path next to cfgPath with os.WriteFile,
// which follows a symlink already sitting there. A symlink planted at
// that exact legacy name, pointing anywhere else, must not have its
// target overwritten by this command.
func TestRunInit_DoesNotFollowAPreexistingSymlinkAtTheValidationTempPath(t *testing.T) {
	home := setupEnv(t)
	cfgPath := filepath.Join(home, ".gitcd.yaml")

	canary := filepath.Join(home, "canary.txt")
	writeFile(t, canary, "do not touch\n")

	legacyTmpPath := cfgPath + ".gitcd-validate-tmp"
	if err := os.Symlink(canary, legacyTmpPath); err != nil {
		t.Fatalf("symlink %s -> %s: %v", legacyTmpPath, canary, err)
	}

	var stdout, stderr bytes.Buffer
	code := runInit([]string{"--move-identity=yes"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init --move-identity=yes = %d, want 0, stderr: %s", code, stderr.String())
	}

	canaryAfter, err := os.ReadFile(canary)
	if err != nil {
		t.Fatalf("read %s: %v", canary, err)
	}
	if string(canaryAfter) != "do not touch\n" {
		t.Fatalf("canary content = %q, want it untouched: the validation step wrote through a preexisting symlink at its old, predictable path", canaryAfter)
	}

	linkTarget, err := os.Readlink(legacyTmpPath)
	if err != nil {
		t.Fatalf("readlink %s: %v", legacyTmpPath, err)
	}
	if linkTarget != canary {
		t.Fatalf("legacy symlink target = %q, want it left pointing at %q", linkTarget, canary)
	}

	cfgContent, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("init did not write %s: %v", cfgPath, err)
	}
	if !strings.Contains(string(cfgContent), "[email protected]") {
		t.Fatalf("%s = %q, want the moved email under profiles.global", cfgPath, cfgContent)
	}
}

// TestRunInit_WritesThroughASymlinkedGitcdYAMLRatherThanReplacingIt
// covers the review's second "should": ~/.gitcd.yaml symlinked into a
// dotfiles repository, the common setup. The rename this command used to
// do landed directly on the symlink's own path, replacing the link with
// an ordinary file and leaving the dotfiles copy holding the stale
// content. The link, and the dotfiles copy it points at, must both still
// carry the move afterwards.
func TestRunInit_WritesThroughASymlinkedGitcdYAMLRatherThanReplacingIt(t *testing.T) {
	home := setupEnv(t)
	dotfilesCopy := filepath.Join(home, "dotfiles", "gitcd.yaml")
	writeFile(t, dotfilesCopy, "profiles:\n  work:\n    user.email: work@example.test\n")

	cfgPath := filepath.Join(home, ".gitcd.yaml")
	if err := os.Symlink(dotfilesCopy, cfgPath); err != nil {
		t.Fatalf("symlink %s -> %s: %v", cfgPath, dotfilesCopy, err)
	}

	var stdout, stderr bytes.Buffer
	code := runInit([]string{"--move-identity=yes"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init --move-identity=yes = %d, want 0, stderr: %s", code, stderr.String())
	}

	info, err := os.Lstat(cfgPath)
	if err != nil {
		t.Fatalf("lstat %s: %v", cfgPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is no longer a symlink after the move, want the dotfiles link kept intact", cfgPath)
	}
	linkTarget, err := os.Readlink(cfgPath)
	if err != nil {
		t.Fatalf("readlink %s: %v", cfgPath, err)
	}
	if linkTarget != dotfilesCopy {
		t.Fatalf("%s now points at %q, want it still pointing at %q", cfgPath, linkTarget, dotfilesCopy)
	}

	dotfilesContent, err := os.ReadFile(dotfilesCopy)
	if err != nil {
		t.Fatalf("read %s: %v", dotfilesCopy, err)
	}
	if !strings.Contains(string(dotfilesContent), "work@example.test") {
		t.Fatalf("%s = %q, want the pre-existing profile untouched", dotfilesCopy, dotfilesContent)
	}
	if !strings.Contains(string(dotfilesContent), "[email protected]") {
		t.Fatalf("%s = %q, the dotfiles copy itself must carry the moved identity", dotfilesCopy, dotfilesContent)
	}

	// Reading through the symlink must agree with reading the dotfiles
	// copy directly: both name the same file.
	viaLink, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read %s: %v", cfgPath, err)
	}
	if string(viaLink) != string(dotfilesContent) {
		t.Fatalf("reading %s and %s disagree:\nvia link: %q\ndotfiles: %q", cfgPath, dotfilesCopy, viaLink, dotfilesContent)
	}
}

// TestRunInit_MultipleDocumentsRefusalNamesTheFile covers the nit left
// standing across both review rounds: ErrMultipleDocuments's own message
// never names the file it refused, so the printed refusal gave no path
// to look at, even though the configuration may sit at either of two
// candidate locations.
func TestRunInit_MultipleDocumentsRefusalNamesTheFile(t *testing.T) {
	home := setupEnv(t)
	cfgPath := filepath.Join(home, ".gitcd.yaml")
	writeFile(t, cfgPath,
		"profiles:\n  work:\n    user.email: work@example.test\n"+
			"---\n"+
			"profiles:\n  personal:\n    user.email: personal@example.test\n",
	)

	var stdout, stderr bytes.Buffer
	code := runInit([]string{"--move-identity=yes"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("init --move-identity=yes = 0, want a refusal for a multi document file, stdout: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "more than one YAML document") {
		t.Fatalf("init stderr = %q, want it to name the multi document refusal", stderr.String())
	}
	if !strings.Contains(stderr.String(), cfgPath) {
		t.Fatalf("init stderr = %q, want it to name %s, the file it refused", stderr.String(), cfgPath)
	}
}

// TestTargetProfileConflict_RefusesWhenTheLenientParseItselfFails covers
// the first "should": targetProfileConflict used to treat any parse
// failure of its own, lenient decode as "no conflict", waving a move
// through for a file it could not actually make sense of.
func TestTargetProfileConflict_RefusesWhenTheLenientParseItselfFails(t *testing.T) {
	home := setupEnv(t)
	cfgPath := filepath.Join(home, ".gitcd.yaml")
	writeFile(t, cfgPath, "profiles:\n  - not\n  - a\n  - mapping\n")

	msg, err := targetProfileConflict(cfgPath, "global")
	if err != nil {
		t.Fatalf("targetProfileConflict() returned an unexpected error: %v", err)
	}
	if msg == "" {
		t.Fatal("targetProfileConflict() = \"\" (no conflict), want a refusal when the file does not even parse")
	}
}

// TestTargetProfileConflict_AnUnrelatedUnknownKeyAloneIsNotAConflict
// confirms the fix above stays independent of config.LoadFrom's own,
// stricter unknown key rejection: a key this lenient parse does not name
// at all must not, by itself, read as a parse failure.
func TestTargetProfileConflict_AnUnrelatedUnknownKeyAloneIsNotAConflict(t *testing.T) {
	home := setupEnv(t)
	cfgPath := filepath.Join(home, ".gitcd.yaml")
	writeFile(t, cfgPath, "totallyUnknownKey: 1\n")

	msg, err := targetProfileConflict(cfgPath, "global")
	if err != nil {
		t.Fatalf("targetProfileConflict() returned an unexpected error: %v", err)
	}
	if msg != "" {
		t.Fatalf("targetProfileConflict() = %q, want no conflict for a file whose only problem is unrelated to profiles or defaultProfile", msg)
	}
}

// TestTargetProfileConflict_AParseFailureIsNotMaskedByAnUnrelatedUnknownKey
// combines both: a genuinely malformed "profiles" key must still be
// refused even alongside an unrelated unknown key, rather than either
// masking the other.
func TestTargetProfileConflict_AParseFailureIsNotMaskedByAnUnrelatedUnknownKey(t *testing.T) {
	home := setupEnv(t)
	cfgPath := filepath.Join(home, ".gitcd.yaml")
	writeFile(t, cfgPath, "totallyUnknownKey: 1\nprofiles:\n  - not\n  - a\n  - mapping\n")

	msg, err := targetProfileConflict(cfgPath, "global")
	if err != nil {
		t.Fatalf("targetProfileConflict() returned an unexpected error: %v", err)
	}
	if msg == "" {
		t.Fatal("targetProfileConflict() = \"\" (no conflict), want the malformed profiles key refused alongside an unrelated unknown key")
	}
}

// TestIdentityKeysLanded is the unit level proof for the blocker's belt
// and braces guarantee: moveGlobalIdentity must abort, rather than go on
// to remove anything from the global git configuration, whenever the
// configuration it is about to rename into place does not actually carry
// every key it claims to have moved.
func TestIdentityKeysLanded(t *testing.T) {
	t.Parallel()
	found := []git.GlobalIdentityValue{{Key: "user.email", Value: "[email protected]"}}

	t.Run("reports an error when the profile does not carry the moved key", func(t *testing.T) {
		t.Parallel()
		cfg := config.Config{Profiles: map[string]config.Profile{"global": {}}}
		if err := identityKeysLanded(cfg, "global", found); err == nil {
			t.Fatal("identityKeysLanded() = nil, want an error when the profile does not carry the key")
		}
	})

	t.Run("reports an error when the profile does not exist at all", func(t *testing.T) {
		t.Parallel()
		cfg := config.Config{}
		if err := identityKeysLanded(cfg, "global", found); err == nil {
			t.Fatal("identityKeysLanded() = nil, want an error when the profile does not exist at all")
		}
	})

	t.Run("reports an error when the value does not match what was moved", func(t *testing.T) {
		t.Parallel()
		cfg := config.Config{Profiles: map[string]config.Profile{"global": {"user.email": "wrong@example.test"}}}
		if err := identityKeysLanded(cfg, "global", found); err == nil {
			t.Fatal("identityKeysLanded() = nil, want an error when the value does not match")
		}
	})

	t.Run("accepts a profile carrying every moved key with its exact value", func(t *testing.T) {
		t.Parallel()
		cfg := config.Config{Profiles: map[string]config.Profile{"global": {"user.email": "[email protected]"}}}
		if err := identityKeysLanded(cfg, "global", found); err != nil {
			t.Fatalf("identityKeysLanded() = %v, want nil when every moved key matches", err)
		}
	})
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
