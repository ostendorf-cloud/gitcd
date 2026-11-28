// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Worktree is one entry of "git worktree list --porcelain": a working tree
// git has registered for a repository, the main checkout included.
type Worktree struct {
	// Path is the working tree's absolute path, as git reports it.
	Path string
	// Head is the commit the working tree has checked out, empty for a
	// bare repository.
	Head string
	// Branch is the short name of the checked out branch, such as
	// "feat/20-menu". Empty for a detached HEAD and for a bare entry.
	Branch string
	// Detached reports a HEAD checked out at a commit rather than a branch.
	Detached bool
	// Bare reports the bare repository entry git lists first for a bare
	// repository. It has no working tree to enter.
	Bare bool
	// Locked reports a working tree "git worktree lock" protects from
	// pruning.
	Locked bool
	// Prunable reports a working tree git would remove with "git worktree
	// prune": its directory is gone or its administrative files are stale.
	Prunable bool
}

// ParseWorktrees reads the output of "git worktree list --porcelain" into
// one Worktree per record. Records are separated by a blank line, each line
// is a label and an optional value. A label this function does not know is
// skipped, so a later git version adding one never fails the parse. The
// first record is the main checkout, as git lists it.
func ParseWorktrees(out string) []Worktree {
	var worktrees []Worktree
	var current *Worktree
	flush := func() {
		if current != nil {
			worktrees = append(worktrees, *current)
			current = nil
		}
	}

	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		if line == "" {
			flush()
			continue
		}
		label, value, _ := strings.Cut(line, " ")
		if label == "worktree" {
			flush()
			current = &Worktree{Path: value}
			continue
		}
		if current == nil {
			continue
		}
		switch label {
		case "HEAD":
			current.Head = value
		case "branch":
			current.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "detached":
			current.Detached = true
		case "bare":
			current.Bare = true
		case "locked":
			current.Locked = true
		case "prunable":
			current.Prunable = true
		}
	}
	flush()
	return worktrees
}

// ListWorktrees runs "git -C path worktree list --porcelain" and parses the
// result. path may be any working tree of the repository: git answers with
// the same registry from every one of them, the main checkout first.
func ListWorktrees(ctx context.Context, path string) ([]Worktree, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git: list the worktrees of %s: %w", path, foldStderr(err))
	}
	return ParseWorktrees(string(out)), nil
}

// CheckBranchName reports whether name is a valid branch name, asking git
// itself through "git check-ref-format --branch": its rules, a leading "-"
// and the "@{-1}" shorthand among them, are not worth a second copy here.
// A name git expands to something else, such as "@{-1}", is refused too.
func CheckBranchName(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "git", "check-ref-format", "--branch", name)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git: %q is not a valid branch name: %w", name, foldStderr(err))
	}
	if strings.TrimSpace(string(out)) != name {
		return fmt.Errorf("git: %q is not a plain branch name", name)
	}
	return nil
}

// BranchExists reports whether the repository at path has a local branch
// called name.
func BranchExists(ctx context.Context, path, name string) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return cmd.Run() == nil
}

// AddWorktree runs "git worktree add" in the repository at repo, creating
// the working tree at target. With createBranch it creates branch from base
// (HEAD when base is empty), otherwise it checks out the existing branch.
// Git writes its own progress to both of its streams, so both go to
// progress, keeping the caller's stdout free for the path it prints.
func AddWorktree(ctx context.Context, repo, target, branch string, createBranch bool, base string, progress io.Writer) error {
	args := []string{"-C", repo, "worktree", "add"}
	if createBranch {
		args = append(args, "-b", branch, "--", target)
		if base != "" {
			args = append(args, base)
		}
	} else {
		args = append(args, "--", target, branch)
	}

	var captured strings.Builder
	cmd := exec.CommandContext(ctx, "git", args...)
	// One shared writer for both streams: os/exec then copies them from a
	// single goroutine, so neither progress nor captured is written
	// concurrently.
	out := io.MultiWriter(progress, &captured)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git: add the worktree %s for %s: %w: %s", target, branch, err, strings.TrimSpace(captured.String()))
	}
	return nil
}
