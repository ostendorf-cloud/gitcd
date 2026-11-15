// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package gitconfig_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
)

// TestRender_GitdirConditionMatchesARealRepository renders the include
// file for a real root, includes it from a global configuration and asks
// real git for user.email inside and outside the root. The repository
// under the root has a remote no hasconfig condition matches, so only the
// gitdir condition can select the profile. It runs on every system: the
// condition is written with forward slashes, a drive letter path included.
func TestRender_GitdirConditionMatchesARealRepository(t *testing.T) {
	home := t.TempDir()
	// git matches a gitdir condition against the repository's real path,
	// so the root is resolved through any symlink in the temporary
	// directory's own path (/tmp on macOS) and, on Windows, any short name.
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temporary directory: %v", err)
	}
	root := filepath.Join(base, "Code")
	const email = "simon@example.test"

	cfg := config.Config{
		Root:     root,
		Default:  "{host}/{owner}/{repo}",
		Profiles: map[string]config.Profile{"private": {"user.email": email}},
		Rules:    []config.Rule{{Host: "github.com", Owner: "acme", Profile: "private"}},
	}
	rendered, err := gitconfig.Render(cfg, home)
	if err != nil {
		t.Fatalf("Render() returned an unexpected error: %v", err)
	}
	for path, content := range rendered {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	globalConfig := filepath.Join(home, "global.gitconfig")
	env := append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+globalConfig,
		"GIT_CONFIG_NOSYSTEM=1",
		"HOME="+home,
		"USERPROFILE="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
	)
	git := func(dir string, args ...string) (string, int) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = dir
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		var exitErr *exec.ExitError
		switch {
		case err == nil:
			return strings.TrimSpace(string(out)), 0
		case errors.As(err, &exitErr):
			return strings.TrimSpace(string(out)), exitErr.ExitCode()
		}
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
		return "", -1
	}

	if out, code := git(home, "config", "--global", "--add", "include.path", gitconfig.IncludePath(home)); code != 0 {
		t.Fatalf("git config --global --add include.path: exit %d: %s", code, out)
	}

	inside := filepath.Join(root, "github.com", "acme", "widget")
	outside := filepath.Join(t.TempDir(), "elsewhere")
	for _, dir := range []string{inside, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
		if out, code := git(dir, "init", "--initial-branch=main"); code != 0 {
			t.Fatalf("git init in %s: exit %d: %s", dir, code, out)
		}
	}
	if out, code := git(inside, "remote", "add", "origin", "https://example.test/other/repo.git"); code != 0 {
		t.Fatalf("git remote add: exit %d: %s", code, out)
	}

	if got, code := git(inside, "config", "user.email"); code != 0 || got != email {
		t.Fatalf("user.email inside the root = %q (exit %d), want %q from the gitdir condition", got, code, email)
	}
	if got, code := git(outside, "config", "user.email"); code != 1 || got != "" {
		t.Fatalf("user.email outside the root = %q (exit %d), want no value (exit 1)", got, code)
	}
}
