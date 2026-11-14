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

func TestRunAdopt_MovesWithConsentAndPrintsBothPaths(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	oldPath := filepath.Join(home, "elsewhere", "widget")
	initRepository(t, oldPath)
	runGit(t, oldPath, "remote", "add", "origin", "https://github.com/acme/widget.git")

	var stdout, stderr bytes.Buffer
	code := run([]string{"adopt", "--yes", oldPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("adopt --yes = %d, want 0, stderr: %s", code, stderr.String())
	}

	wantNewPath := filepath.Join(root, "github.com", "acme", "widget")
	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("adopt stdout = %q, want exactly two lines, the old and the new path", stdout.String())
	}
	if lines[0] != oldPath {
		t.Fatalf("adopt stdout first line = %q, want the old path %q", lines[0], oldPath)
	}
	if lines[1] != wantNewPath {
		t.Fatalf("adopt stdout second line = %q, want the new path %q", lines[1], wantNewPath)
	}

	if _, err := os.Stat(wantNewPath); err != nil {
		t.Fatalf("adopt did not move the repository to %s: %v", wantNewPath, err)
	}
	if _, err := os.Stat(oldPath); err == nil {
		t.Fatalf("adopt left the old path %s behind", oldPath)
	}
}

func TestRunAdopt_WithoutConsentNeverMoves(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	oldPath := filepath.Join(home, "elsewhere", "widget")
	initRepository(t, oldPath)
	runGit(t, oldPath, "remote", "add", "origin", "https://github.com/acme/widget.git")

	var stdout, stderr bytes.Buffer
	// No "--yes", and stdin is empty: the prompt reaches EOF immediately
	// and must decline, the safe default.
	code := run([]string{"adopt", oldPath}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("adopt with no consent and empty stdin = 0, want a non-zero exit")
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("adopt moved the repository despite no consent: %v", err)
	}
	wantNewPath := filepath.Join(root, "github.com", "acme", "widget")
	if _, err := os.Stat(wantNewPath); err == nil {
		t.Fatalf("adopt moved the repository to %s despite no consent", wantNewPath)
	}
}

func TestRunAdopt_DryRunNeverMoves(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	oldPath := filepath.Join(home, "elsewhere", "widget")
	initRepository(t, oldPath)
	runGit(t, oldPath, "remote", "add", "origin", "https://github.com/acme/widget.git")

	var stdout, stderr bytes.Buffer
	code := run([]string{"adopt", "--dry-run", oldPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("adopt --dry-run = %d, want 0, stderr: %s", code, stderr.String())
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("adopt --dry-run moved the repository: %v", err)
	}
	wantNewPath := filepath.Join(root, "github.com", "acme", "widget")
	if _, err := os.Stat(wantNewPath); err == nil {
		t.Fatalf("adopt --dry-run wrote to %s", wantNewPath)
	}
	if !strings.Contains(stdout.String(), oldPath) || !strings.Contains(stdout.String(), wantNewPath) {
		t.Fatalf("adopt --dry-run stdout = %q, want both the old and the new path", stdout.String())
	}
}

func TestRunAdopt_RefusesWorktreeConfig(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	oldPath := filepath.Join(home, "elsewhere", "widget")
	initRepository(t, oldPath)
	runGit(t, oldPath, "remote", "add", "origin", "https://github.com/acme/widget.git")
	runGit(t, oldPath, "config", "--local", "extensions.worktreeConfig", "true")

	var stdout, stderr bytes.Buffer
	code := run([]string{"adopt", "--yes", oldPath}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("adopt on a repository with extensions.worktreeConfig = 0, want a refusal")
	}
	if !strings.Contains(stderr.String(), "worktreeConfig") {
		t.Fatalf("adopt stderr = %q, want it to name extensions.worktreeConfig", stderr.String())
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("adopt moved the repository despite refusing: %v", err)
	}
}

func TestRunAdopt_RefusesAnExistingTargetWithADifferentRemote(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	newPath := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, newPath)
	runGit(t, newPath, "remote", "add", "origin", "https://github.com/acme/other.git")

	oldPath := filepath.Join(home, "elsewhere", "widget")
	initRepository(t, oldPath)
	runGit(t, oldPath, "remote", "add", "origin", "https://github.com/acme/widget.git")

	var stdout, stderr bytes.Buffer
	code := run([]string{"adopt", "--yes", oldPath}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("adopt onto an occupied target = 0, want a refusal")
	}
	// The specific, friendly wording adoptTargetConflict produces, not
	// merely a path mentioned somewhere: a bare directory rename onto an
	// already occupied target fails at the OS level regardless, with its
	// own message naming both paths, so that alone would not prove this
	// refusal runs before ever attempting the move.
	if !strings.Contains(stderr.String(), "already exists with a different remote") {
		t.Fatalf("adopt stderr = %q, want the specific conflict message naming %s", stderr.String(), newPath)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("adopt moved the repository despite refusing: %v", err)
	}
}

func TestRunAdopt_UpdatesAStalePinToTheCurrentlyExpectedProfile(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
root: `+root+`
profiles:
  work:
    user.email: acme-work-identity
  personal:
    user.email: acme-personal-identity
rules:
  - host: github.com
    owner: acme
    profile: work
`)

	oldPath := filepath.Join(home, "elsewhere", "widget")
	initRepository(t, oldPath)
	runGit(t, oldPath, "remote", "add", "origin", "https://github.com/acme/widget.git")
	runGit(t, oldPath, "config", "--local", "user.email", "acme-personal-identity")
	runGit(t, oldPath, "config", "--local", "gitcd.profile", "personal")

	var stdout, stderr bytes.Buffer
	code := run([]string{"adopt", "--yes", oldPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("adopt --yes = %d, want 0, stderr: %s", code, stderr.String())
	}

	newPath := filepath.Join(root, "github.com", "acme", "widget")
	got := runGitOutput(t, newPath, "config", "--local", "--get", "gitcd.profile")
	if got != "work" {
		t.Fatalf("gitcd.profile after adopt = %q, want it updated to %q, the profile the rules now imply", got, "work")
	}
	gotEmail := runGitOutput(t, newPath, "config", "--local", "--get", "user.email")
	if gotEmail != "acme-work-identity" {
		t.Fatalf("user.email after adopt = %q, want %q", gotEmail, "acme-work-identity")
	}
}

func TestRunAdopt_RefusesALinkedWorktree(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	mainCheckout := filepath.Join(home, "elsewhere", "widget")
	initRepository(t, mainCheckout)
	runGit(t, mainCheckout, "remote", "add", "origin", "https://github.com/acme/widget.git")

	worktree := filepath.Join(home, "elsewhere", "widget-feat")
	runGit(t, mainCheckout, "worktree", "add", worktree, "-b", "feat")

	var stdout, stderr bytes.Buffer
	code := run([]string{"adopt", "--yes", worktree}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("adopt on a linked worktree = 0, want a refusal")
	}
	if !strings.Contains(stderr.String(), "linked worktree") {
		t.Fatalf("adopt stderr = %q, want it to name the linked worktree", stderr.String())
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("adopt moved the linked worktree despite refusing: %v", err)
	}
	// The pair must still work as git itself understands it: a worktree
	// adopt silently broke would fail this, exactly the symptom the
	// review's own reproduction demonstrated.
	runGit(t, worktree, "rev-parse", "--is-inside-work-tree")
}

func TestRunAdopt_RefusesAMainCheckoutWithALinkedWorktree(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	mainCheckout := filepath.Join(home, "elsewhere", "widget")
	initRepository(t, mainCheckout)
	runGit(t, mainCheckout, "remote", "add", "origin", "https://github.com/acme/widget.git")

	worktree := filepath.Join(home, "elsewhere", "widget-feat")
	runGit(t, mainCheckout, "worktree", "add", worktree, "-b", "feat")

	var stdout, stderr bytes.Buffer
	code := run([]string{"adopt", "--yes", mainCheckout}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("adopt on a main checkout with a linked worktree = 0, want a refusal")
	}
	if !strings.Contains(stderr.String(), "linked worktree") {
		t.Fatalf("adopt stderr = %q, want it to name the linked worktree", stderr.String())
	}
	if _, err := os.Stat(mainCheckout); err != nil {
		t.Fatalf("adopt moved the main checkout despite refusing: %v", err)
	}
	// The pair must still work as git itself understands it: a main
	// checkout moved out from under its worktree without repairing the
	// link would fail this, exactly the symptom the review's own
	// reproduction demonstrated.
	runGit(t, worktree, "rev-parse", "--is-inside-work-tree")
}

func TestRunAdopt_RefusesABareRepositoryWithAnAccurateMessage(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	// A bare repository carries no ".git" entry of its own: the
	// directory itself holds git's administrative files directly.
	bare := filepath.Join(home, "elsewhere", "widget.git")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatalf("create %s: %v", bare, err)
	}
	runGit(t, bare, "init", "--bare", "--initial-branch=main")

	var stdout, stderr bytes.Buffer
	code := run([]string{"adopt", "--yes", bare}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("adopt on a bare repository = 0, want a refusal")
	}
	if !strings.Contains(stderr.String(), "bare repository") {
		t.Fatalf("adopt stderr = %q, want it to name %s as a bare repository", stderr.String(), bare)
	}
	if strings.Contains(stderr.String(), "is not a git repository") {
		t.Fatalf("adopt stderr = %q, want it to stop claiming %s is not a git repository at all", stderr.String(), bare)
	}
}

func TestRunAdopt_RefusesAMissingPathAsMissingNotAsNotAGitRepository(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	// Never created at all, not even an empty directory.
	missing := filepath.Join(home, "elsewhere", "widget")

	var stdout, stderr bytes.Buffer
	code := run([]string{"adopt", "--yes", missing}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("adopt on a missing path = 0, want a refusal")
	}
	if !strings.Contains(stderr.String(), "does not exist") {
		t.Fatalf("adopt stderr = %q, want it to say the path does not exist", stderr.String())
	}
	if strings.Contains(stderr.String(), "is not a git repository") {
		t.Fatalf("adopt stderr = %q, want it to stop claiming a missing path is not a git repository", stderr.String())
	}
}

func TestRunAdopt_RefusesAnOrphanedLinkedWorktreeWithGitsOwnReasonFoldedIn(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	mainCheckout := filepath.Join(home, "elsewhere", "widget")
	initRepository(t, mainCheckout)
	runGit(t, mainCheckout, "remote", "add", "origin", "https://github.com/acme/widget.git")

	worktree := filepath.Join(home, "elsewhere", "widget-feat")
	runGit(t, mainCheckout, "worktree", "add", worktree, "-b", "feat")

	// Orphan the linked worktree: its main checkout is gone, so every
	// git invocation inside it now fails with git's own diagnostic
	// ("fatal: not a git repository: (null)") rather than succeeding.
	if err := os.RemoveAll(mainCheckout); err != nil {
		t.Fatalf("remove %s: %v", mainCheckout, err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"adopt", "--yes", worktree}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("adopt on an orphaned linked worktree = 0, want a refusal")
	}
	if !strings.Contains(stderr.String(), "fatal:") {
		t.Fatalf(
			"adopt stderr = %q, want git's own stderr folded into the message, not a bare \"exit status 128\"", stderr.String(),
		)
	}
}

func TestRunAdopt_RefusesMovingIntoItselfAndLeavesNoDirectoryBehind(t *testing.T) {
	home := setupEnv(t)
	// The repository itself becomes the configured root, so the layout
	// renders a target path nested inside the very repository being
	// adopted: the scenario the review's own reproduction used.
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")
	initRepository(t, root)
	runGit(t, root, "remote", "add", "origin", "https://github.com/acme/gadget.git")

	var stdout, stderr bytes.Buffer
	code := run([]string{"adopt", "--yes", root}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("adopt onto a target nested inside the source = 0, want a refusal")
	}
	if !strings.Contains(stderr.String(), "inside") {
		t.Fatalf("adopt stderr = %q, want it to say the target is inside the source", stderr.String())
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read %s: %v", root, err)
	}
	for _, e := range entries {
		if e.Name() == "github.com" {
			t.Fatalf("adopt left %s behind inside the repository it refused to move", filepath.Join(root, "github.com"))
		}
	}
}

func TestRunAdopt_MkdirAllFailureLeavesNoDirectoryBehind(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	oldPath := filepath.Join(home, "elsewhere", "widget")
	initRepository(t, oldPath)
	// An owner segment long enough to exceed the filesystem's own name
	// limit: "os.MkdirAll" creates every missing ancestor up to it, then
	// fails partway through, exactly the review's own reproduction.
	longOwner := strings.Repeat("a", 300)
	runGit(t, oldPath, "remote", "add", "origin", "https://github.com/"+longOwner+"/widget.git")

	var stdout, stderr bytes.Buffer
	code := run([]string{"adopt", "--yes", oldPath}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("adopt with a target MkdirAll cannot create = 0, want a refusal")
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("adopt moved the source despite the MkdirAll failure: %v", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf(
			"adopt left %s behind after the MkdirAll failure (stat err: %v); want every directory it created removed", root, err,
		)
	}
}

func TestRunAdopt_TreatsAnUncheckableTargetAsAConflict(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	// The target's immediate parent exists but is unreadable: "os.Stat"
	// on the target itself fails with a permission error, not "the path
	// does not exist".
	blocked := filepath.Join(root, "github.com", "acme")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatalf("create %s: %v", blocked, err)
	}
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatalf("chmod %s: %v", blocked, err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })

	oldPath := filepath.Join(home, "elsewhere", "widget")
	initRepository(t, oldPath)
	runGit(t, oldPath, "remote", "add", "origin", "https://github.com/acme/widget.git")

	var stdout, stderr bytes.Buffer
	code := run([]string{"adopt", "--yes", oldPath}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("adopt onto an unreadable target = 0, want a refusal")
	}
	if !strings.Contains(stderr.String(), "cannot check") {
		t.Fatalf("adopt stderr = %q, want it to say the target could not be checked", stderr.String())
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("adopt moved the repository despite an unreadable target: %v", err)
	}
}

func TestRunAdopt_ExactlyOnePathIsRequired(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"adopt"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("adopt with no path = %d, want 2", code)
	}
}
