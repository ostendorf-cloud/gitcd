// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package gitconfig_test

import (
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
)

var update = flag.Bool("update", false, "refresh the golden files instead of comparing against them")

func testConfig() config.Config {
	return config.Config{
		Root:          "/home/simon/Code",
		Default:       "{host}/{owner}/{repo}",
		UseConfigOnly: true,
		Hosts: map[string]config.Host{
			"dev.azure.com": {Layout: "{host}/{org}/{project}/{repo}"},
		},
		Profiles: map[string]config.Profile{
			"private": {
				"user.name":       "Simon Ostendorf",
				"user.email":      "simon@example.test",
				"user.signingkey": "ssh-ed25519 AAAAEXAMPLE",
				"gpg.format":      "ssh",
				"commit.gpgsign":  "true",
			},
			"ado": {
				"user.name":  "Simon Ostendorf",
				"user.email": "simon@work.example.test",
			},
			"locked-down": {
				"user.name":  "Simon Ostendorf",
				"user.email": "simon@locked.example.test",
			},
		},
		Rules: []config.Rule{
			// Deliberately listed least specific first, most specific
			// last, the opposite of emit order, so the golden file also
			// proves emit order does not follow configuration order.
			{Host: "github.com", Profile: "private"},
			{Host: "github.com", Owner: "ostendorf-cloud", Repo: "secrets", Profile: "locked-down"},
			{Host: "dev.azure.com", Profile: "ado"},
		},
		DefaultProfile: "private",
	}
}

func TestRender(t *testing.T) {
	cfg := testConfig()
	home := "/home/simon"

	rendered, err := gitconfig.Render(cfg, home)
	if err != nil {
		t.Fatalf("Render() returned an unexpected error: %v", err)
	}

	paths := make([]string, 0, len(rendered))
	for path := range rendered {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var got strings.Builder
	for _, path := range paths {
		got.WriteString("=== ")
		got.WriteString(path)
		got.WriteString(" ===\n")
		got.WriteString(rendered[path])
	}

	goldenPath := filepath.Join("testdata", "render.golden")
	if *update {
		if err := os.WriteFile(goldenPath, []byte(got.String()), 0o644); err != nil {
			t.Fatalf("write golden file %s: %v", goldenPath, err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file %s: %v", goldenPath, err)
	}
	if got.String() != string(want) {
		t.Fatalf("Render() does not match %s, run the test with -update to refresh it:\n%s", goldenPath, got.String())
	}
}

func TestRender_SpecificityOrder(t *testing.T) {
	// Acceptance: a general rule listed last in the configuration file
	// must not override a more specific one listed earlier. The config
	// under test lists the host only rule for github.com first and the
	// host, owner and repo rule last; the rendered include file must
	// still carry the specific rule's includeIf block after the general
	// one, since includeIf block order, not configuration order, decides
	// which wins.
	cfg := testConfig()
	rendered, err := gitconfig.Render(cfg, "/home/simon")
	if err != nil {
		t.Fatalf("Render() returned an unexpected error: %v", err)
	}
	includes := rendered[gitconfig.IncludePath("/home/simon")]

	if !strings.Contains(includes, "locked-down.gitconfig") {
		t.Fatalf("Render() include file never references the locked-down profile:\n%s", includes)
	}

	hostOnlyBlock := strings.Index(includes, "git@github.com:**")
	repoBlock := strings.Index(includes, "ostendorf-cloud/secrets")
	if hostOnlyBlock == -1 {
		t.Fatalf("Render() include file has no host only block for github.com:\n%s", includes)
	}
	if repoBlock == -1 {
		t.Fatalf("Render() include file has no repo specific block for ostendorf-cloud/secrets:\n%s", includes)
	}
	if repoBlock < hostOnlyBlock {
		t.Fatalf(
			"Render() emits the repo specific block before the host only block, want it after so it wins: %s",
			includes,
		)
	}
}

func TestRenderProfile(t *testing.T) {
	t.Parallel()

	t.Run("adds user.useConfigOnly when the profile does not set its own", func(t *testing.T) {
		t.Parallel()
		got, err := gitconfig.RenderProfile(config.Profile{"user.email": "simon@example.test"}, true)
		if err != nil {
			t.Fatalf("RenderProfile() returned an unexpected error: %v", err)
		}
		if !strings.Contains(got, "useConfigOnly") {
			t.Fatalf("RenderProfile() = %q, want it to add user.useConfigOnly", got)
		}
	})

	t.Run("omits user.useConfigOnly when the config opts out", func(t *testing.T) {
		t.Parallel()
		got, err := gitconfig.RenderProfile(config.Profile{"user.email": "simon@example.test"}, false)
		if err != nil {
			t.Fatalf("RenderProfile() returned an unexpected error: %v", err)
		}
		if strings.Contains(got, "useConfigOnly") {
			t.Fatalf("RenderProfile() = %q, want no user.useConfigOnly when opted out", got)
		}
	})

	t.Run("a profile's own useConfigOnly value is not overwritten", func(t *testing.T) {
		t.Parallel()
		got, err := gitconfig.RenderProfile(
			config.Profile{"user.email": "simon@example.test", "user.useConfigOnly": "false"}, true,
		)
		if err != nil {
			t.Fatalf("RenderProfile() returned an unexpected error: %v", err)
		}
		if !strings.Contains(got, `useConfigOnly = "false"`) {
			t.Fatalf("RenderProfile() = %q, want the profile's own useConfigOnly value kept", got)
		}
	})

	t.Run("an invalid key is rejected", func(t *testing.T) {
		t.Parallel()
		_, err := gitconfig.RenderProfile(config.Profile{"notakey": "value"}, true)
		if err == nil {
			t.Fatal("RenderProfile() succeeded, want an error for a key with no section")
		}
	})
}

func TestRender_HasconfigFormsPerRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		rule  config.Rule
		wants []string
	}{
		{
			name: "a host only rule matches every owner and repo under it, both URL forms",
			rule: config.Rule{Host: "github.com", Profile: "private"},
			wants: []string{
				`hasconfig:remote.*.url:git@github.com:**`,
				`hasconfig:remote.*.url:https://github.com/**`,
			},
		},
		{
			name: "a host and owner rule matches every repo under that owner, both URL forms",
			rule: config.Rule{Host: "github.com", Owner: "ostendorf-cloud", Profile: "private"},
			wants: []string{
				`hasconfig:remote.*.url:git@github.com:ostendorf-cloud/**`,
				`hasconfig:remote.*.url:https://github.com/ostendorf-cloud/**`,
			},
		},
		{
			name: "a host, owner and repo rule matches that repository literally, with and without .git",
			rule: config.Rule{Host: "github.com", Owner: "ostendorf-cloud", Repo: "gitcd", Profile: "private"},
			wants: []string{
				`hasconfig:remote.*.url:git@github.com:ostendorf-cloud/gitcd"`,
				`hasconfig:remote.*.url:git@github.com:ostendorf-cloud/gitcd.git"`,
				`hasconfig:remote.*.url:https://github.com/ostendorf-cloud/gitcd"`,
				`hasconfig:remote.*.url:https://github.com/ostendorf-cloud/gitcd.git"`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Config{
				Root:     "/home/simon/Code",
				Default:  "{host}/{owner}/{repo}",
				Profiles: map[string]config.Profile{"private": {"user.email": "simon@example.test"}},
				Rules:    []config.Rule{tt.rule},
			}
			rendered, err := gitconfig.Render(cfg, "/home/simon")
			if err != nil {
				t.Fatalf("Render() returned an unexpected error: %v", err)
			}
			includes := rendered[gitconfig.IncludePath("/home/simon")]
			for _, want := range tt.wants {
				if !strings.Contains(includes, want) {
					t.Fatalf("Render() include file is missing %q:\n%s", want, includes)
				}
			}
		})
	}
}

func TestRender_GitdirBlock(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Root:     "/home/simon/Code",
		Default:  "{host}/{owner}/{repo}",
		Profiles: map[string]config.Profile{"private": {"user.email": "simon@example.test"}},
		Rules:    []config.Rule{{Host: "github.com", Owner: "ostendorf-cloud", Profile: "private"}},
	}
	rendered, err := gitconfig.Render(cfg, "/home/simon")
	if err != nil {
		t.Fatalf("Render() returned an unexpected error: %v", err)
	}
	includes := rendered[gitconfig.IncludePath("/home/simon")]

	want := `gitdir/i:/home/simon/Code/github.com/ostendorf-cloud/*/**`
	if !strings.Contains(includes, want) {
		t.Fatalf("Render() include file is missing the gitdir block %q:\n%s", want, includes)
	}
}

func TestPaths(t *testing.T) {
	t.Parallel()

	home := "/home/simon"
	if got, want := gitconfig.Dir(home), "/home/simon/.config/gitcd"; got != want {
		t.Fatalf("Dir(%q) = %q, want %q", home, got, want)
	}
	if got, want := gitconfig.ProfilesDir(home), "/home/simon/.config/gitcd/profiles"; got != want {
		t.Fatalf("ProfilesDir(%q) = %q, want %q", home, got, want)
	}
	if got, want := gitconfig.ProfilePath(home, "private"), "/home/simon/.config/gitcd/profiles/private.gitconfig"; got != want {
		t.Fatalf("ProfilePath(%q, %q) = %q, want %q", home, "private", got, want)
	}
	if got, want := gitconfig.IncludePath(home), "/home/simon/.config/gitcd/includes.gitconfig"; got != want {
		t.Fatalf("IncludePath(%q) = %q, want %q", home, got, want)
	}
}
