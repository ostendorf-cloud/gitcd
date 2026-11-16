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

// rootSymlinkEnv points the configuration's root at a symlink to a
// directory called target, with no global identity key so a clean run exits
// 0.
func rootSymlinkEnv(t *testing.T, target string) {
	t.Helper()
	home := setupEnv(t)
	runGit(t, home, "config", "--global", "--unset-all", "user.name")
	runGit(t, home, "config", "--global", "--unset-all", "user.email")

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temporary directory: %v", err)
	}
	real := filepath.Join(base, target)
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Skipf("the file system refuses the directory name %q: %v", target, err)
	}
	link := filepath.Join(base, "Code")
	symlinkOrSkip(t, real, link)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
root: '`+link+`'
profiles:
  work:
    user.email: acme-work-identity
rules:
  - host: github.com
    profile: work
`)
}

func hasLine(out, prefix string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// TestRunDoctor_FixLeavesAnUnrenderableRootSymlinkOutstanding: no re-render
// can change what a link target is called, so --fix must not call the
// finding fixed, must exit non zero, and the next run reports it again.
func TestRunDoctor_FixLeavesAnUnrenderableRootSymlinkOutstanding(t *testing.T) {
	rootSymlinkEnv(t, `a"b`)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"apply"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apply = %d, want 0, stderr: %s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := run([]string{"doctor", "--fix"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("doctor --fix = %d, want 1, stdout: %s stderr: %s", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "fixed [root-symlink]") {
		t.Fatalf("doctor --fix stdout = %q, want no root-symlink finding reported as fixed", stdout.String())
	}
	if !hasLine(stdout.String(), "[root-symlink]") {
		t.Fatalf("doctor --fix stdout = %q, want the root-symlink finding outstanding", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"doctor"}, &stdout, &stderr); code != 1 || !hasLine(stdout.String(), "[root-symlink]") {
		t.Fatalf("second doctor = %d, stdout: %q, want 1 and the root-symlink finding again", code, stdout.String())
	}
}

// TestRunDoctor_FixRepairsAFixableRootSymlink: a missing include file with
// a symlinked root is repaired by one re-render and doctor then converges.
func TestRunDoctor_FixRepairsAFixableRootSymlink(t *testing.T) {
	rootSymlinkEnv(t, "volume")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"doctor", "--fix"}, &stdout, &stderr); code != 0 {
		t.Fatalf("doctor --fix = %d, want 0, stdout: %s stderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "fixed [root-symlink]") {
		t.Fatalf("doctor --fix stdout = %q, want the root-symlink finding reported as fixed", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"doctor"}, &stdout, &stderr); code != 0 {
		t.Fatalf("second doctor = %d, want 0 once converged, stdout: %s stderr: %s", code, stdout.String(), stderr.String())
	}
}
