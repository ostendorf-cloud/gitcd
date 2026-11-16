// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package testenv_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/testenv"
)

func TestGitValue(t *testing.T) {
	const (
		globalEmail   = "jane@global.example.test"
		injectedEmail = "jane@injected.example.test"
		decoyEmail    = "jane@decoy.example.test"
	)
	tests := []struct {
		name string
		// env is set in the test process before GitValue runs.
		env  map[string]string
		key  string
		want string
	}{
		{
			name: "reads the global file",
			key:  "user.email",
			want: globalEmail,
		},
		{
			name: "an unset key is an empty answer",
			key:  "user.name",
			want: "",
		},
		{
			name: "drops an inherited key and value pair",
			env: map[string]string{
				"GIT_CONFIG_COUNT":   "1",
				"GIT_CONFIG_KEY_0":   "user.email",
				"GIT_CONFIG_VALUE_0": injectedEmail,
			},
			key:  "user.email",
			want: globalEmail,
		},
		{
			name: "drops an inherited repository location",
			env:  map[string]string{"GIT_DIR": "{decoy}"},
			key:  "user.email",
			want: globalEmail,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			globalPath := filepath.Join(home, "gitconfig-global")
			if err := os.WriteFile(globalPath, []byte("[user]\n\temail = "+globalEmail+"\n"), 0o644); err != nil {
				t.Fatalf("write %s: %v", globalPath, err)
			}
			// The setup git calls below run in the test process environment,
			// so it carries a temporary home and no system file.
			testenv.SetHome(t, home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			t.Setenv("GIT_CONFIG_GLOBAL", globalPath)
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			repo := filepath.Join(home, "repo")
			decoy := filepath.Join(home, "decoy")
			for _, dir := range []string{repo, decoy} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatalf("create %s: %v", dir, err)
				}
				cmd := exec.CommandContext(context.Background(), "git", "init", "-q", dir)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git init %s: %v: %s", dir, err, out)
				}
			}
			cmd := exec.CommandContext(context.Background(), "git", "-C", decoy, "config", "user.email", decoyEmail)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git config in %s: %v: %s", decoy, err, out)
			}

			for k, v := range tt.env {
				if v == "{decoy}" {
					v = filepath.Join(decoy, ".git")
				}
				t.Setenv(k, v)
			}

			if got := testenv.GitValue(t, home, globalPath, repo, tt.key); got != tt.want {
				t.Fatalf("GitValue(%s) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

// identityVars is every variable SetHome has to unset. The list is spelled
// out here, not read from the package, so a variable dropped from the
// package fails this test.
var identityVars = []string{
	"GIT_AUTHOR_NAME",
	"GIT_AUTHOR_EMAIL",
	"GIT_COMMITTER_NAME",
	"GIT_COMMITTER_EMAIL",
	"EMAIL",
	"GIT_CONFIG_COUNT",
	"GIT_CONFIG_PARAMETERS",
}

// TestSetHome_UnsetsTheGitIdentityVariablesAndRestoresThem exports every
// identity variable, calls SetHome in a subtest and asserts the variables
// are not just empty but absent, and back with their value afterwards.
func TestSetHome_UnsetsTheGitIdentityVariablesAndRestoresThem(t *testing.T) {
	for _, name := range identityVars {
		t.Setenv(name, "exported-by-the-shell")
	}
	// One variable that was never exported must stay absent afterwards.
	const absent = "GIT_CONFIG_PARAMETERS"
	t.Setenv(absent, "")
	if err := os.Unsetenv(absent); err != nil {
		t.Fatalf("unset %s: %v", absent, err)
	}

	t.Run("inside the test", func(t *testing.T) {
		testenv.SetHome(t, t.TempDir())
		for _, name := range identityVars {
			if value, ok := os.LookupEnv(name); ok {
				t.Errorf("%s = %q after SetHome, want it unset", name, value)
			}
		}
	})

	for _, name := range identityVars {
		value, ok := os.LookupEnv(name)
		switch {
		case name == absent && ok:
			t.Errorf("%s = %q after the test, want it still unset", name, value)
		case name != absent && value != "exported-by-the-shell":
			t.Errorf("%s = %q (set %v) after the test, want the exported value back", name, value, ok)
		}
	}
}
