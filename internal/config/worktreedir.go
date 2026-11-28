// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrInvalidWorktreeDir is wrapped into the error Load returns when
// "worktreeDir" is not a usable directory template: one that reaches
// outside the repository through a ".." segment, holds an unknown
// placeholder or a control character.
var ErrInvalidWorktreeDir = errors.New("config: invalid worktreeDir")

// ErrInvalidWorktreeBranch is returned by WorktreeTarget for a branch name
// that would place a worktree outside its directory.
var ErrInvalidWorktreeBranch = errors.New("config: invalid worktree branch")

// worktreeDirRepoPlaceholder is the one placeholder a "worktreeDir"
// template may carry: the directory name of the repository's main
// checkout, which keeps one absolute directory shared by several
// repositories apart.
const worktreeDirRepoPlaceholder = "{repo}"

// pathSegments splits p on both separators. A template or a branch name is
// written with "/", but a ".." behind a backslash is a parent segment on
// Windows, so both count on every platform.
func pathSegments(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' })
}

// ValidateWorktreeDir reports whether template can name the directory
// "gitcd worktree add" creates worktrees in. template is a path relative to
// the repository's main checkout, such as ".claude/worktrees", or an
// absolute path, with a leading "~" already expanded, that may carry the
// "{repo}" placeholder. An empty template is valid: it means the option is
// not set.
//
// What it refuses is a path that leaves the repository without saying so:
// a ".." segment in either form. An absolute path states its destination
// outright, so only a ".." segment, which would make it read as one place
// and resolve to another, is refused there.
func ValidateWorktreeDir(template string) error {
	if template == "" {
		return nil
	}
	for _, r := range template {
		if isControl(r) {
			return fmt.Errorf("%w: %q holds a control character", ErrInvalidWorktreeDir, template)
		}
	}
	rest := template
	for {
		i := strings.Index(rest, "{")
		if i < 0 {
			break
		}
		if !strings.HasPrefix(rest[i:], worktreeDirRepoPlaceholder) {
			return fmt.Errorf("%w: %q names a placeholder other than %s", ErrInvalidWorktreeDir, template, worktreeDirRepoPlaceholder)
		}
		rest = rest[i+len(worktreeDirRepoPlaceholder):]
	}
	if strings.Contains(strings.ReplaceAll(template, worktreeDirRepoPlaceholder, ""), "}") {
		return fmt.Errorf("%w: %q holds an unmatched brace", ErrInvalidWorktreeDir, template)
	}
	for _, segment := range pathSegments(template) {
		if segment == ".." {
			return fmt.Errorf("%w: %q holds a \"..\" segment, which could leave the repository", ErrInvalidWorktreeDir, template)
		}
	}
	return nil
}

// ResolveWorktreeDir returns the directory "gitcd worktree add" creates
// worktrees in for the repository whose main checkout is repoPath. A
// relative template is joined onto repoPath, an absolute one is used as it
// stands, and "{repo}" becomes the final segment of repoPath. It returns the
// empty string and no error for an empty template.
func ResolveWorktreeDir(template, repoPath string) (string, error) {
	if template == "" {
		return "", nil
	}
	if err := ValidateWorktreeDir(template); err != nil {
		return "", err
	}
	resolved := strings.ReplaceAll(template, worktreeDirRepoPlaceholder, filepath.Base(repoPath))
	if filepath.IsAbs(resolved) {
		return filepath.Clean(resolved), nil
	}
	return filepath.Join(repoPath, filepath.FromSlash(resolved)), nil
}

// WorktreeTarget returns the path of the worktree for branch inside dir: a
// branch name with a slash becomes a nested directory, so "feat/20-menu"
// gives "<dir>/feat/20-menu". It refuses a branch whose path would not stay
// inside dir: an empty name, an absolute one, or one with an empty, "." or
// ".." segment.
func WorktreeTarget(dir, branch string) (string, error) {
	if branch == "" {
		return "", fmt.Errorf("%w: the branch name is empty", ErrInvalidWorktreeBranch)
	}
	if filepath.IsAbs(branch) || strings.HasPrefix(branch, "/") || strings.HasPrefix(branch, `\`) {
		return "", fmt.Errorf("%w: %q is an absolute path", ErrInvalidWorktreeBranch, branch)
	}
	if strings.Contains(branch, `\`) || strings.Contains(branch, ":") {
		return "", fmt.Errorf("%w: %q holds a backslash or a colon, which some systems read as a path separator", ErrInvalidWorktreeBranch, branch)
	}
	for _, segment := range strings.Split(branch, "/") {
		switch segment {
		case "", ".", "..":
			return "", fmt.Errorf("%w: %q holds an empty, \".\" or \"..\" segment", ErrInvalidWorktreeBranch, branch)
		}
	}
	target := filepath.Join(dir, filepath.FromSlash(branch))
	rel, err := filepath.Rel(dir, target)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q leaves %s", ErrInvalidWorktreeBranch, branch, dir)
	}
	return target, nil
}
