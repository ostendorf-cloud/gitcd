// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package git_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/git"
)

// gitOutput runs git with args in dir and returns its trimmed stdout,
// failing the test on a non-zero exit.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

func TestGlobalConfigPath(t *testing.T) {
	t.Run("GIT_CONFIG_GLOBAL wins when set", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		explicit := filepath.Join(home, "explicit.gitconfig")
		t.Setenv("GIT_CONFIG_GLOBAL", explicit)

		got, err := git.GlobalConfigPath()
		if err != nil {
			t.Fatalf("GlobalConfigPath() returned an unexpected error: %v", err)
		}
		if got != explicit {
			t.Fatalf("GlobalConfigPath() = %q, want %q", got, explicit)
		}
	})

	t.Run("falls back to the home gitconfig when it exists", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("GIT_CONFIG_GLOBAL", "")
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		primary := filepath.Join(home, ".gitconfig")
		if err := os.WriteFile(primary, nil, 0o644); err != nil {
			t.Fatalf("write %s: %v", primary, err)
		}

		got, err := git.GlobalConfigPath()
		if err != nil {
			t.Fatalf("GlobalConfigPath() returned an unexpected error: %v", err)
		}
		if got != primary {
			t.Fatalf("GlobalConfigPath() = %q, want %q", got, primary)
		}
	})

	t.Run("falls back to the xdg path when only it exists", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("GIT_CONFIG_GLOBAL", "")
		xdg := filepath.Join(home, ".config")
		t.Setenv("XDG_CONFIG_HOME", xdg)
		fallback := filepath.Join(xdg, "git", "config")
		if err := os.MkdirAll(filepath.Dir(fallback), 0o755); err != nil {
			t.Fatalf("create %s: %v", filepath.Dir(fallback), err)
		}
		if err := os.WriteFile(fallback, nil, 0o644); err != nil {
			t.Fatalf("write %s: %v", fallback, err)
		}

		got, err := git.GlobalConfigPath()
		if err != nil {
			t.Fatalf("GlobalConfigPath() returned an unexpected error: %v", err)
		}
		if got != fallback {
			t.Fatalf("GlobalConfigPath() = %q, want %q", got, fallback)
		}
	})

	t.Run("defaults to the home gitconfig when neither exists", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("GIT_CONFIG_GLOBAL", "")
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

		got, err := git.GlobalConfigPath()
		if err != nil {
			t.Fatalf("GlobalConfigPath() returned an unexpected error: %v", err)
		}
		if want := filepath.Join(home, ".gitconfig"); got != want {
			t.Fatalf("GlobalConfigPath() = %q, want %q", got, want)
		}
	})
}

func TestConfigAddGlobal(t *testing.T) {
	setupGitEnv(t)

	if err := git.ConfigAddGlobal(context.Background(), "include.path", "/tmp/example.gitconfig"); err != nil {
		t.Fatalf("ConfigAddGlobal() returned an unexpected error: %v", err)
	}

	got := gitOutput(t, t.TempDir(), "config", "--global", "--get-all", "include.path")
	if got != "/tmp/example.gitconfig" {
		t.Fatalf("include.path = %q, want %q", got, "/tmp/example.gitconfig")
	}
}

func TestLocalConfig(t *testing.T) {
	setupGitEnv(t)

	t.Run("set then get round trips a value", func(t *testing.T) {
		root := t.TempDir()
		initRepository(t, root)

		if err := git.ConfigSetLocal(context.Background(), root, "gitcd.profile", "private"); err != nil {
			t.Fatalf("ConfigSetLocal() returned an unexpected error: %v", err)
		}
		got, err := git.ConfigGetLocal(context.Background(), root, "gitcd.profile")
		if err != nil {
			t.Fatalf("ConfigGetLocal() returned an unexpected error: %v", err)
		}
		if got != "private" {
			t.Fatalf("ConfigGetLocal() = %q, want %q", got, "private")
		}
	})

	t.Run("get an unset key wraps ErrConfigKeyNotSet", func(t *testing.T) {
		root := t.TempDir()
		initRepository(t, root)

		_, err := git.ConfigGetLocal(context.Background(), root, "gitcd.profile")
		if !errors.Is(err, git.ErrConfigKeyNotSet) {
			t.Fatalf("ConfigGetLocal() error = %v, want it to wrap ErrConfigKeyNotSet", err)
		}
	})

	t.Run("unset removes a value that was set", func(t *testing.T) {
		root := t.TempDir()
		initRepository(t, root)
		run(t, root, "config", "--local", "user.signingkey", "ssh-ed25519 AAAA")

		if err := git.ConfigUnsetLocal(context.Background(), root, "user.signingkey"); err != nil {
			t.Fatalf("ConfigUnsetLocal() returned an unexpected error: %v", err)
		}
		if _, err := git.ConfigGetLocal(context.Background(), root, "user.signingkey"); !errors.Is(err, git.ErrConfigKeyNotSet) {
			t.Fatalf("ConfigGetLocal() after unset error = %v, want ErrConfigKeyNotSet", err)
		}
	})

	t.Run("unset a key that is not set wraps ErrConfigKeyNotSet instead of failing", func(t *testing.T) {
		root := t.TempDir()
		initRepository(t, root)

		err := git.ConfigUnsetLocal(context.Background(), root, "gitcd.profile")
		if !errors.Is(err, git.ErrConfigKeyNotSet) {
			t.Fatalf("ConfigUnsetLocal() error = %v, want it to wrap ErrConfigKeyNotSet", err)
		}
	})

	t.Run("set never writes dot git slash config by path inside a linked worktree", func(t *testing.T) {
		root := t.TempDir()
		initRepository(t, root)
		worktree := filepath.Join(root, "..", "worktree")
		run(t, root, "worktree", "add", worktree)

		if err := git.ConfigSetLocal(context.Background(), worktree, "gitcd.profile", "private"); err != nil {
			t.Fatalf("ConfigSetLocal() returned an unexpected error: %v", err)
		}

		// The value must be readable back from the worktree, and from the
		// main checkout too, since local configuration inside a linked
		// worktree is shared through the repository's common directory,
		// not ".git/config" relative to the worktree itself.
		got, err := git.ConfigGetLocal(context.Background(), root, "gitcd.profile")
		if err != nil {
			t.Fatalf("ConfigGetLocal(root) returned an unexpected error: %v", err)
		}
		if got != "private" {
			t.Fatalf("ConfigGetLocal(root) = %q, want %q, the write made from the worktree to be visible from the main checkout", got, "private")
		}
	})
}

func TestShowOrigin(t *testing.T) {
	setupGitEnv(t)

	t.Run("reports the value and its origin file", func(t *testing.T) {
		root := t.TempDir()
		initRepository(t, root)
		run(t, root, "config", "--local", "user.email", "[email protected]")

		value, ok, err := git.ShowOrigin(context.Background(), root, "user.email")
		if err != nil {
			t.Fatalf("ShowOrigin() returned an unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("ShowOrigin() ok = false, want true for a key that is set")
		}
		if value.Value != "[email protected]" {
			t.Fatalf("ShowOrigin() value = %q, want %q", value.Value, "[email protected]")
		}
		if value.Origin == "" {
			t.Fatal("ShowOrigin() origin is empty, want the local config file's path")
		}
	})

	t.Run("an unset key reports ok false with no error", func(t *testing.T) {
		root := t.TempDir()
		initRepository(t, root)

		value, ok, err := git.ShowOrigin(context.Background(), root, "user.signingkey")
		if err != nil {
			t.Fatalf("ShowOrigin() returned an unexpected error: %v", err)
		}
		if ok {
			t.Fatalf("ShowOrigin() ok = true, want false for an unset key, got %+v", value)
		}
	})
}

func TestDirAndCommonDir(t *testing.T) {
	setupGitEnv(t)

	t.Run("an ordinary clone has equal git and common directories", func(t *testing.T) {
		root := t.TempDir()
		initRepository(t, root)

		dir, err := git.Dir(context.Background(), root)
		if err != nil {
			t.Fatalf("Dir() returned an unexpected error: %v", err)
		}
		common, err := git.CommonDir(context.Background(), root)
		if err != nil {
			t.Fatalf("CommonDir() returned an unexpected error: %v", err)
		}
		if dir != common {
			t.Fatalf("Dir() = %q, CommonDir() = %q, want them equal for an ordinary clone", dir, common)
		}
	})

	t.Run("a linked worktree has a different git directory than its common directory", func(t *testing.T) {
		root := t.TempDir()
		initRepository(t, root)
		worktree := filepath.Join(root, "..", "worktree")
		run(t, root, "worktree", "add", worktree)

		dir, err := git.Dir(context.Background(), worktree)
		if err != nil {
			t.Fatalf("Dir() returned an unexpected error: %v", err)
		}
		common, err := git.CommonDir(context.Background(), worktree)
		if err != nil {
			t.Fatalf("CommonDir() returned an unexpected error: %v", err)
		}
		if dir == common {
			t.Fatalf("Dir() = CommonDir() = %q, want a linked worktree's own git directory to differ from the shared common directory", dir)
		}
	})
}

func TestIdentityKeyAfterInclude(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		wantKey   string
		wantFound bool
	}{
		{
			name:      "no include section at all",
			content:   "[user]\n\tname = Test User\n\temail = [email protected]\n",
			wantFound: false,
		},
		{
			name: "identity keys only before the include, the documented safe case",
			content: "[user]\n\tname = Test User\n\temail = [email protected]\n" +
				"[include]\n\tpath = /some/other/include.gitconfig\n",
			wantFound: false,
		},
		{
			name: "an identity key after the last bare include section",
			content: "[include]\n\tpath = /some/other/include.gitconfig\n" +
				"[user]\n\tsigningkey = ssh-ed25519 AAAATRAILING\n",
			wantKey:   "user.signingkey",
			wantFound: true,
		},
		{
			name: "an includeIf section after the include does not itself count",
			content: "[include]\n\tpath = /some/other/include.gitconfig\n" +
				"[includeIf \"gitdir:/some/path/\"]\n\tpath = /some/conditional.gitconfig\n",
			wantFound: false,
		},
		{
			name: "an identity key between two bare include sections is safe, only the last section matters",
			content: "[include]\n\tpath = /first.gitconfig\n" +
				"[user]\n\tname = In Between\n" +
				"[include]\n\tpath = /second.gitconfig\n",
			wantFound: false,
		},
		{
			name: "an identity key after the second of two bare include sections is still caught",
			content: "[include]\n\tpath = /first.gitconfig\n" +
				"[include]\n\tpath = /second.gitconfig\n" +
				"[user]\n\tname = After Both\n",
			wantFound: true,
			wantKey:   "user.name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			key, found, err := git.IdentityKeyAfterInclude([]byte(tt.content), git.IdentityKeys)
			if err != nil {
				t.Fatalf("IdentityKeyAfterInclude() returned an unexpected error: %v", err)
			}
			if found != tt.wantFound {
				t.Fatalf("IdentityKeyAfterInclude() found = %v, want %v (key %q)", found, tt.wantFound, key)
			}
			if tt.wantFound && key != tt.wantKey {
				t.Fatalf("IdentityKeyAfterInclude() key = %q, want %q", key, tt.wantKey)
			}
		})
	}
}

// Note: the two "two bare include sections" cases above exercise exactly
// why only the LAST bare include section matters: "git config --global
// --add" always inserts a new key into that last matching section, so an
// identity key sitting between two include sections ends up before the
// insertion point, safe, while one sitting after both does not.

func TestFindGlobalIdentityValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    []git.GlobalIdentityValue
	}{
		{
			name:    "no identity key at all",
			content: "[core]\n\teditor = vim\n",
			want:    nil,
		},
		{
			name:    "a key before any include, the position IdentityKeyAfterInclude calls safe",
			content: "[user]\n\tname = Test User\n\temail = test@example.test\n" + "[include]\n\tpath = /other.gitconfig\n",
			want: []git.GlobalIdentityValue{
				{Key: "user.name", Value: "Test User"},
				{Key: "user.email", Value: "test@example.test"},
			},
		},
		{
			name:    "a key after an include is found too, same as before it",
			content: "[include]\n\tpath = /other.gitconfig\n" + "[user]\n\tsigningkey = ssh-ed25519 AAAA\n",
			want: []git.GlobalIdentityValue{
				{Key: "user.signingkey", Value: "ssh-ed25519 AAAA"},
			},
		},
		{
			name:    "a quoted value is unquoted, escaped characters undone",
			content: "[user]\n\tname = \"Jane \\\"The Dev\\\" Doe\"\n",
			want: []git.GlobalIdentityValue{
				{Key: "user.name", Value: `Jane "The Dev" Doe`},
			},
		},
		{
			name:    "a bare boolean key with no equals sign carries true",
			content: "[commit]\n\tgpgsign\n",
			want: []git.GlobalIdentityValue{
				{Key: "commit.gpgsign", Value: "true"},
			},
		},
		{
			name:    "a key under an unrelated section is ignored",
			content: "[alias]\n\tname = \"Test\"\n",
			want:    nil,
		},
		{
			name:    "an includeIf section is scanned too, not only a bare include",
			content: "[includeIf \"gitdir:/some/path/\"]\n\tsigningkey = ssh-ed25519 AAAA\n",
			want:    nil,
		},
		{
			name:    "the last value wins when a key is set more than once",
			content: "[user]\n\temail = first@example.test\n[user]\n\temail = second@example.test\n",
			want: []git.GlobalIdentityValue{
				{Key: "user.email", Value: "second@example.test"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := git.FindGlobalIdentityValues([]byte(tt.content), git.IdentityKeys)
			if len(got) != len(tt.want) {
				t.Fatalf("FindGlobalIdentityValues() = %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("FindGlobalIdentityValues()[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestFindGlobalIdentityValues_IncludeIfSectionNameRequiresNoSubsection
// documents a deliberate narrowing shared with IdentityKeyAfterInclude:
// "[includeIf \"...\"]" carries a subsection, the condition itself, so it
// is never mistaken for the section "includeif" when looking for an
// identity key, but it is also never itself scanned as a section named
// after its condition; the key lines under it belong to the subsectioned
// header's own section name, "includeif", which "user.name" and friends
// never collide with. This test exists so a future change to how
// sections are tracked has something to catch a regression against.
func TestFindGlobalIdentityValues_IncludeIfSectionNameRequiresNoSubsection(t *testing.T) {
	t.Parallel()
	content := "[includeIf \"gitdir:/some/path/\"]\n\tpath = /conditional.gitconfig\n[user]\n\temail = after@example.test\n"
	got := git.FindGlobalIdentityValues([]byte(content), git.IdentityKeys)
	want := []git.GlobalIdentityValue{{Key: "user.email", Value: "after@example.test"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("FindGlobalIdentityValues() = %+v, want %+v", got, want)
	}
}

func TestConfigUnsetGlobal(t *testing.T) {
	setupGitEnv(t)

	t.Run("removes a value that was set", func(t *testing.T) {
		if err := git.ConfigAddGlobal(context.Background(), "user.signingkey", "ssh-ed25519 AAAA"); err != nil {
			t.Fatalf("ConfigAddGlobal() returned an unexpected error: %v", err)
		}

		if err := git.ConfigUnsetGlobal(context.Background(), "user.signingkey"); err != nil {
			t.Fatalf("ConfigUnsetGlobal() returned an unexpected error: %v", err)
		}

		// A second unset of the same key now wraps ErrConfigKeyNotSet,
		// which only happens once git itself reports no value left: the
		// behavioural proof that the first call actually removed it.
		if err := git.ConfigUnsetGlobal(context.Background(), "user.signingkey"); !errors.Is(err, git.ErrConfigKeyNotSet) {
			t.Fatalf("ConfigUnsetGlobal() after removal, error = %v, want it to wrap ErrConfigKeyNotSet", err)
		}
	})

	t.Run("unset a key that is not set wraps ErrConfigKeyNotSet instead of failing", func(t *testing.T) {
		err := git.ConfigUnsetGlobal(context.Background(), "user.doesnotexist")
		if !errors.Is(err, git.ErrConfigKeyNotSet) {
			t.Fatalf("ConfigUnsetGlobal() error = %v, want it to wrap ErrConfigKeyNotSet", err)
		}
	})
}
