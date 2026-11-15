// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package gitconfig_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
	"github.com/ostendorf-cloud/gitcd/internal/testenv"
)

const symlinkTestEmail = "simon@example.test"

func symlinkOrSkip(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Symlink(oldname, newname); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("cannot create a symbolic link here, the account may lack the privilege: %v", err)
		}
		t.Fatalf("symlink %s -> %s: %v", newname, oldname, err)
	}
}

// symlinkedRoot builds a root that is a symlink to a directory called
// targetName, renders the include file for it and includes that file from
// an isolated global configuration. It returns the link, the real
// directory and a function running git in a directory.
func symlinkedRoot(t *testing.T, targetName string) (linkRoot, realRoot string, git func(dir string, args ...string) (string, error)) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temporary directory: %v", err)
	}
	realRoot = filepath.Join(base, targetName)
	linkRoot = filepath.Join(base, "Code")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Skipf("the file system refuses the directory name %q: %v", targetName, err)
	}
	symlinkOrSkip(t, realRoot, linkRoot)

	home := filepath.Join(base, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("create %s: %v", home, err)
	}
	testenv.SetHome(t, home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "global.gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	cfg := config.Config{
		Root:     linkRoot,
		Default:  "{host}/{owner}/{repo}",
		Profiles: map[string]config.Profile{"private": {"user.email": symlinkTestEmail}},
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

	git = func(dir string, args ...string) (string, error) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	if out, err := git(home, "config", "--global", "--add", "include.path", gitconfig.IncludePath(home)); err != nil {
		t.Fatalf("git config --global --add include.path: %v: %s", err, out)
	}
	repo := filepath.Join(realRoot, "github.com", "acme", "widget")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("create the repository directory: %v", err)
	}
	if out, err := git(repo, "init", "--initial-branch=main"); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return linkRoot, realRoot, git
}

// TestRender_SymlinkedRootMatchesARealRepository configures a root that is a
// symlink and asks real git for user.email inside a repository under it,
// entered once through the resolved path and once through the link. git
// matches a gitdir condition against the path it sees, so each way in needs
// its own condition.
func TestRender_SymlinkedRootMatchesARealRepository(t *testing.T) {
	linkRoot, realRoot, git := symlinkedRoot(t, filepath.Join("volume", "Code"))

	tests := []struct {
		name string
		dir  string
	}{
		{name: "entered through the resolved path", dir: filepath.Join(realRoot, "github.com", "acme", "widget")},
		{name: "entered through the link", dir: filepath.Join(linkRoot, "github.com", "acme", "widget")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := git(tt.dir, "config", "user.email")
			if err != nil || got != symlinkTestEmail {
				t.Fatalf("user.email in %s = %q (%v), want %q from the gitdir condition", tt.dir, got, err, symlinkTestEmail)
			}
		})
	}
}

// TestRender_SymlinkTargetNames covers the name of the directory a root's
// symlink points at. The resolved path reaches the include file only when
// the same check the configured root passes accepts it. A target git would
// read differently, or refuse, must leave the file valid and the link's own
// condition working, and a plain one, a space or non ASCII text included,
// must match as the link does.
func TestRender_SymlinkTargetNames(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		resolvedOK bool
	}{
		{name: "a space", target: "my code", resolvedOK: true},
		{name: "non ASCII text", target: "Código-日本", resolvedOK: true},
		{name: "a double quote", target: `a"b`},
		{name: "a line break", target: "a\nb"},
		{name: "a bracket expression", target: "[l]"},
		{name: "a wildcard", target: "a*b"},
		{name: "a question mark", target: "a?b"},
		{name: "a brace", target: "a{b}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			linkRoot, realRoot, git := symlinkedRoot(t, tt.target)

			if out, err := git(linkRoot, "config", "--list"); err != nil {
				t.Fatalf("git refuses the configuration the include file makes: %v: %s", err, out)
			}
			viaLink := filepath.Join(linkRoot, "github.com", "acme", "widget")
			viaReal := filepath.Join(realRoot, "github.com", "acme", "widget")

			// On Linux and macOS a command entered through the link keeps the
			// link in its working directory, so the configured root's
			// condition matches. On Windows git matches the resolved path
			// whichever way the directory was entered (the CI run of this
			// test shows it), so a refused target leaves no condition that
			// can match at all, and the profile cannot apply there.
			wantViaLink := symlinkTestEmail
			if !tt.resolvedOK && runtime.GOOS == "windows" {
				wantViaLink = ""
			}
			wantViaReal := symlinkTestEmail
			if !tt.resolvedOK {
				wantViaReal = ""
			}
			for _, c := range []struct{ name, dir, want string }{
				{"through the link", viaLink, wantViaLink},
				{"through the resolved path", viaReal, wantViaReal},
			} {
				got, err := git(c.dir, "config", "user.email")
				if c.want == "" && err != nil && got == "" {
					continue
				}
				if err != nil || got != c.want {
					t.Errorf("user.email %s = %q (%v), want %q", c.name, got, err, c.want)
				}
			}
		})
	}
}
