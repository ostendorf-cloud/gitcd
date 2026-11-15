// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package git_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/git"
	"github.com/ostendorf-cloud/gitcd/internal/testenv"
)

// TestShowOrigin_NonASCIIFileName reads a value out of a configuration file
// whose directory name is not ASCII. Real git prints that origin quoted and
// octal escaped, whatever the system, and ShowOrigin has to hand the plain
// path back.
func TestShowOrigin_NonASCIIFileName(t *testing.T) {
	home := t.TempDir()
	testenv.SetHome(t, home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	dir := filepath.Join(home, "Código")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	global := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(global, []byte("[user]\n\temail = me@example.test\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", global, err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)

	got, ok, err := git.ShowOrigin(context.Background(), home, "user.email")
	if err != nil || !ok {
		t.Fatalf("ShowOrigin() = ok %v, err %v, want the value", ok, err)
	}
	if got.Value != "me@example.test" || got.Origin != global {
		t.Fatalf("ShowOrigin() = %+v, want value %q and origin %q", got, "me@example.test", global)
	}
}
