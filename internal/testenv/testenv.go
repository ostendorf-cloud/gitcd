// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package testenv holds helpers shared by the tests of several packages.
// Only test files import it.
package testenv

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// GitValue returns the value git itself resolves for key in repo, read
// with a controlled environment: home as the home directory, globalConfig
// as the only global file, no system file and no GIT_* variable inherited
// from the caller. It returns "" when git resolves no value.
func GitValue(t testing.TB, home, globalConfig, repo, key string) string {
	t.Helper()
	var env []string
	for _, kv := range os.Environ() {
		switch {
		case strings.HasPrefix(kv, "GIT_"), strings.HasPrefix(kv, "HOME="), strings.HasPrefix(kv, "USERPROFILE="),
			strings.HasPrefix(kv, "XDG_CONFIG_HOME="), strings.HasPrefix(kv, "APPDATA="), strings.HasPrefix(kv, "LOCALAPPDATA="):
		default:
			env = append(env, kv)
		}
	}
	env = append(env,
		"HOME="+home, "USERPROFILE="+home, "APPDATA="+home, "LOCALAPPDATA="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"GIT_CONFIG_GLOBAL="+globalConfig,
		"GIT_CONFIG_NOSYSTEM=1",
	)
	cmd := exec.CommandContext(context.Background(), "git", "config", "--get", key)
	cmd.Dir = repo
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		// Exit code 1 means the key is unset, which is an answer.
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			t.Fatalf("git config --get %s: %v", key, err)
		}
		return ""
	}
	return strings.TrimSpace(string(out))
}

// FileURL returns the file URL of an absolute path. A Windows path needs a
// leading slash and forward slashes: "file://C:\x" is not a URL git reads.
func FileURL(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return "file://" + path
}

// gitIdentityVars are the variables that hand git an identity or a
// configuration value ahead of every configuration file, so a test
// pointing git at its own global file still sees the developer's shell.
//
//   - GIT_AUTHOR_NAME, GIT_AUTHOR_EMAIL, GIT_COMMITTER_NAME and
//     GIT_COMMITTER_EMAIL beat user.name and user.email, and make a commit
//     succeed where user.useConfigOnly expects a refusal.
//   - EMAIL is git's fallback for both email addresses.
//   - GIT_CONFIG_COUNT switches on GIT_CONFIG_KEY_<n> and
//     GIT_CONFIG_VALUE_<n>, which inject any key, user.name and
//     user.useConfigOnly included, above the files. Without the count git
//     reads neither, so the count alone is cleared.
//   - GIT_CONFIG_PARAMETERS carries the "-c" options of a parent git
//     process, as when the suite runs from a git hook or "rebase --exec".
//
// The dates GIT_AUTHOR_DATE and GIT_COMMITTER_DATE are left alone: they
// change a commit's timestamp, never who made it or whether it succeeds.
var gitIdentityVars = []string{
	"GIT_AUTHOR_NAME",
	"GIT_AUTHOR_EMAIL",
	"GIT_COMMITTER_NAME",
	"GIT_COMMITTER_EMAIL",
	"EMAIL",
	"GIT_CONFIG_COUNT",
	"GIT_CONFIG_PARAMETERS",
}

// ClearGitIdentity unsets every variable in gitIdentityVars for the
// duration of the test. It unsets rather than empties them: git treats an
// empty GIT_AUTHOR_NAME as set and refuses with "empty ident name not
// allowed", which is no more the unset case than the exported value is.
// testing has no Unsetenv, so t.Setenv registers the restore of the old
// value, or its absence, and os.Unsetenv then removes the variable.
// SetHome calls it, so every test that isolates its home isolates the
// identity too.
func ClearGitIdentity(t testing.TB) {
	t.Helper()
	for _, name := range gitIdentityVars {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}
}

// SetHome points every variable a Go program or git reads the home
// directory from at home, for the duration of the test. Unix reads HOME.
// Windows reads USERPROFILE, which is what os.UserHomeDir returns there,
// and APPDATA and LOCALAPPDATA for per user configuration. It also clears
// the git identity variables, see ClearGitIdentity.
func SetHome(t testing.TB, home string) {
	t.Helper()
	ClearGitIdentity(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", home)
	t.Setenv("LOCALAPPDATA", home)
}
