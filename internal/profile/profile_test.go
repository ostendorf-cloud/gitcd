// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package profile_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/profile"
	"github.com/ostendorf-cloud/gitcd/internal/remote"
)

func TestResolve(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		DefaultProfile: "private",
		Rules: []config.Rule{
			// Deliberately listed in an order where the more specific
			// rule comes first and the less specific one last, the
			// opposite of specificity order: Resolve must still pick the
			// more specific rule for a remote both match, proving that
			// YAML list order never decides the outcome.
			{Host: "github.com", Owner: "ostendorf-cloud", Profile: "work"},
			{Host: "github.com", Profile: "general"},
		},
	}

	tests := []struct {
		name      string
		remote    remote.Remote
		wantProf  string
		wantRule  bool
		wantOwner string
	}{
		{
			name:      "the more specific host and owner rule wins over the host only rule",
			remote:    remote.Remote{Host: "github.com", Owner: "ostendorf-cloud", Repo: "gitcd"},
			wantProf:  "work",
			wantRule:  true,
			wantOwner: "ostendorf-cloud",
		},
		{
			name:     "a different owner on the same host falls back to the host only rule",
			remote:   remote.Remote{Host: "github.com", Owner: "someone-else", Repo: "widget"},
			wantProf: "general",
			wantRule: true,
		},
		{
			name:     "an unrelated host falls back to the default profile",
			remote:   remote.Remote{Host: "gitlab.com", Owner: "acme", Repo: "widget"},
			wantProf: "private",
			wantRule: false,
		},
		{
			name:     "the host match is case insensitive",
			remote:   remote.Remote{Host: "GitHub.com", Owner: "ostendorf-cloud", Repo: "gitcd"},
			wantProf: "work",
			wantRule: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := profile.Resolve(tt.remote, cfg)
			if got.Profile != tt.wantProf {
				t.Fatalf("Resolve() profile = %q, want %q", got.Profile, tt.wantProf)
			}
			if (got.Rule != nil) != tt.wantRule {
				t.Fatalf("Resolve() rule = %+v, want a matched rule: %v", got.Rule, tt.wantRule)
			}
			if tt.wantOwner != "" && (got.Rule == nil || got.Rule.Owner != tt.wantOwner) {
				t.Fatalf("Resolve() rule = %+v, want owner %q", got.Rule, tt.wantOwner)
			}
		})
	}
}

func TestResolve_NoDefaultAndNoMatch(t *testing.T) {
	t.Parallel()

	cfg := config.Config{Rules: []config.Rule{{Host: "github.com", Profile: "work"}}}
	got := profile.Resolve(remote.Remote{Host: "gitlab.com", Owner: "acme", Repo: "widget"}, cfg)
	if got.Profile != "" {
		t.Fatalf("Resolve() profile = %q, want empty when nothing matches and no default is set", got.Profile)
	}
	if got.Rule != nil {
		t.Fatalf("Resolve() rule = %+v, want nil", got.Rule)
	}
}

func TestResolve_RepoLevelRuleBeatsOwnerLevelRule(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Rules: []config.Rule{
			{Host: "github.com", Owner: "acme", Repo: "secret", Profile: "locked-down"},
			{Host: "github.com", Owner: "acme", Profile: "general"},
		},
	}

	got := profile.Resolve(remote.Remote{Host: "github.com", Owner: "acme", Repo: "secret"}, cfg)
	if got.Profile != "locked-down" {
		t.Fatalf("Resolve() profile = %q, want the repo specific rule to win", got.Profile)
	}

	got = profile.Resolve(remote.Remote{Host: "github.com", Owner: "acme", Repo: "other"}, cfg)
	if got.Profile != "general" {
		t.Fatalf("Resolve() profile = %q, want the owner level rule for a different repo", got.Profile)
	}
}

func TestSortRules(t *testing.T) {
	t.Parallel()

	rules := []config.Rule{
		{Host: "github.com", Owner: "acme", Repo: "widget", Profile: "repo-level"},
		{Host: "github.com", Profile: "host-level"},
		{Host: "github.com", Owner: "acme", Profile: "owner-level"},
	}

	got := profile.SortRules(rules)
	want := []string{"host-level", "owner-level", "repo-level"}
	if len(got) != len(want) {
		t.Fatalf("SortRules() = %+v, want %d entries", got, len(want))
	}
	for i, name := range want {
		if got[i].Profile != name {
			t.Fatalf("SortRules()[%d].Profile = %q, want %q, ascending specificity", i, got[i].Profile, name)
		}
	}
}

// setupGitEnv isolates every git invocation the test makes from the real
// user's configuration and from the network.
func setupGitEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	globalConfig := filepath.Join(home, ".gitconfig")
	if err := os.WriteFile(globalConfig, []byte("[user]\n\tname = Test User\n\temail = [email protected]\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", globalConfig, err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return home
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func initRepository(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("example\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	run(t, dir, "add", "README.md")
	run(t, dir, "commit", "-m", "add README")
}

func TestInspect(t *testing.T) {
	home := setupGitEnv(t)
	managedDir := filepath.Join(home, ".config", "gitcd")

	t.Run("resolves the remote, the expected profile and the effective identity", func(t *testing.T) {
		repoDir := filepath.Join(home, "repo")
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			t.Fatalf("create %s: %v", repoDir, err)
		}
		initRepository(t, repoDir)
		run(t, repoDir, "remote", "add", "origin", "https://github.com/ostendorf-cloud/gitcd.git")

		managedProfile := filepath.Join(managedDir, "profiles", "work.gitconfig")
		if err := os.MkdirAll(filepath.Dir(managedProfile), 0o755); err != nil {
			t.Fatalf("create %s: %v", filepath.Dir(managedProfile), err)
		}
		if err := os.WriteFile(managedProfile, []byte("[user]\n\temail = [email protected]\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", managedProfile, err)
		}
		run(t, home, "config", "--global", "--add", "include.path", managedProfile)

		cfg := config.Config{
			Rules: []config.Rule{{Host: "github.com", Owner: "ostendorf-cloud", Profile: "work"}},
		}

		repo, err := profile.Inspect(context.Background(), repoDir, cfg, managedDir)
		if err != nil {
			t.Fatalf("Inspect() returned an unexpected error: %v", err)
		}
		if repo.ExpectedProfile != "work" {
			t.Fatalf("Inspect().ExpectedProfile = %q, want %q", repo.ExpectedProfile, "work")
		}
		if repo.MatchedRule == nil || repo.MatchedRule.Owner != "ostendorf-cloud" {
			t.Fatalf("Inspect().MatchedRule = %+v, want the owner level rule", repo.MatchedRule)
		}
		if repo.Email.Value != "[email protected]" {
			t.Fatalf("Inspect().Email.Value = %q, want %q", repo.Email.Value, "[email protected]")
		}
		if repo.Email.Origin != profile.OriginManaged {
			t.Fatalf("Inspect().Email.Origin = %q, want %q", repo.Email.Origin, profile.OriginManaged)
		}
		if repo.IsWorktree {
			t.Fatal("Inspect().IsWorktree = true, want false for an ordinary clone")
		}
	})

	t.Run("a local pin takes precedence and is classified as local", func(t *testing.T) {
		repoDir := filepath.Join(home, "pinned")
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			t.Fatalf("create %s: %v", repoDir, err)
		}
		initRepository(t, repoDir)
		run(t, repoDir, "remote", "add", "origin", "https://github.com/acme/widget.git")
		run(t, repoDir, "config", "--local", "user.email", "[email protected]")

		repo, err := profile.Inspect(context.Background(), repoDir, config.Config{}, managedDir)
		if err != nil {
			t.Fatalf("Inspect() returned an unexpected error: %v", err)
		}
		if repo.Email.Origin != profile.OriginLocal {
			t.Fatalf("Inspect().Email.Origin = %q, want %q", repo.Email.Origin, profile.OriginLocal)
		}
		if repo.ExpectedProfile != "" {
			t.Fatalf("Inspect().ExpectedProfile = %q, want empty: no rule and no default are configured", repo.ExpectedProfile)
		}
	})

	t.Run("a linked worktree is reported with distinct git and common directories", func(t *testing.T) {
		repoDir := filepath.Join(home, "worktree-main")
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			t.Fatalf("create %s: %v", repoDir, err)
		}
		initRepository(t, repoDir)
		run(t, repoDir, "remote", "add", "origin", "https://github.com/acme/widget.git")
		worktreeDir := filepath.Join(home, "worktree-linked")
		run(t, repoDir, "worktree", "add", worktreeDir)

		repo, err := profile.Inspect(context.Background(), worktreeDir, config.Config{}, managedDir)
		if err != nil {
			t.Fatalf("Inspect() returned an unexpected error: %v", err)
		}
		if !repo.IsWorktree {
			t.Fatal("Inspect().IsWorktree = false, want true for a linked worktree")
		}
		if repo.GitDir == repo.CommonDir {
			t.Fatalf("Inspect().GitDir = CommonDir = %q, want them to differ for a linked worktree", repo.GitDir)
		}
	})
}
