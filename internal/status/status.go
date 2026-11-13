// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package status reads one repository's branch, dirty and ahead or
// behind state out of "git status --porcelain=v2 --branch". An unborn
// HEAD, left by cloning an empty repository, and a branch with no
// upstream both omit lines porcelain v2 otherwise prints; State renders
// both as an explicit, named state rather than a blank or missing field.
package status

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ostendorf-cloud/gitcd/internal/git"
)

// State is one repository's status, as of one "git status" call.
type State struct {
	// Path is the repository's working directory, as given to Inspect.
	Path string
	// Branch is the current branch name. Empty when Detached is true;
	// still set when Unborn is true, since git names the branch a first
	// commit will create even before it exists.
	Branch string
	// Detached reports a detached HEAD, checked out at a commit rather
	// than a branch.
	Detached bool
	// Unborn reports a HEAD with no commit yet, the state a freshly
	// cloned empty repository is left in.
	Unborn bool
	// Upstream is the configured upstream's name, "<remote>/<branch>",
	// or "" when the current branch has none.
	Upstream string
	// HasUpstreamComparison reports whether git could compare the local
	// branch against Upstream at all: false for an unborn HEAD even when
	// Upstream is set, since there is no local commit yet to compare.
	HasUpstreamComparison bool
	// Ahead is the number of commits the local branch carries that
	// Upstream does not. Only meaningful when HasUpstreamComparison is
	// true.
	Ahead int
	// Behind is the number of commits Upstream carries that the local
	// branch does not. Only meaningful when HasUpstreamComparison is
	// true.
	Behind int
	// Staged reports a change already added to the index.
	Staged bool
	// Unstaged reports a tracked file changed in the working tree but
	// not staged.
	Unstaged bool
	// Untracked reports a file git does not track at all.
	Untracked bool
}

// Dirty reports whether State carries any uncommitted material at all:
// a staged change, an unstaged change or an untracked file.
func (s State) Dirty() bool {
	return s.Staged || s.Unstaged || s.Untracked
}

// Inspect runs "git status" against path and parses it into a State.
func Inspect(ctx context.Context, path string) (State, error) {
	out, err := git.StatusPorcelainV2(ctx, path)
	if err != nil {
		return State{}, fmt.Errorf("status: inspect %s: %w", path, err)
	}
	return parse(path, out), nil
}

// parse reads porcelain v2 output into a State. A line this package does
// not recognise is skipped rather than failing the whole parse: a future
// git version adding a new line kind must never turn a readable status
// into a crash.
func parse(path, out string) State {
	s := State{Path: path}

	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		switch fields[0] {
		case "#":
			parseBranchLine(&s, fields[1:])
		case "1", "2", "u":
			parseChangeLine(&s, fields)
		case "?":
			s.Untracked = true
		}
	}

	return s
}

// parseBranchLine reads one "# branch.*" line's fields, the "#" already
// stripped.
func parseBranchLine(s *State, fields []string) {
	if len(fields) < 2 {
		return
	}
	switch fields[0] {
	case "branch.oid":
		s.Unborn = fields[1] == "(initial)"
	case "branch.head":
		if fields[1] == "(detached)" {
			s.Detached = true
			return
		}
		s.Branch = fields[1]
	case "branch.upstream":
		s.Upstream = fields[1]
	case "branch.ab":
		s.HasUpstreamComparison = true
		s.Ahead = parseSignedCount(fields[1])
		if len(fields) > 2 {
			s.Behind = parseSignedCount(fields[2])
		}
	}
}

// parseSignedCount reads "+3" or "-2" into its absolute value: branch.ab
// always carries the sign itself, "+" for ahead, "-" for behind, so the
// caller already knows which count it is reading.
func parseSignedCount(field string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(field, "+"))
	if err != nil {
		return 0
	}
	if n < 0 {
		return -n
	}
	return n
}

// parseChangeLine reads one ordinary ("1"), rename or copy ("2"), or
// unmerged ("u") entry: every one of those line kinds carries its two
// character XY status at the same position, fields[1].
func parseChangeLine(s *State, fields []string) {
	if len(fields) < 2 || len(fields[1]) != 2 {
		return
	}
	xy := fields[1]
	if xy[0] != '.' {
		s.Staged = true
	}
	if xy[1] != '.' {
		s.Unstaged = true
	}
}
