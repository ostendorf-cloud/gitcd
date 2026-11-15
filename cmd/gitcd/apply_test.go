// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"errors"
	"fmt"
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

// TestRunApply_WarnsForAnUnrenderableSymlinkTarget: a root linked to a
// directory the root check refuses still applies, with the configured
// root's condition only and a warning on stderr.
func TestRunApply_WarnsForAnUnrenderableSymlinkTarget(t *testing.T) {
	home := setupEnv(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temporary directory: %v", err)
	}
	target := filepath.Join(base, "a\"b\nc")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Skipf("the file system refuses the directory name: %v", err)
	}
	link := filepath.Join(base, "Code")
	symlinkOrSkip(t, target, link)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
root: '`+link+`'
profiles:
  private:
    user.email: simon@example.test

rules:
  - host: github.com
    profile: private
`)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"apply"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apply = %d, want 0, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "warning") || !strings.Contains(stderr.String(), fmt.Sprintf("%q", target)) {
		t.Fatalf("apply stderr = %q, want a warning naming %q", stderr.String(), target)
	}
	if strings.Count(stderr.String(), "\n") != 1 {
		t.Fatalf("apply stderr = %q, want the warning on one line", stderr.String())
	}
	content, err := os.ReadFile(filepath.Join(home, ".config", "gitcd", "includes.gitconfig"))
	if err != nil {
		t.Fatalf("read the include file: %v", err)
	}
	if strings.Contains(string(content), `a"b`) || strings.Contains(string(content), "a\\\"b") {
		t.Fatalf("include file = %q, want no condition for the refused target", content)
	}
}

// assertManagedModes checks the mode of the managed directory, its profiles
// directory and every file gitcd wrote under it.
func assertManagedModes(t *testing.T, home string) {
	t.Helper()
	managedDir := filepath.Join(home, ".config", "gitcd")
	want := map[string]os.FileMode{
		managedDir:                                           0o700,
		filepath.Join(managedDir, "profiles"):                0o700,
		filepath.Join(managedDir, "includes.gitconfig"):      0o600,
		filepath.Join(managedDir, "profiles", "a.gitconfig"): 0o600,
		filepath.Join(managedDir, "profiles", "b.gitconfig"): 0o600,
	}
	for path := range want {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
	}
	t.Run("modes", func(t *testing.T) {
		skipWithoutUnixModes(t)
		for path, mode := range want {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat %s: %v", path, err)
			}
			if got := info.Mode().Perm(); got != mode {
				t.Errorf("mode of %s = %o, want %o", path, got, mode)
			}
		}
	})
}

const modesConfig = `
profiles:
  a:
    user.email: a@example.test
  b:
    user.email: b@example.test
defaultProfile: a
`

func TestRunApply_CreatesTheManagedFilesPrivate(t *testing.T) {
	home := setupEnv(t)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), modesConfig)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"apply"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apply = %d, want 0, stderr: %s", code, stderr.String())
	}

	assertManagedModes(t, home)
}

func TestRunApply_TightensFilesAnEarlierVersionWroteWide(t *testing.T) {
	home := setupEnv(t)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), modesConfig)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"apply"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apply = %d, want 0, stderr: %s", code, stderr.String())
	}

	managedDir := filepath.Join(home, ".config", "gitcd")
	for path, mode := range map[string]os.FileMode{
		managedDir:                                           0o755,
		filepath.Join(managedDir, "profiles"):                0o755,
		filepath.Join(managedDir, "includes.gitconfig"):      0o644,
		filepath.Join(managedDir, "profiles", "a.gitconfig"): 0o644,
		filepath.Join(managedDir, "profiles", "b.gitconfig"): 0o644,
	} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatalf("chmod %s: %v", path, err)
		}
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"apply"}, &stdout, &stderr); code != 0 {
		t.Fatalf("second apply = %d, want 0, stderr: %s", code, stderr.String())
	}

	assertManagedModes(t, home)
}

func TestWritePrivateFile_RefusesASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	writeFile(t, target, "keep")
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	link := filepath.Join(dir, "link")
	symlinkOrSkip(t, target, link)
	dangling := filepath.Join(dir, "dangling")
	symlinkOrSkip(t, filepath.Join(dir, "missing"), dangling)

	tests := []struct {
		name string
		path string
	}{
		{name: "link to a file", path: link},
		{name: "dangling link", path: dangling},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := writePrivateFile(tt.path, []byte("new"))
			if !errors.Is(err, errSymlinkAtManagedPath) {
				t.Fatalf("writePrivateFile(%s) error = %v, want errSymlinkAtManagedPath", tt.path, err)
			}
		})
	}

	if got, _ := os.ReadFile(target); string(got) != "keep" {
		t.Fatalf("target = %q, want it untouched", got)
	}
	if _, err := os.Lstat(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("the dangling link's target was created")
	}
	t.Run("target mode", func(t *testing.T) {
		skipWithoutUnixModes(t)
		info, err := os.Stat(target)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Fatalf("target mode = %o, want 644 unchanged", got)
		}
	})
}

func TestRunApply_RefusesASymlinkAtAManagedPath(t *testing.T) {
	home := setupEnv(t)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), modesConfig)
	managedDir := filepath.Join(home, ".config", "gitcd")
	if err := os.MkdirAll(managedDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	target := filepath.Join(home, "elsewhere.gitconfig")
	writeFile(t, target, "keep")
	symlinkOrSkip(t, target, filepath.Join(managedDir, "includes.gitconfig"))

	var stdout, stderr bytes.Buffer
	if code := run([]string{"apply"}, &stdout, &stderr); code == 0 {
		t.Fatal("apply = 0, want a failure for a symlink at a managed path")
	}
	if got, _ := os.ReadFile(target); string(got) != "keep" {
		t.Fatalf("symlink target = %q, want it untouched", got)
	}
}

func TestRunInit_CreatesTheManagedDirectoryPrivate(t *testing.T) {
	home := setupEnv(t)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"init"}, &stdout, &stderr); code != 0 {
		t.Fatalf("init = %d, want 0, stderr: %s", code, stderr.String())
	}

	managedDir := filepath.Join(home, ".config", "gitcd")
	info, err := os.Stat(managedDir)
	if err != nil {
		t.Fatalf("stat %s: %v", managedDir, err)
	}
	t.Run("mode", func(t *testing.T) {
		skipWithoutUnixModes(t)
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("mode of %s = %o, want 700", managedDir, got)
		}
	})
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
