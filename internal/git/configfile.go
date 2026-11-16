// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ostendorf-cloud/gitcd/internal/remote"
)

// ConfigUnsetFile runs "git config --file path --unset-all key", removing
// every value key carries in the file at path. It is the same edit
// ConfigUnsetGlobal makes to the global file, aimed at a file of the
// caller's choice, so a caller can compute what the global file would look
// like after an unset without touching it. It wraps ErrConfigKeyNotSet when
// key carries no value in that file.
func ConfigUnsetFile(ctx context.Context, path, key string) error {
	cmd := exec.CommandContext(ctx, "git", "config", "--file", path, "--unset-all", key)
	if out, err := cmd.CombinedOutput(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 5 {
			return fmt.Errorf("git: unset %s in %s: %w", key, path, ErrConfigKeyNotSet)
		}
		return fmt.Errorf("git: unset %s in %s: %w: %s", key, path, err, remote.Redact(strings.TrimSpace(string(out))))
	}
	return nil
}
