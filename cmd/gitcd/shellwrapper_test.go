// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestShellWrapper_DryRunCloneDoesNotCdIntoANeverCreatedDirectory
// reproduces, through a real bash process, the exact failure reported
// against the "gitcd shell-init bash" wrapper: it keyed its "cd" only on
// "$1" being "clone", so it tried to "cd" into the path "clone --dry-run"
// printed even though a dry run deliberately never creates it. zsh shares
// bashLike's body, so this also covers it.
func TestShellWrapper_DryRunCloneDoesNotCdIntoANeverCreatedDirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the real binary and runs it under bash")
	}
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}

	home := setupEnv(t)
	bin := buildGitcd(t)

	script := `set -e
eval "$(gitcd shell-init bash)"
gitcd clone --dry-run github.com/ostendorf-cloud/release-me
`
	cmd := exec.CommandContext(context.Background(), bashPath, "-c", script)
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"PATH="+filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the bash wrapper around clone --dry-run failed: %v: %s", err, out)
	}

	want := filepath.Join(home, "Code", "github.com", "ostendorf-cloud", "release-me")
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("the bash wrapper around clone --dry-run printed %q, want %q", got, want)
	}
	if _, err := os.Stat(want); err == nil {
		t.Fatalf("clone --dry-run created %s through the wrapper, want it untouched", want)
	}
}
