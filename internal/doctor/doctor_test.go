// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package doctor_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/doctor"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
)

// setupGitEnv isolates every git invocation a test makes from the real
// user's configuration and from the network, returning the home
// directory a repository and the managed directory are built under.
func setupGitEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	globalConfig := filepath.Join(home, ".gitconfig")
	// A plain token, not an address shape, so a test comparing it against
	// a profile's own identity value never accidentally collides with one
	// through some unrelated normalisation of email-shaped text.
	if err := os.WriteFile(globalConfig, []byte("[user]\n\tname = Test User\n\temail = global-fallback-identity\n"), 0o644); err != nil {
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	run(t, dir, "init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("example\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	run(t, dir, "add", "README.md")
	run(t, dir, "commit", "-m", "add README")
}

// findKind reports whether findings carries one of kind, and the first
// one found.
func findKind(findings []doctor.Finding, kind doctor.Kind) (doctor.Finding, bool) {
	for _, f := range findings {
		if f.Kind == kind {
			return f, true
		}
	}
	return doctor.Finding{}, false
}

func TestCheckRepo(t *testing.T) {
	home := setupGitEnv(t)
	root := filepath.Join(home, "code")

	t.Run("no findings for a repository whose identity and path already match the rules", func(t *testing.T) {
		repoDir := filepath.Join(root, "github.com", "acme", "clean")
		initRepository(t, repoDir)
		run(t, repoDir, "remote", "add", "origin", "https://github.com/acme/clean.git")
		run(t, repoDir, "config", "--local", "user.email", "acme-work-identity")
		run(t, repoDir, "config", "--local", "gitcd.profile", "work")

		cfg := config.Config{
			Root:    root,
			Default: "{host}/{owner}/{repo}",
			Profiles: map[string]config.Profile{
				"work": {"user.email": "acme-work-identity"},
			},
			Rules: []config.Rule{{Host: "github.com", Owner: "acme", Profile: "work"}},
		}

		findings, err := doctor.CheckRepo(context.Background(), cfg, home, "github.com/acme/clean", repoDir)
		if err != nil {
			t.Fatalf("CheckRepo() returned an unexpected error: %v", err)
		}
		if len(findings) != 0 {
			t.Fatalf("CheckRepo() = %+v, want no findings", findings)
		}
	})

	t.Run("identity drift: no pin, effective identity differs from the expected profile", func(t *testing.T) {
		repoDir := filepath.Join(root, "github.com", "acme", "drifted")
		initRepository(t, repoDir)
		run(t, repoDir, "remote", "add", "origin", "https://github.com/acme/drifted.git")
		// No local pin and no managed include: the global fallback name
		// and email set by setupGitEnv become the effective identity,
		// which does not match what the "work" profile expects.

		cfg := config.Config{
			Root:    root,
			Default: "{host}/{owner}/{repo}",
			Profiles: map[string]config.Profile{
				// Deliberately different from setupGitEnv's own global
				// "global-fallback-identity", the effective identity this
				// repository falls back to with no pin and no managed
				// include of its own.
				"work": {"user.email": "acme-work-identity"},
			},
			Rules: []config.Rule{{Host: "github.com", Owner: "acme", Profile: "work"}},
		}

		findings, err := doctor.CheckRepo(context.Background(), cfg, home, "github.com/acme/drifted", repoDir)
		if err != nil {
			t.Fatalf("CheckRepo() returned an unexpected error: %v", err)
		}
		f, ok := findKind(findings, doctor.KindIdentityDrift)
		if !ok {
			t.Fatalf("CheckRepo() = %+v, want a %s finding", findings, doctor.KindIdentityDrift)
		}
		if !strings.Contains(f.Message, "work") {
			t.Fatalf("finding message = %q, want it to name the expected profile", f.Message)
		}
	})

	t.Run("a deliberate pin naming a profile that still exists is never reported as drift", func(t *testing.T) {
		repoDir := filepath.Join(root, "github.com", "acme", "pinned")
		initRepository(t, repoDir)
		run(t, repoDir, "remote", "add", "origin", "https://github.com/acme/pinned.git")
		run(t, repoDir, "config", "--local", "user.email", "acme-personal-identity")
		run(t, repoDir, "config", "--local", "gitcd.profile", "personal")

		cfg := config.Config{
			Root:    root,
			Default: "{host}/{owner}/{repo}",
			Profiles: map[string]config.Profile{
				"work":     {"user.email": "acme-work-identity"},
				"personal": {"user.email": "acme-personal-identity"},
			},
			Rules: []config.Rule{{Host: "github.com", Owner: "acme", Profile: "work"}},
		}

		findings, err := doctor.CheckRepo(context.Background(), cfg, home, "github.com/acme/pinned", repoDir)
		if err != nil {
			t.Fatalf("CheckRepo() returned an unexpected error: %v", err)
		}
		if _, ok := findKind(findings, doctor.KindIdentityDrift); ok {
			t.Fatalf("CheckRepo() = %+v, want no %s finding for a deliberate, still valid pin", findings, doctor.KindIdentityDrift)
		}
	})

	t.Run("stale pin: gitcd.profile names a profile the configuration no longer carries", func(t *testing.T) {
		repoDir := filepath.Join(root, "github.com", "acme", "stale-pin")
		initRepository(t, repoDir)
		run(t, repoDir, "remote", "add", "origin", "https://github.com/acme/stale-pin.git")
		run(t, repoDir, "config", "--local", "gitcd.profile", "retired")

		cfg := config.Config{Root: root, Default: "{host}/{owner}/{repo}"}

		findings, err := doctor.CheckRepo(context.Background(), cfg, home, "github.com/acme/stale-pin", repoDir)
		if err != nil {
			t.Fatalf("CheckRepo() returned an unexpected error: %v", err)
		}
		f, ok := findKind(findings, doctor.KindStalePin)
		if !ok {
			t.Fatalf("CheckRepo() = %+v, want a %s finding", findings, doctor.KindStalePin)
		}
		if !strings.Contains(f.Message, "retired") {
			t.Fatalf("finding message = %q, want it to name the stale profile", f.Message)
		}
	})

	t.Run("path mismatch: repository sits at a path the layout would not render", func(t *testing.T) {
		repoDir := filepath.Join(root, "somewhere", "else")
		initRepository(t, repoDir)
		run(t, repoDir, "remote", "add", "origin", "https://github.com/acme/misplaced.git")

		cfg := config.Config{Root: root, Default: "{host}/{owner}/{repo}"}

		findings, err := doctor.CheckRepo(context.Background(), cfg, home, "somewhere/else", repoDir)
		if err != nil {
			t.Fatalf("CheckRepo() returned an unexpected error: %v", err)
		}
		f, ok := findKind(findings, doctor.KindPathMismatch)
		if !ok {
			t.Fatalf("CheckRepo() = %+v, want a %s finding", findings, doctor.KindPathMismatch)
		}
		want := filepath.Join(root, "github.com", "acme", "misplaced")
		if !strings.Contains(f.Message, want) {
			t.Fatalf("finding message = %q, want it to name the rendered path %q", f.Message, want)
		}
	})

	t.Run("path case mismatch: the layout renders the same path spelled differently", func(t *testing.T) {
		repoDir := filepath.Join(root, "github.com", "ACME", "cased")
		initRepository(t, repoDir)
		run(t, repoDir, "remote", "add", "origin", "https://github.com/acme/cased.git")

		cfg := config.Config{Root: root, Default: "{host}/{owner}/{repo}"}

		findings, err := doctor.CheckRepo(context.Background(), cfg, home, "github.com/ACME/cased", repoDir)
		if err != nil {
			t.Fatalf("CheckRepo() returned an unexpected error: %v", err)
		}
		f, ok := findKind(findings, doctor.KindPathCase)
		if !ok {
			t.Fatalf("CheckRepo() = %+v, want a %s finding", findings, doctor.KindPathCase)
		}
		if !f.Fixable {
			t.Fatal("KindPathCase finding is not marked Fixable")
		}
		want := filepath.Join(root, "github.com", "acme", "cased")
		if f.WantPath != want {
			t.Fatalf("finding WantPath = %q, want %q", f.WantPath, want)
		}
		if _, ok := findKind(findings, doctor.KindPathMismatch); ok {
			t.Fatalf("CheckRepo() = %+v, want no %s finding alongside a case only mismatch", findings, doctor.KindPathMismatch)
		}
	})

	t.Run("rule case mismatch: a rule would match case-insensitively but never applies as written", func(t *testing.T) {
		repoDir := filepath.Join(root, "github.com", "acme", "rule-case")
		initRepository(t, repoDir)
		run(t, repoDir, "remote", "add", "origin", "https://github.com/acme/rule-case.git")

		cfg := config.Config{
			Root:     root,
			Default:  "{host}/{owner}/{repo}",
			Profiles: map[string]config.Profile{"work": {"user.email": "acme-work-identity"}},
			// "Acme" differs in case from the remote's actual "acme".
			Rules: []config.Rule{{Host: "github.com", Owner: "Acme", Profile: "work"}},
		}

		findings, err := doctor.CheckRepo(context.Background(), cfg, home, "github.com/acme/rule-case", repoDir)
		if err != nil {
			t.Fatalf("CheckRepo() returned an unexpected error: %v", err)
		}
		f, ok := findKind(findings, doctor.KindRuleCaseMismatch)
		if !ok {
			t.Fatalf("CheckRepo() = %+v, want a %s finding", findings, doctor.KindRuleCaseMismatch)
		}
		if !strings.Contains(f.Message, "Acme") || !strings.Contains(f.Message, "acme") {
			t.Fatalf("finding message = %q, want it to name both the rule's and the remote's casing", f.Message)
		}
	})

	t.Run("an exactly matching rule is never reported as a case mismatch", func(t *testing.T) {
		repoDir := filepath.Join(root, "github.com", "acme", "rule-exact")
		initRepository(t, repoDir)
		run(t, repoDir, "remote", "add", "origin", "https://github.com/acme/rule-exact.git")

		cfg := config.Config{
			Root:     root,
			Default:  "{host}/{owner}/{repo}",
			Profiles: map[string]config.Profile{"work": {"user.email": "acme-work-identity"}},
			Rules:    []config.Rule{{Host: "github.com", Owner: "acme", Profile: "work"}},
		}

		findings, err := doctor.CheckRepo(context.Background(), cfg, home, "github.com/acme/rule-exact", repoDir)
		if err != nil {
			t.Fatalf("CheckRepo() returned an unexpected error: %v", err)
		}
		if _, ok := findKind(findings, doctor.KindRuleCaseMismatch); ok {
			t.Fatalf("CheckRepo() = %+v, want no %s finding for an exactly matching rule", findings, doctor.KindRuleCaseMismatch)
		}
	})
}

func TestCheckAncestorCaseConflicts(t *testing.T) {
	t.Parallel()

	t.Run("two repositories under one ancestor disagree: a conflict, naming both", func(t *testing.T) {
		t.Parallel()
		repos := []doctor.RepoPath{
			{Repo: "github.com/Acme/widget", Path: "/code/github.com/Acme/widget", Want: "/code/github.com/acme/widget"},
			{Repo: "github.com/Acme/other", Path: "/code/github.com/Acme/other", Want: "/code/github.com/Acme/other"},
		}
		findings := doctor.CheckAncestorCaseConflicts(repos)
		f, ok := findKind(findings, doctor.KindAncestorCaseConflict)
		if !ok {
			t.Fatalf("CheckAncestorCaseConflicts() = %+v, want a %s finding", findings, doctor.KindAncestorCaseConflict)
		}
		if f.Path != "/code/github.com/Acme" {
			t.Fatalf("finding Path = %q, want the shared ancestor %q", f.Path, "/code/github.com/Acme")
		}
		if !strings.Contains(f.Message, "github.com/Acme/widget") || !strings.Contains(f.Message, "github.com/Acme/other") {
			t.Fatalf("finding message = %q, want it to name both repositories", f.Message)
		}
	})

	t.Run("two repositories under one ancestor that agree: no conflict", func(t *testing.T) {
		t.Parallel()
		repos := []doctor.RepoPath{
			{Repo: "github.com/Acme/widget", Path: "/code/github.com/Acme/widget", Want: "/code/github.com/acme/widget"},
			{Repo: "github.com/Acme/gadget", Path: "/code/github.com/Acme/gadget", Want: "/code/github.com/acme/gadget"},
		}
		findings := doctor.CheckAncestorCaseConflicts(repos)
		if len(findings) != 0 {
			t.Fatalf("CheckAncestorCaseConflicts() = %+v, want no findings for an ancestor both repositories agree on", findings)
		}
	})

	t.Run("a repository with no sibling under its ancestor: no conflict", func(t *testing.T) {
		t.Parallel()
		repos := []doctor.RepoPath{
			{Repo: "github.com/Acme/widget", Path: "/code/github.com/Acme/widget", Want: "/code/github.com/acme/widget"},
		}
		findings := doctor.CheckAncestorCaseConflicts(repos)
		if len(findings) != 0 {
			t.Fatalf("CheckAncestorCaseConflicts() = %+v, want no findings with no sibling repository", findings)
		}
	})

	t.Run("a repository already at its own wanted path is never itself flagged", func(t *testing.T) {
		t.Parallel()
		repos := []doctor.RepoPath{
			{Repo: "github.com/Acme/widget", Path: "/code/github.com/Acme/widget", Want: "/code/github.com/acme/widget"},
			{Repo: "github.com/Acme/other", Path: "/code/github.com/Acme/other", Want: "/code/github.com/Acme/other"},
		}
		findings := doctor.CheckAncestorCaseConflicts(repos)
		if _, ok := findKind(findings, doctor.KindAncestorCaseConflict); !ok {
			t.Fatalf("CheckAncestorCaseConflicts() = %+v, want a conflict finding", findings)
		}
		for _, f := range findings {
			if f.Repo == "github.com/Acme/other" {
				t.Fatalf("CheckAncestorCaseConflicts() = %+v, want no finding naming the already correct repository as its own subject", findings)
			}
		}
	})
}

func TestCheckManagedFiles(t *testing.T) {
	home := setupGitEnv(t)

	cfg := config.Config{
		Root:     filepath.Join(home, "code"),
		Profiles: map[string]config.Profile{"work": {"user.email": "acme-work-identity"}},
	}

	t.Run("missing: a rendered file does not exist on disk at all", func(t *testing.T) {
		findings, err := doctor.CheckManagedFiles(cfg, home)
		if err != nil {
			t.Fatalf("CheckManagedFiles() returned an unexpected error: %v", err)
		}
		f, ok := findKind(findings, doctor.KindManagedFileMissing)
		if !ok {
			t.Fatalf("CheckManagedFiles() = %+v, want a %s finding", findings, doctor.KindManagedFileMissing)
		}
		if !f.Fixable {
			t.Fatal("KindManagedFileMissing finding is not marked Fixable")
		}
	})

	t.Run("stale: a rendered file exists but no longer matches the current configuration", func(t *testing.T) {
		managed := filepath.Join(home, ".config", "gitcd")
		if err := os.MkdirAll(filepath.Join(managed, "profiles"), 0o755); err != nil {
			t.Fatalf("create %s: %v", managed, err)
		}
		if err := os.WriteFile(filepath.Join(managed, "profiles", "work.gitconfig"), []byte("stale content\n"), 0o644); err != nil {
			t.Fatalf("write stale profile file: %v", err)
		}
		if err := os.WriteFile(filepath.Join(managed, "includes.gitconfig"), []byte("stale includes\n"), 0o644); err != nil {
			t.Fatalf("write stale include file: %v", err)
		}

		findings, err := doctor.CheckManagedFiles(cfg, home)
		if err != nil {
			t.Fatalf("CheckManagedFiles() returned an unexpected error: %v", err)
		}
		if _, ok := findKind(findings, doctor.KindManagedFileStale); !ok {
			t.Fatalf("CheckManagedFiles() = %+v, want a %s finding", findings, doctor.KindManagedFileStale)
		}
	})

	t.Run("no findings once every rendered file matches disk", func(t *testing.T) {
		rendered, err := gitconfig.Render(cfg, home)
		if err != nil {
			t.Fatalf("render the managed configuration: %v", err)
		}
		for path, content := range rendered {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("create %s: %v", filepath.Dir(path), err)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}
		}

		findings, err := doctor.CheckManagedFiles(cfg, home)
		if err != nil {
			t.Fatalf("CheckManagedFiles() returned an unexpected error: %v", err)
		}
		if len(findings) != 0 {
			t.Fatalf("CheckManagedFiles() = %+v, want no findings once disk matches the render", findings)
		}
	})
}

func TestCheckGlobal(t *testing.T) {
	t.Run("a global identity key is reported, naming the target profile", func(t *testing.T) {
		content := []byte("[user]\n\temail = [email protected]\n")
		findings, err := doctor.CheckGlobal(content, "/home/user/.gitconfig", "global", true)
		if err != nil {
			t.Fatalf("CheckGlobal() returned an unexpected error: %v", err)
		}
		f, ok := findKind(findings, doctor.KindGlobalIdentityKey)
		if !ok {
			t.Fatalf("CheckGlobal() = %+v, want a %s finding", findings, doctor.KindGlobalIdentityKey)
		}
		if !strings.Contains(f.Message, "[email protected]") || !strings.Contains(f.Message, `"global"`) {
			t.Fatalf("finding message = %q, want it to name the key's value and the target profile", f.Message)
		}
	})

	t.Run("a target profile that does not exist yet is named as a suggestion, not as fact", func(t *testing.T) {
		content := []byte("[user]\n\temail = [email protected]\n")
		findings, err := doctor.CheckGlobal(content, "/home/user/.gitconfig", "global", false)
		if err != nil {
			t.Fatalf("CheckGlobal() returned an unexpected error: %v", err)
		}
		f, ok := findKind(findings, doctor.KindGlobalIdentityKey)
		if !ok {
			t.Fatalf("CheckGlobal() = %+v, want a %s finding", findings, doctor.KindGlobalIdentityKey)
		}
		if !strings.Contains(f.Message, "does not yet define") {
			t.Fatalf("finding message = %q, want it to say the profile is not yet configured", f.Message)
		}
	})

	t.Run("instead attaches to belongs, not to the trailing subclause", func(t *testing.T) {
		content := []byte("[user]\n\temail = [email protected]\n")
		findings, err := doctor.CheckGlobal(content, "/home/user/.gitconfig", "global", false)
		if err != nil {
			t.Fatalf("CheckGlobal() returned an unexpected error: %v", err)
		}
		f, ok := findKind(findings, doctor.KindGlobalIdentityKey)
		if !ok {
			t.Fatalf("CheckGlobal() = %+v, want a %s finding", findings, doctor.KindGlobalIdentityKey)
		}
		if !strings.Contains(f.Message, "belongs instead in") {
			t.Fatalf("finding message = %q, want it to read \"belongs instead in\"", f.Message)
		}
		if strings.HasSuffix(f.Message, "define instead") {
			t.Fatalf("finding message = %q, want \"instead\" not trailing after the subclause", f.Message)
		}
	})

	t.Run("a global identity key after the include is reported as an ordering finding too", func(t *testing.T) {
		content := []byte("[include]\n\tpath = /some/include.gitconfig\n[user]\n\temail = [email protected]\n")
		findings, err := doctor.CheckGlobal(content, "/home/user/.gitconfig", "global", true)
		if err != nil {
			t.Fatalf("CheckGlobal() returned an unexpected error: %v", err)
		}
		if _, ok := findKind(findings, doctor.KindGlobalIdentityOrder); !ok {
			t.Fatalf("CheckGlobal() = %+v, want a %s finding", findings, doctor.KindGlobalIdentityOrder)
		}
	})

	t.Run("every identity key after the include is named, not only the first", func(t *testing.T) {
		content := []byte(
			"[include]\n\tpath = /some/include.gitconfig\n[user]\n\tname = Someone\n\temail = [email protected]\n",
		)
		findings, err := doctor.CheckGlobal(content, "/home/user/.gitconfig", "global", true)
		if err != nil {
			t.Fatalf("CheckGlobal() returned an unexpected error: %v", err)
		}
		f, ok := findKind(findings, doctor.KindGlobalIdentityOrder)
		if !ok {
			t.Fatalf("CheckGlobal() = %+v, want a %s finding", findings, doctor.KindGlobalIdentityOrder)
		}
		if !strings.Contains(f.Message, "user.name") || !strings.Contains(f.Message, "user.email") {
			t.Fatalf("finding message = %q, want it to name both keys sitting after the include", f.Message)
		}
	})

	t.Run("no findings for a global configuration with no identity key at all", func(t *testing.T) {
		content := []byte("[core]\n\teditor = vim\n")
		findings, err := doctor.CheckGlobal(content, "/home/user/.gitconfig", "global", true)
		if err != nil {
			t.Fatalf("CheckGlobal() returned an unexpected error: %v", err)
		}
		if len(findings) != 0 {
			t.Fatalf("CheckGlobal() = %+v, want no findings", findings)
		}
	})
}
