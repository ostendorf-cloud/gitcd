// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"context"
	"fmt"
	"os/exec"
)

// StatusPorcelainV2 runs "git -C path status --porcelain=v2 --branch" and
// returns its raw stdout, unparsed: internal/status reads the branch and
// per file lines out of it. Porcelain v2 is used, rather than the plain
// "--short" form, because it is the one format stable across git versions
// and because it reports an unborn HEAD and a branch with no upstream as
// the absence of a line, never as an error, which is exactly the shape
// internal/status needs to tell apart from a crash.
func StatusPorcelainV2(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "status", "--porcelain=v2", "--branch")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git: read the status of %s: %w", path, err)
	}
	return string(out), nil
}
