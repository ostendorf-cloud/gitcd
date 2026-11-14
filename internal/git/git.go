// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package git wraps the git CLI for the operations gitcd needs: clone, and
// reading back the remote a clone already on disk carries. It shells out
// rather than linking a git implementation, the same choice release-me
// makes for the operations the git CLI already covers well.
package git

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Clone runs "git clone" from remoteURL into dest, creating dest's parent
// directories first. Git writes its own clone progress to stderr, which
// Clone streams to progress as the clone runs, rather than buffering it
// until the clone finishes, so a slow clone of a large repository shows
// activity instead of looking hung. Clone's own stdout stays empty, so a
// caller that prints the destination path there is never interleaved with
// git's progress output. On failure, the progress already streamed to
// progress is also folded into the returned error.
func Clone(ctx context.Context, remoteURL, dest string, progress io.Writer) error {
	return CloneWithConfig(ctx, remoteURL, dest, nil, progress)
}

// CloneWithConfig is Clone, pinning every entry of config through "git
// clone -c key=value" before the remote is even fetched: the mechanism
// "gitcd clone --profile" uses to apply an identity at clone time, rather
// than relying on the includeIf chain "gitcd apply" renders.
func CloneWithConfig(ctx context.Context, remoteURL, dest string, config map[string]string, progress io.Writer) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("git: create %s: %w", filepath.Dir(dest), err)
	}

	args := []string{"clone"}
	for _, key := range sortedConfigPairs(config) {
		args = append(args, "-c", key+"="+config[key])
	}
	args = append(args, "--", remoteURL, dest)

	var captured bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Stderr = io.MultiWriter(progress, &captured)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git: clone %s into %s: %w: %s", remoteURL, dest, err, strings.TrimSpace(captured.String()))
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

// IsBareRepository reports whether path is itself a bare repository: no
// ".git" entry of its own, path holding git's administrative files
// ("HEAD", "objects", "refs") directly, exactly what IsRepository does
// not expect. It is meant to be called only once IsRepository has
// already reported false, to tell that case apart from a path that is
// not a git repository at all: asking git to inspect a directory that
// is neither exits non-zero too, read the same as "no" here.
func IsBareRepository(ctx context.Context, path string) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--is-bare-repository")
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// Worktrees runs "git -C path worktree list --porcelain" and returns the
// absolute path of every worktree git has registered for the repository at
// path, path's own main checkout first and then every linked worktree, in
// the order git itself reports them: git's own registry is authoritative
// for this, and reading it back costs one subprocess call per repository
// rather than a directory walk that would have to look inside a path a
// repository boundary rule otherwise always skips past, such as a linked
// worktree nested inside its own repository's working tree. A caller
// refusing to move a repository that still has a linked worktree, or that
// is one itself, reads the same list to tell those apart from an ordinary
// clone with no worktree of its own.
func Worktrees(ctx context.Context, path string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git: list the worktrees of %s: %w", path, err)
	}

	var worktrees []string
	for _, line := range strings.Split(string(out), "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			worktrees = append(worktrees, p)
		}
	}
	return worktrees, nil
}
