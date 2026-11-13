// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Fetch runs "git -C path fetch" against path's own default remote,
// without a pull, a rebase or a merge: it only ever updates the
// repository's remote tracking refs. Git itself picks the remote, the
// configured upstream of the current branch, or the single remote a
// repository carries when there is no ambiguity, which for a repository
// gitcd cloned is always "origin".
func Fetch(ctx context.Context, path string) error {
	var captured bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", "-C", path, "fetch")
	cmd.Stdout = &captured
	cmd.Stderr = &captured
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git: fetch %s: %w: %s", path, err, strings.TrimSpace(captured.String()))
	}
	return nil
}
