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

// SetHome points every variable a Go program or git reads the home
// directory from at home, for the duration of the test. Unix reads HOME.
// Windows reads USERPROFILE, which is what os.UserHomeDir returns there,
// and APPDATA and LOCALAPPDATA for per user configuration.
func SetHome(t testing.TB, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", home)
	t.Setenv("LOCALAPPDATA", home)
}
