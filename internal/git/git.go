// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package git wraps the git CLI for the operations gitcd needs: clone, and
// reading back the remote a clone already on disk carries. It shells out
// rather than linking a git implementation, the same choice release-me
// makes for the operations the git CLI already covers well.
package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Clone runs "git clone" from remoteURL into dest, creating dest's parent
// directories first. The clone inherits the standard git output on its own
// stdout and stderr are discarded from a caller's view only on failure,
// where they are folded into the returned error.
func Clone(ctx context.Context, remoteURL, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("git: create %s: %w", filepath.Dir(dest), err)
	}

	cmd := exec.CommandContext(ctx, "git", "clone", "--", remoteURL, dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git: clone %s into %s: %w: %s", remoteURL, dest, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RemoteURL reads the "origin" remote of the repository at path, exactly as
// it is stored. It reads "remote.origin.url" through "git config" rather
// than "git remote get-url", which expands any configured
// "url.<base>.insteadOf" rewrite; gitcd compares this value against a
// remote it itself parsed, and a rewritten form, a corporate mirror for
// instance, is not something remote.Parse necessarily understands.
func RemoteURL(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "config", "--get", "remote.origin.url")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git: read the origin remote of %s: %w", path, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// IsRepository reports whether path holds a ".git" entry, a directory for
// an ordinary clone or a file for a linked worktree.
func IsRepository(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}
