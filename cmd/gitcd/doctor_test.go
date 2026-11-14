// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func TestRunDoctor_GlobalIdentityFindingNamesTheConfiguredDefaultProfile(t *testing.T) {
	home := setupEnv(t)
	// setupEnv's own baseline already sets a global user.name and
	// user.email directly, exactly the KindGlobalIdentityKey finding
	// this test inspects the wording of.
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  work:
    user.email: acme-work-identity
defaultProfile: work
rules:
  - host: github.com
    profile: work
`)

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("doctor = %d, want 1, stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"work"`) {
		t.Fatalf(
			"doctor stdout = %q, want the global identity finding to name the configured default profile %q", out, "work",
		)
	}
	if strings.Contains(out, `"global"`) {
		t.Fatalf(
			"doctor stdout = %q, want it to stop naming the hardcoded %q profile once a default profile is configured",
			out, "global",
		)
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

func TestRunDoctor_JSONFixOutputIsValidJSON(t *testing.T) {
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
	run([]string{"doctor", "--json", "--fix"}, &stdout, &stderr)

	var out struct {
		ConfigPath string           `json:"configPath"`
		Fixed      []map[string]any `json:"fixed"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("doctor --json --fix produced invalid JSON on stdout: %v, output: %s", err, stdout.String())
	}
	if out.ConfigPath != filepath.Join(home, ".gitcd.yaml") {
		t.Fatalf("doctor --json --fix configPath = %q, want %q", out.ConfigPath, filepath.Join(home, ".gitcd.yaml"))
	}
	if len(out.Fixed) == 0 {
		t.Fatal("doctor --json --fix fixed is empty, want the managed file fix reported")
	}
}

func TestRunDoctor_JSONFixDryRunOutputIsValidJSON(t *testing.T) {
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
	run([]string{"doctor", "--json", "--fix", "--dry-run"}, &stdout, &stderr)

	var out struct {
		ConfigPath string `json:"configPath"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("doctor --json --fix --dry-run produced invalid JSON on stdout: %v, output: %s", err, stdout.String())
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

func TestRunDoctor_FixRenameMessageNamesTheAncestorNotTheRepoPath(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	// Two repositories share one mis-cased owner ancestor, and both
	// remotes agree it should read lowercase: a legitimate, uncontested
	// rename of the shared ancestor, not the per-repository leaf path.
	widget := filepath.Join(root, "github.com", "ACME", "widget")
	initRepository(t, widget)
	runGit(t, widget, "remote", "add", "origin", "https://github.com/acme/widget.git")
	gadget := filepath.Join(root, "github.com", "ACME", "gadget")
	initRepository(t, gadget)
	runGit(t, gadget, "remote", "add", "origin", "https://github.com/acme/gadget.git")

	var stdout, stderr bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdout, &stderr)

	oldAncestor := filepath.Join(root, "github.com", "ACME")
	newAncestor := filepath.Join(root, "github.com", "acme")
	wantLine := fmt.Sprintf("renamed %s to %s", oldAncestor, newAncestor)
	if !strings.Contains(stdout.String(), wantLine) {
		t.Fatalf("doctor --fix stdout = %q, want a line naming the ancestor pair %q", stdout.String(), wantLine)
	}
	badLine := fmt.Sprintf("renamed %s to %s", widget, filepath.Join(root, "github.com", "acme", "widget"))
	if strings.Contains(stdout.String(), badLine) {
		t.Fatalf("doctor --fix stdout = %q, want it to never name the repository's own leaf path as what moved", stdout.String())
	}
}

func TestRunDoctor_FixPrintsTheSharedAncestorRenameExactlyOnce(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	// Two repositories share one mis-cased owner ancestor, and both
	// remotes agree it should read lowercase: renameCaseOnly's own call
	// for "widget" renames the shared ancestor; its call for "gadget"
	// must recognise that ancestor is already correctly spelled and
	// print nothing a second time, on every filesystem alike, including
	// a case insensitive but case preserving one (APFS, the macOS
	// default), where "os.Stat" alone cannot tell "ACME" and "acme"
	// apart.
	widget := filepath.Join(root, "github.com", "ACME", "widget")
	initRepository(t, widget)
	runGit(t, widget, "remote", "add", "origin", "https://github.com/acme/widget.git")
	gadget := filepath.Join(root, "github.com", "ACME", "gadget")
	initRepository(t, gadget)
	runGit(t, gadget, "remote", "add", "origin", "https://github.com/acme/gadget.git")

	var stdout, stderr bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdout, &stderr)

	oldAncestor := filepath.Join(root, "github.com", "ACME")
	newAncestor := filepath.Join(root, "github.com", "acme")
	wantLine := fmt.Sprintf("renamed %s to %s", oldAncestor, newAncestor)
	if got := strings.Count(stdout.String(), wantLine); got != 1 {
		t.Fatalf("doctor --fix stdout contains %q %d times, want exactly once: %q", wantLine, got, stdout.String())
	}
}

func TestRunDoctor_FixRefusesAContestedAncestorAndConverges(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	// Both repositories physically share one ancestor directory, spelled
	// "Acme" on disk. "widget"'s own remote wants it lowercase, so it is
	// mismatched; "other"'s remote wants exactly "Acme", so it already
	// matches and carries no finding of its own. Renaming the ancestor to
	// fix "widget" would immediately un-place "other".
	widget := filepath.Join(root, "github.com", "Acme", "widget")
	initRepository(t, widget)
	runGit(t, widget, "remote", "add", "origin", "https://github.com/acme/widget.git")
	other := filepath.Join(root, "github.com", "Acme", "other")
	initRepository(t, other)
	runGit(t, other, "remote", "add", "origin", "https://github.com/Acme/other.git")

	readEntries := func() []string {
		entries, err := os.ReadDir(filepath.Join(root, "github.com"))
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Join(root, "github.com"), err)
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return names
	}
	before := readEntries()

	var stdout1, stderr1 bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdout1, &stderr1)
	if strings.Contains(stdout1.String(), "renamed") {
		t.Fatalf("doctor --fix (run 1) stdout = %q, want no rename of a contested ancestor", stdout1.String())
	}
	if !strings.Contains(stdout1.String(), "ancestor-case-conflict") {
		t.Fatalf("doctor --fix (run 1) stdout = %q, want an ancestor-case-conflict finding", stdout1.String())
	}
	afterRun1 := readEntries()
	if !equalStringSets(before, afterRun1) {
		t.Fatalf("doctor --fix (run 1) changed the directory entries under github.com: before %v, after %v", before, afterRun1)
	}

	var stdout2, stderr2 bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdout2, &stderr2)
	if strings.Contains(stdout2.String(), "renamed") {
		t.Fatalf("doctor --fix (run 2) stdout = %q, want no rename of a contested ancestor", stdout2.String())
	}
	if !strings.Contains(stdout2.String(), "ancestor-case-conflict") {
		t.Fatalf("doctor --fix (run 2) stdout = %q, want the conflict reported again", stdout2.String())
	}
	afterRun2 := readEntries()
	if !equalStringSets(afterRun1, afterRun2) {
		t.Fatalf(
			"doctor --fix (run 2) is not a no-op: directory entries under github.com changed, run 1 %v, run 2 %v",
			afterRun1, afterRun2,
		)
	}
}

func TestRunDoctor_FixReportsAndFixesAnUnrelatedLeafMismatchUnderAContestedAncestor(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	// Three repositories share one ancestor directory, "Acme". "widget"
	// and "other" genuinely contest its spelling, exactly the fixture
	// TestRunDoctor_FixRefusesAContestedAncestorAndConverges already
	// uses. "Mismatched" agrees with "Acme" and carries its own,
	// unrelated leaf mismatch: the directory reads "Mismatched", its own
	// remote implies the lowercase "mismatched", a mismatch one segment
	// below the contested ancestor, not the ancestor itself.
	widget := filepath.Join(root, "github.com", "Acme", "widget")
	initRepository(t, widget)
	runGit(t, widget, "remote", "add", "origin", "https://github.com/acme/widget.git")
	other := filepath.Join(root, "github.com", "Acme", "other")
	initRepository(t, other)
	runGit(t, other, "remote", "add", "origin", "https://github.com/Acme/other.git")
	mismatched := filepath.Join(root, "github.com", "Acme", "Mismatched")
	initRepository(t, mismatched)
	runGit(t, mismatched, "remote", "add", "origin", "https://github.com/Acme/mismatched.git")
	wantMismatchedPath := filepath.Join(root, "github.com", "Acme", "mismatched")

	var stdoutBefore, stderrBefore bytes.Buffer
	code := run([]string{"doctor"}, &stdoutBefore, &stderrBefore)
	if code != 1 {
		t.Fatalf("doctor = %d, want 1, stderr: %s", code, stderrBefore.String())
	}
	before := stdoutBefore.String()
	if !strings.Contains(before, "ancestor-case-conflict") {
		t.Fatalf("doctor stdout = %q, want the ancestor conflict reported", before)
	}
	if !strings.Contains(before, "path-case-mismatch") || !strings.Contains(before, mismatched) {
		t.Fatalf("doctor stdout = %q, want the leaf mismatch of %s reported too, not swallowed by the ancestor conflict", before, mismatched)
	}

	var stdoutFix1, stderrFix1 bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdoutFix1, &stderrFix1)
	fix1 := stdoutFix1.String()
	if !strings.Contains(fix1, "ancestor-case-conflict") {
		t.Fatalf("doctor --fix stdout = %q, want the ancestor conflict still reported as outstanding", fix1)
	}
	contestedAncestor := filepath.Join(root, "github.com", "Acme")
	if strings.Contains(fix1, "renamed "+contestedAncestor+" to") {
		t.Fatalf("doctor --fix stdout = %q, want the contested ancestor %s never renamed", fix1, contestedAncestor)
	}
	wantFixLine := fmt.Sprintf("renamed %s to %s", mismatched, wantMismatchedPath)
	if !strings.Contains(fix1, wantFixLine) {
		t.Fatalf("doctor --fix stdout = %q, want it to fix the leaf mismatch: %q", fix1, wantFixLine)
	}
	if _, err := os.Stat(wantMismatchedPath); err != nil {
		t.Fatalf("doctor --fix did not rename %s to %s: %v", mismatched, wantMismatchedPath, err)
	}
	if _, err := os.Stat(widget); err != nil {
		t.Fatalf("doctor --fix moved the contested repository %s: %v", widget, err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("doctor --fix moved the contested repository %s: %v", other, err)
	}

	// Convergence: a second "--fix" run changes nothing further. The
	// leaf is already fixed, and the ancestor conflict is reported again,
	// identically, rather than flipping anything.
	var stdoutFix2, stderrFix2 bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdoutFix2, &stderrFix2)
	fix2 := stdoutFix2.String()
	if strings.Contains(fix2, "renamed") {
		t.Fatalf("doctor --fix (run 2) stdout = %q, want no further rename: the tree already converged", fix2)
	}
	if !strings.Contains(fix2, "ancestor-case-conflict") {
		t.Fatalf("doctor --fix (run 2) stdout = %q, want the ancestor conflict reported again", fix2)
	}
}

func TestRunDoctor_FixesTheSameLeafMismatchWithNoAncestorConflict(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	// The same leaf-only mismatch as above, with the contesting "widget"
	// and "other" repositories removed: no ancestor conflict exists at
	// all, so filterContestedPathCase has nothing to suppress. The leaf
	// mismatch is reported and fixed exactly as it is above, confirming
	// the ancestor conflict, not something else, was what suppressed it.
	mismatched := filepath.Join(root, "github.com", "Acme", "Mismatched")
	initRepository(t, mismatched)
	runGit(t, mismatched, "remote", "add", "origin", "https://github.com/Acme/mismatched.git")
	wantMismatchedPath := filepath.Join(root, "github.com", "Acme", "mismatched")

	var stdout, stderr bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdout, &stderr)
	out := stdout.String()
	if strings.Contains(out, "ancestor-case-conflict") {
		t.Fatalf("doctor --fix stdout = %q, want no ancestor conflict in this fixture", out)
	}
	wantFixLine := fmt.Sprintf("renamed %s to %s", mismatched, wantMismatchedPath)
	if !strings.Contains(out, wantFixLine) {
		t.Fatalf("doctor --fix stdout = %q, want it to fix the leaf mismatch: %q", out, wantFixLine)
	}
	if _, err := os.Stat(wantMismatchedPath); err != nil {
		t.Fatalf("doctor --fix did not rename %s to %s: %v", mismatched, wantMismatchedPath, err)
	}

	var stdout2, stderr2 bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdout2, &stderr2)
	if strings.Contains(stdout2.String(), "renamed") {
		t.Fatalf("doctor --fix (run 2) stdout = %q, want no further rename: the tree already converged", stdout2.String())
	}
}

// ownerEntryName reads the exact, on-disk spelling of the owner
// directory ("acme" or "Acme") under root/github.com: os.Stat alone
// cannot tell the two spellings apart on a case insensitive, case
// preserving filesystem (APFS, the macOS default), since it resolves
// both to the same entry either way. Reading the parent directory back
// reports the entry's own, exact spelling, the same technique
// TestRunDoctor_FixRenamesACaseOnlyPathMismatch already relies on.
func ownerEntryName(t *testing.T, root string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "github.com"))
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(root, "github.com"), err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory entries under %s = %v, want exactly one", filepath.Join(root, "github.com"), entries)
	}
	return entries[0].Name()
}

func TestRunDoctor_FixNeverReportsFixedOrExitsZeroForAStillMisplacedTwoSegmentMismatch(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	// Two segments differ at once: the host ("GitHub.com" instead of
	// "github.com") and the owner ("Acme" instead of "acme").
	// renameCaseOnly only ever renames the topmost differing segment per
	// call, the host here, so one "--fix" run cannot fully place this
	// repository.
	misCased := filepath.Join(root, "GitHub.com", "Acme", "widget")
	initRepository(t, misCased)
	runGit(t, misCased, "remote", "add", "origin", "https://github.com/acme/widget.git")

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--fix"}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("doctor --fix = 0, want non-zero: the repository is still misplaced after one run, stdout: %s", stdout.String())
	}

	// Only the host segment was renamed; the owner segment must still
	// read "Acme", the exact, on-disk spelling, not merely something
	// "os.Stat" resolves on a case insensitive filesystem.
	if got := ownerEntryName(t, root); got != "Acme" {
		t.Fatalf("owner directory entry = %q, want it still spelled %q after only one run", got, "Acme")
	}
	wantAfterOneRun := filepath.Join(root, "github.com", "Acme", "widget")
	if _, err := os.Stat(wantAfterOneRun); err != nil {
		t.Fatalf("doctor --fix did not rename the host segment to %s: %v", wantAfterOneRun, err)
	}

	out := stdout.String()
	if strings.Contains(out, "fixed [path-case-mismatch]") {
		t.Fatalf("doctor --fix stdout = %q, want no \"fixed [path-case-mismatch]\" line: the repository is still misplaced", out)
	}
}

func TestRunDoctor_FixConvergesOnATwoSegmentMismatchOverSeveralRuns(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	misCased := filepath.Join(root, "GitHub.com", "Acme", "widget")
	initRepository(t, misCased)
	runGit(t, misCased, "remote", "add", "origin", "https://github.com/acme/widget.git")

	var stdout1, stderr1 bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdout1, &stderr1)
	if got := ownerEntryName(t, root); got != "Acme" {
		t.Fatalf("owner directory entry after run 1 = %q, want convergence to take more than one run", got)
	}

	var stdout2, stderr2 bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdout2, &stderr2)
	if got := ownerEntryName(t, root); got != "acme" {
		t.Fatalf("owner directory entry after run 2 = %q, want %q: doctor --fix did not converge", got, "acme")
	}
	if _, err := os.Stat(filepath.Join(root, "github.com", "acme", "widget")); err != nil {
		t.Fatalf("doctor --fix (run 2) did not converge to the fully placed path: %v", err)
	}
	if !strings.Contains(stdout2.String(), "fixed [path-case-mismatch]") {
		t.Fatalf("doctor --fix (run 2) stdout = %q, want the finding reported as fixed once fully placed", stdout2.String())
	}

	var stdout3, stderr3 bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdout3, &stderr3)
	if strings.Contains(stdout3.String(), "renamed") {
		t.Fatalf("doctor --fix (run 3) stdout = %q, want no further rename: the tree already converged", stdout3.String())
	}
}

func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := map[string]int{}
	for _, v := range a {
		counts[v]++
	}
	for _, v := range b {
		counts[v]--
	}
	for _, c := range counts {
		if c != 0 {
			return false
		}
	}
	return true
}
