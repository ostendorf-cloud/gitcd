// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// gitConfigEscaped returns path as git writes it into a configuration
// file: a backslash is doubled, which only a Windows path carries.
func gitConfigEscaped(path string) string {
	return strings.ReplaceAll(path, `\`, `\\`)
}

// samePath reports whether a and b name the same location. Each is
// resolved through its longest existing ancestor with filepath.EvalSymlinks,
// which on Windows also turns a short (8.3) name into the long one, and the
// not yet existing rest is appended unchanged.
func samePath(a, b string) bool {
	return resolveExisting(a) == resolveExisting(b)
}

func resolveExisting(path string) string {
	path = filepath.Clean(path)
	rest := ""
	for p := path; ; p = filepath.Dir(p) {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(resolved, rest)
		}
		if filepath.Dir(p) == p {
			return path
		}
		rest = filepath.Join(filepath.Base(p), rest)
	}
}

// skipWithoutUnixModes skips a test that asserts Unix permission bits or
// relies on a mode that blocks access. Windows reports only 0o666 or
// 0o444 for a file and ignores a 0o000 directory mode.
func skipWithoutUnixModes(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits: Chmod only toggles the read only attribute")
	}
}

// symlinkOrSkip creates a symbolic link at newname pointing at oldname.
// It skips the test when the system refuses, which Windows does for an
// account without the symlink privilege.
func symlinkOrSkip(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Symlink(oldname, newname); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("cannot create a symbolic link here, the account may lack the privilege: %v", err)
		}
		t.Fatalf("symlink %s -> %s: %v", newname, oldname, err)
	}
}
