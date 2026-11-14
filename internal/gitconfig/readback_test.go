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

// isolateGit points every path and lookup git would otherwise take from
// the person running the tests at a temporary directory, so a test that
// hands a file to git reads nothing of the real user's configuration and
// writes nothing into it.
func isolateGit(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// readBack returns what git itself reads back for key out of the config
// file at path, and whether git found the key at all. A file git cannot
// parse fails the test: a managed file is reached through an include in
// the global configuration, so one git refuses breaks every git command
// in every repository that include matches, which is never an acceptable
// outcome of rendering.
func readBack(t *testing.T, path, key string) (string, bool) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", "config", "--file", path, "--get", key)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return strings.TrimRight(string(out), "\n"), true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		// git's documented exit code for a key the file does not set.
		return "", false
	}
	t.Fatalf("git config --file %s --get %s: %v: %s", path, key, err, out)
	return "", false
}

// writeIfRendered writes content to a file under a temporary directory
// and returns its path. A refused render contributes an empty file, so
// the caller's assertion about what git reads back holds either way: a
// rendered file that introduces a key, and no file at all, are told
// apart by the assertion itself rather than by the test skipping it.
func writeIfRendered(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "profile.gitconfig")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestRenderProfile_GeneratedFileSetsOnlyTheProfilesOwnKeys(t *testing.T) {
	isolateGit(t)

	tests := []struct {
		name    string
		profile config.Profile
		// probe is a key the profile does not name, which git must not
		// find in the rendered file.
		probe string
	}{
		{
			name: "a key whose subsection holds a quote and a bracket",
			profile: config.Profile{
				`user.a"]` + "\n[core]\n\tpager = other-pager\n" + `[x "y.email`: "simon@example.test",
			},
			probe: "core.pager",
		},
		{
			name: "a key whose subsection holds a line break",
			profile: config.Profile{
				"user.one\ntwo.email": "simon@example.test",
			},
			probe: "core.pager",
		},
		{
			name: "a value holding a quote and a bracket",
			profile: config.Profile{
				`user.email`: `simon@example.test" ` + "\n[core]\n\tpager = touch /tmp/marker\n\tx = \"",
			},
			probe: "core.pager",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := gitconfig.RenderProfile(tt.profile, true)
			if err != nil {
				content = ""
			}
			path := writeIfRendered(t, content)
			if got, found := readBack(t, path, tt.probe); found {
				t.Fatalf("git read %s = %q out of the rendered file, want a key the profile never named to be absent", tt.probe, got)
			}
		})
	}
}

func TestRenderProfile_KeepsAValueGitCanReadBack(t *testing.T) {
	isolateGit(t)

	tests := []struct {
		name  string
		value string
	}{
		{name: "a plain value", value: "simon@example.test"},
		{name: "a value holding a quote", value: `Simon "Si" Ostendorf`},
		{name: "a value holding a backslash", value: `C:\Users\simon`},
		{name: "a value spanning two lines", value: "first\nsecond"},
		{name: "a value holding a tab", value: "left\tright"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := gitconfig.RenderProfile(config.Profile{"user.name": tt.value}, true)
			if err != nil {
				t.Fatalf("RenderProfile() returned an unexpected error: %v", err)
			}
			path := writeIfRendered(t, content)
			got, found := readBack(t, path, "user.name")
			if !found {
				t.Fatal("git did not find user.name in the rendered file, want the profile's own value")
			}
			if got != tt.value {
				t.Fatalf("git read user.name = %q, want %q", got, tt.value)
			}
			if _, found := readBack(t, path, "core.pager"); found {
				t.Fatal("git found core.pager in the rendered file, want only the keys the profile names")
			}
		})
	}
}

func TestRenderProfile_AppliesAnOptedInKeyUnchanged(t *testing.T) {
	isolateGit(t)

	// The opt in itself lives in the configuration file and is checked at
	// load time; once a key is through that gate it reaches the renderer
	// as any other profile key does, and has to render as one.
	content, err := gitconfig.RenderProfile(config.Profile{
		"user.email": "simon@example.test",
		"core.pager": "less -FRX",
	}, true)
	if err != nil {
		t.Fatalf("RenderProfile() returned an unexpected error: %v", err)
	}

	path := writeIfRendered(t, content)
	got, found := readBack(t, path, "core.pager")
	if !found {
		t.Fatal("git did not find core.pager in the rendered file, want the value the profile set")
	}
	if got != "less -FRX" {
		t.Fatalf("git read core.pager = %q, want %q", got, "less -FRX")
	}
}

func TestRender_IncludeFileRefusesARuleItCannotRenderLiterally(t *testing.T) {
	isolateGit(t)

	tests := []struct {
		name  string
		rule  config.Rule
		owner string
	}{
		{
			name: "an owner holding a quote and a bracket",
			rule: config.Rule{Host: "github.com", Owner: `acme" ] [includeIf "gitdir/i:/`, Profile: "private"},
		},
		{
			name: "an owner holding a single character wildcard",
			rule: config.Rule{Host: "github.com", Owner: "acm?", Profile: "private"},
		},
		{
			name: "an owner holding a wildcard",
			rule: config.Rule{Host: "github.com", Owner: "acme*", Profile: "private"},
		},
		{
			name: "an owner that is only a crossing wildcard",
			rule: config.Rule{Host: "github.com", Owner: "**", Profile: "private"},
		},
		{
			name: "an owner climbing out of the configured root",
			rule: config.Rule{Host: "github.com", Owner: "../..", Profile: "private"},
		},
		{
			name: "a repo holding a wildcard",
			rule: config.Rule{Host: "github.com", Owner: "acme", Repo: "*", Profile: "private"},
		},
		{
			name: "a host holding a quote",
			rule: config.Rule{Host: `github.com" ] [x "`, Owner: "acme", Profile: "private"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Root:    "/home/simon/Code",
				Default: "{host}/{owner}/{repo}",
				Profiles: map[string]config.Profile{
					"private": {"user.email": "simon@example.test"},
				},
				Rules: []config.Rule{tt.rule},
			}

			if _, err := gitconfig.Render(cfg, "/home/simon"); err == nil {
				t.Fatal("Render() rendered a rule whose fields cannot be written into a condition literally, want an error")
			}
		})
	}
}

// TestRender_ConditionsStayScopedToWhatTheRuleNames reads the rendered
// include file back with git and checks the conditions git itself finds
// in it. A rule names one host and one owner, so every condition in the
// file has to name that host, and the directory condition has to be
// rooted at the configured root. A host alias and a layout template are
// interpolated into those conditions as literally as the rule's own
// fields are, so either can widen them or move them.
func TestRender_ConditionsStayScopedToWhatTheRuleNames(t *testing.T) {
	isolateGit(t)

	const (
		home = "/home/simon"
		root = "/home/simon/Code"
	)

	tests := []struct {
		name    string
		aliases []string
		layout  string
	}{
		{name: "an alias that is only a wildcard", aliases: []string{"*"}},
		{name: "an alias holding a wildcard", aliases: []string{"github-*"}},
		{name: "an alias holding a quote and a line break", aliases: []string{"github.com\" ]\n[includeIf \"gitdir:/"}},
		{name: "a layout holding a crossing wildcard", layout: "{host}/**/{repo}"},
		{name: "a layout climbing out of the root", layout: "../../../{host}/{owner}/{repo}"},
		{name: "a layout holding a quote and a line break", layout: "{host}\" ]\n[includeIf \"gitdir:/\"]\n\tpath = /x\n[x \"/{owner}/{repo}"},
		{name: "no alias and the default layout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Root:    root,
				Default: "{host}/{owner}/{repo}",
				Hosts: map[string]config.Host{
					"github.com": {Layout: tt.layout, Aliases: tt.aliases},
				},
				Profiles: map[string]config.Profile{"private": {"user.email": "simon@example.test"}},
				Rules:    []config.Rule{{Host: "github.com", Owner: "acme", Profile: "private"}},
			}

			var content string
			if rendered, err := gitconfig.Render(cfg, home); err == nil {
				content = rendered[gitconfig.IncludePath(home)]
			}
			path := writeIfRendered(t, content)

			for _, key := range listKeys(t, path) {
				condition, _, found := strings.Cut(strings.TrimPrefix(key, "includeif."), ".path")
				if !found || !strings.HasPrefix(key, "includeif.") {
					continue
				}
				switch {
				case strings.HasPrefix(condition, "gitdir/i:"):
					if !strings.HasPrefix(strings.TrimPrefix(condition, "gitdir/i:"), root+"/") {
						t.Errorf("the include file carries the condition %q, want one rooted at %q", condition, root)
					}
				case strings.HasPrefix(condition, "hasconfig:"):
					if !strings.Contains(condition, "github.com") {
						t.Errorf("the include file carries the condition %q, want one naming the host the rule names", condition)
					}
				default:
					t.Errorf("the include file carries the condition %q, which gitcd does not render", condition)
				}
			}
		})
	}
}

// listKeys returns every key git itself reads out of the config file at
// path, failing the test when git cannot parse the file at all.
func listKeys(t *testing.T, path string) []string {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "git", "config", "--file", path, "--list", "--name-only")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git config --file %s --list: %v: %s", path, err, out)
	}

	var keys []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line != "" {
			keys = append(keys, line)
		}
	}
	return keys
}

func TestRender_PathsStayUnderTheManagedDirectory(t *testing.T) {
	home := "/home/simon"
	managed := gitconfig.Dir(home) + string(filepath.Separator)

	tests := []struct {
		name        string
		profileName string
	}{
		{name: "a name climbing above the managed directory", profileName: "../../escaped-profile"},
		{name: "a name naming a subdirectory", profileName: "nested/escaped-profile"},
		{name: "an absolute name", profileName: "/etc/escaped-profile"},
		{name: "a name that is the parent directory", profileName: ".."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Root:     "/home/simon/Code",
				Default:  "{host}/{owner}/{repo}",
				Profiles: map[string]config.Profile{tt.profileName: {"user.email": "simon@example.test"}},
				Rules:    []config.Rule{{Host: "github.com", Profile: tt.profileName}},
			}

			rendered, err := gitconfig.Render(cfg, home)
			if err != nil {
				// A refused name renders nothing, which satisfies the
				// same guarantee the paths below assert.
				return
			}
			for path := range rendered {
				if !strings.HasPrefix(path, managed) {
					t.Fatalf("Render() would write %s, outside the managed directory %s", path, managed)
				}
			}
		})
	}

	t.Run("an ordinary name renders inside it", func(t *testing.T) {
		cfg := config.Config{
			Root:     "/home/simon/Code",
			Default:  "{host}/{owner}/{repo}",
			Profiles: map[string]config.Profile{"work.eu_2": {"user.email": "simon@example.test"}},
			Rules:    []config.Rule{{Host: "github.com", Profile: "work.eu_2"}},
		}

		rendered, err := gitconfig.Render(cfg, home)
		if err != nil {
			t.Fatalf("Render() returned an unexpected error: %v", err)
		}
		if _, ok := rendered[gitconfig.ProfilePath(home, "work.eu_2")]; !ok {
			t.Fatalf("Render() = %v, want it to render the profile's own file", rendered)
		}
	})
}

func TestRender_IncludeFileNamesOnlyTheConfiguredProfilePaths(t *testing.T) {
	isolateGit(t)

	cfg := config.Config{
		Root:          "/home/simon/Code",
		Default:       "{host}/{owner}/{repo}",
		UseConfigOnly: true,
		Profiles: map[string]config.Profile{
			"private": {"user.email": "simon@example.test"},
		},
		Rules: []config.Rule{
			{Host: "github.com", Owner: "ostendorf-cloud", Profile: "private"},
		},
	}

	rendered, err := gitconfig.Render(cfg, "/home/simon")
	if err != nil {
		t.Fatalf("Render() returned an unexpected error: %v", err)
	}

	path := writeIfRendered(t, rendered[gitconfig.IncludePath("/home/simon")])

	// Every path the include file points at, whatever condition it sits
	// under, has to be a profile file gitcd itself renders, and the only
	// other key in the file is the unconditional "user.useConfigOnly". A
	// condition that did not close where it was meant to would leave
	// another key here, or make the whole file unparseable, and git reads
	// both back.
	cmd := exec.CommandContext(context.Background(), "git", "config", "--file", path, "--list")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git config --file %s --list: %v: %s", path, err, out)
	}

	wantPath := gitconfig.ProfilePath("/home/simon", "private")
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		key, value, _ := strings.Cut(line, "=")
		switch {
		case line == "":
		case key == "user.useconfigonly":
		case strings.HasSuffix(key, ".path"):
			if value != wantPath {
				t.Fatalf("the include file points %s at %q, want %q", key, value, wantPath)
			}
		default:
			t.Fatalf("git read %q out of the include file, want only the keys gitcd renders", line)
		}
	}
}
