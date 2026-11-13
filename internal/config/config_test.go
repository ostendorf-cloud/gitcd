// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/layout"
	"github.com/ostendorf-cloud/gitcd/internal/remote"
)

// withHome points HOME, and XDG_CONFIG_HOME at a fresh subdirectory of it,
// to a temporary directory for the duration of the test, so Load never
// reads the real user's configuration.
func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	return home
}

func TestLoad(t *testing.T) {
	t.Run("no configuration file yields the defaults", func(t *testing.T) {
		withHome(t)

		got, err := config.Load()
		if err != nil {
			t.Fatalf("Load() returned an unexpected error: %v", err)
		}
		want := config.Default()
		want.Root, _ = os.UserHomeDir()
		want.Root = filepath.Join(want.Root, "Code")
		if got.Root != want.Root || got.Default != want.Default {
			t.Fatalf("Load() = %+v, want %+v", got, want)
		}
		if !reflect.DeepEqual(got.Hosts, want.Hosts) {
			t.Fatalf("Load().Hosts = %+v, want %+v", got.Hosts, want.Hosts)
		}
	})

	t.Run("no configuration file still ships the azure devops default layout and aliases", func(t *testing.T) {
		withHome(t)

		got, err := config.Load()
		if err != nil {
			t.Fatalf("Load() returned an unexpected error: %v", err)
		}

		azure, ok := got.Hosts["dev.azure.com"]
		if !ok {
			t.Fatalf("Load().Hosts has no entry for dev.azure.com, but remote.Parse recognises it unconditionally: %+v", got.Hosts)
		}
		if azure.Layout != "{host}/{org}/{project}/{repo}" {
			t.Fatalf("default dev.azure.com layout = %q, want the four segment layout carrying the project", azure.Layout)
		}
		if want := []string{"ssh.dev.azure.com", "vs-ssh.visualstudio.com"}; !equalStrings(azure.Aliases, want) {
			t.Fatalf("default dev.azure.com aliases = %v, want %v", azure.Aliases, want)
		}
	})

	t.Run("reads the home configuration file over the xdg fallback", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
root: ~/Projects
layout:
  default: "{owner}/{repo}"
`)
		writeFile(t, filepath.Join(home, ".config", "gitcd", "config.yaml"), `
root: ~/Other
`)

		got, err := config.Load()
		if err != nil {
			t.Fatalf("Load() returned an unexpected error: %v", err)
		}
		if want := filepath.Join(home, "Projects"); got.Root != want {
			t.Fatalf("Load().Root = %q, want %q", got.Root, want)
		}
		if got.Default != "{owner}/{repo}" {
			t.Fatalf("Load().Default = %q, want %q", got.Default, "{owner}/{repo}")
		}
	})

	t.Run("falls back to the xdg configuration file", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".config", "gitcd", "config.yaml"), `
root: ~/Other
`)

		got, err := config.Load()
		if err != nil {
			t.Fatalf("Load() returned an unexpected error: %v", err)
		}
		if want := filepath.Join(home, "Other"); got.Root != want {
			t.Fatalf("Load().Root = %q, want %q", got.Root, want)
		}
	})

	t.Run("loads the host settings and defaults a host without its own layout", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
hosts:
  dev.azure.com:
    layout: "{host}/{org}/{project}/{repo}"
    aliases: [ssh.dev.azure.com, VS-SSH.visualstudio.com]
  github.com:
    aliases: [github-work]
    protocol: ssh
`)

		got, err := config.Load()
		if err != nil {
			t.Fatalf("Load() returned an unexpected error: %v", err)
		}

		azure, ok := got.Hosts["dev.azure.com"]
		if !ok {
			t.Fatalf("Load().Hosts has no entry for dev.azure.com: %+v", got.Hosts)
		}
		if azure.Layout != "{host}/{org}/{project}/{repo}" {
			t.Fatalf("dev.azure.com layout = %q, want the configured template", azure.Layout)
		}
		if want := []string{"ssh.dev.azure.com", "vs-ssh.visualstudio.com"}; !equalStrings(azure.Aliases, want) {
			t.Fatalf("dev.azure.com aliases = %v, want %v, lowercased", azure.Aliases, want)
		}

		github, ok := got.Hosts["github.com"]
		if !ok {
			t.Fatalf("Load().Hosts has no entry for github.com: %+v", got.Hosts)
		}
		if github.Layout != got.Default {
			t.Fatalf("github.com layout = %q, want the fallback default %q", github.Layout, got.Default)
		}
		if github.Protocol != "ssh" {
			t.Fatalf("github.com protocol = %q, want %q", github.Protocol, "ssh")
		}
	})

	t.Run("adding an alias to a default host keeps its default layout, end to end", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
hosts:
  dev.azure.com:
    aliases: [ssh.dev.azure.com, vs-ssh.visualstudio.com, my-ado-alias]
`)

		got, err := config.Load()
		if err != nil {
			t.Fatalf("Load() returned an unexpected error: %v", err)
		}

		r, err := remote.Parse("https://dev.azure.com/acme-corp/Widget/_git/widget-repo", got.AliasMap())
		if err != nil {
			t.Fatalf("remote.Parse() returned an unexpected error: %v", err)
		}
		path, err := layout.Render(got.LayoutFor(r.Host), r)
		if err != nil {
			t.Fatalf("layout.Render() returned an unexpected error: %v, the default layout was dropped", err)
		}
		if want := "dev.azure.com/acme-corp/Widget/widget-repo"; path != want {
			t.Fatalf("Render() = %q, want %q, the project segment must survive", path, want)
		}

		r, err = remote.Parse("git@my-ado-alias:v3/acme-corp/Widget/widget-repo.git", got.AliasMap())
		if err != nil {
			t.Fatalf("remote.Parse() of the new alias returned an unexpected error: %v", err)
		}
		if r.Host != "dev.azure.com" {
			t.Fatalf("remote.Parse() resolved %q to host %q, want dev.azure.com", "my-ado-alias", r.Host)
		}
	})

	t.Run("setting a layout on a default host keeps its default aliases", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
hosts:
  dev.azure.com:
    layout: "{host}/{org}/{project}/{repo}-mirror"
`)

		got, err := config.Load()
		if err != nil {
			t.Fatalf("Load() returned an unexpected error: %v", err)
		}

		azure, ok := got.Hosts["dev.azure.com"]
		if !ok {
			t.Fatalf("Load().Hosts has no entry for dev.azure.com: %+v", got.Hosts)
		}
		if azure.Layout != "{host}/{org}/{project}/{repo}-mirror" {
			t.Fatalf("dev.azure.com layout = %q, want the configured template", azure.Layout)
		}
		if want := []string{"ssh.dev.azure.com", "vs-ssh.visualstudio.com"}; !equalStrings(azure.Aliases, want) {
			t.Fatalf("dev.azure.com aliases = %v, want the default aliases %v kept", azure.Aliases, want)
		}
	})

	t.Run("a host with no default entry still works", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
hosts:
  gitlab.com:
    protocol: ssh
`)

		got, err := config.Load()
		if err != nil {
			t.Fatalf("Load() returned an unexpected error: %v", err)
		}

		gitlab, ok := got.Hosts["gitlab.com"]
		if !ok {
			t.Fatalf("Load().Hosts has no entry for gitlab.com: %+v", got.Hosts)
		}
		if gitlab.Layout != got.Default {
			t.Fatalf("gitlab.com layout = %q, want the fallback default %q", gitlab.Layout, got.Default)
		}
		if len(gitlab.Aliases) != 0 {
			t.Fatalf("gitlab.com aliases = %v, want none", gitlab.Aliases)
		}
		if gitlab.Protocol != "ssh" {
			t.Fatalf("gitlab.com protocol = %q, want %q", gitlab.Protocol, "ssh")
		}
	})

	t.Run("an empty alias list deliberately clears the default aliases", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
hosts:
  dev.azure.com:
    aliases: []
`)

		got, err := config.Load()
		if err != nil {
			t.Fatalf("Load() returned an unexpected error: %v", err)
		}

		azure, ok := got.Hosts["dev.azure.com"]
		if !ok {
			t.Fatalf("Load().Hosts has no entry for dev.azure.com: %+v", got.Hosts)
		}
		if len(azure.Aliases) != 0 {
			t.Fatalf("dev.azure.com aliases = %v, want none, an explicit empty list clears the default", azure.Aliases)
		}
		if _, ok := got.AliasMap()["ssh.dev.azure.com"]; ok {
			t.Fatal("AliasMap() still resolves the cleared default alias ssh.dev.azure.com")
		}
	})

	t.Run("an empty layout deliberately clears the default layout, falling back to the generic default", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
hosts:
  dev.azure.com:
    layout: ""
`)

		got, err := config.Load()
		if err != nil {
			t.Fatalf("Load() returned an unexpected error: %v", err)
		}

		if got.LayoutFor("dev.azure.com") != got.Default {
			t.Fatalf("LayoutFor(dev.azure.com) = %q, want the generic default %q", got.LayoutFor("dev.azure.com"), got.Default)
		}
	})

	t.Run("an unknown key is a named error", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), "roots: ~/Code\n")

		_, err := config.Load()
		if !errors.Is(err, config.ErrUnknownKey) {
			t.Fatalf("Load() error = %v, want it to wrap config.ErrUnknownKey", err)
		}
	})

	t.Run("an unknown key nested under a host is a named error", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), "hosts:\n  github.com:\n    alias: [github-work]\n")

		_, err := config.Load()
		if !errors.Is(err, config.ErrUnknownKey) {
			t.Fatalf("Load() error = %v, want it to wrap config.ErrUnknownKey", err)
		}
	})

	t.Run("a tilde naming another user's home directory is rejected", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: ~otheruser/Code\n")

		_, err := config.Load()
		if err == nil {
			t.Fatal("Load() succeeded, want an error for a tilde naming another user")
		}
	})
}

func TestPath(t *testing.T) {
	t.Run("the primary candidate when neither file exists", func(t *testing.T) {
		home := withHome(t)

		got, err := config.Path()
		if err != nil {
			t.Fatalf("Path() returned an unexpected error: %v", err)
		}
		if want := filepath.Join(home, ".gitcd.yaml"); got != want {
			t.Fatalf("Path() = %q, want %q", got, want)
		}
	})

	t.Run("the file Load would read when it already exists", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: ~/elsewhere\n")

		got, err := config.Path()
		if err != nil {
			t.Fatalf("Path() returned an unexpected error: %v", err)
		}
		if want := filepath.Join(home, ".gitcd.yaml"); got != want {
			t.Fatalf("Path() = %q, want %q", got, want)
		}
	})

	t.Run("the xdg fallback when only it exists", func(t *testing.T) {
		home := withHome(t)
		xdgPath := filepath.Join(home, ".config", "gitcd", "config.yaml")
		writeFile(t, xdgPath, "root: ~/elsewhere\n")

		got, err := config.Path()
		if err != nil {
			t.Fatalf("Path() returned an unexpected error: %v", err)
		}
		if got != xdgPath {
			t.Fatalf("Path() = %q, want %q", got, xdgPath)
		}
	})
}

func TestLoad_Profiles(t *testing.T) {
	t.Run("no file ships useConfigOnly true and no profiles", func(t *testing.T) {
		withHome(t)

		got, err := config.Load()
		if err != nil {
			t.Fatalf("Load() returned an unexpected error: %v", err)
		}
		if !got.UseConfigOnly {
			t.Fatal("Load().UseConfigOnly = false, want true when the file sets no useConfigOnly key")
		}
		if len(got.Profiles) != 0 {
			t.Fatalf("Load().Profiles = %+v, want none", got.Profiles)
		}
	})

	t.Run("loads profiles, rules and the default profile", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  private:
    user.name: Simon Ostendorf
    user.email: "simon@example.test"
  ado:
    user.name: Simon Ostendorf
    user.email: "simon@work.example.test"

rules:
  - host: dev.azure.com
    profile: ado
  - host: github.com
    owner: ostendorf-cloud
    profile: private

defaultProfile: private
`)

		got, err := config.Load()
		if err != nil {
			t.Fatalf("Load() returned an unexpected error: %v", err)
		}
		if got.DefaultProfile != "private" {
			t.Fatalf("Load().DefaultProfile = %q, want %q", got.DefaultProfile, "private")
		}
		if len(got.Rules) != 2 {
			t.Fatalf("Load().Rules = %+v, want 2 entries", got.Rules)
		}
		private, ok := got.Profiles["private"]
		if !ok {
			t.Fatalf("Load().Profiles has no entry for %q: %+v", "private", got.Profiles)
		}
		if private["user.email"] != "simon@example.test" {
			t.Fatalf("private profile user.email = %q, want %q", private["user.email"], "simon@example.test")
		}
	})

	t.Run("useConfigOnly false is honoured", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), "useConfigOnly: false\n")

		got, err := config.Load()
		if err != nil {
			t.Fatalf("Load() returned an unexpected error: %v", err)
		}
		if got.UseConfigOnly {
			t.Fatal("Load().UseConfigOnly = true, want false when the file sets useConfigOnly: false")
		}
	})

	t.Run("a profile key under the remote section is rejected", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  private:
    remote.origin.url: https://example.test/acme/widget.git
`)

		_, err := config.Load()
		if !errors.Is(err, config.ErrInvalidProfile) {
			t.Fatalf("Load() error = %v, want it to wrap config.ErrInvalidProfile", err)
		}
	})

	t.Run("a profile key that is not a section.key pair is rejected", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  private:
    username: Simon Ostendorf
`)

		_, err := config.Load()
		if !errors.Is(err, config.ErrInvalidProfile) {
			t.Fatalf("Load() error = %v, want it to wrap config.ErrInvalidProfile", err)
		}
	})

	t.Run("a rule with no host is rejected", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  private:
    user.email: "simon@example.test"

rules:
  - owner: ostendorf-cloud
    profile: private
`)

		_, err := config.Load()
		if !errors.Is(err, config.ErrInvalidProfile) {
			t.Fatalf("Load() error = %v, want it to wrap config.ErrInvalidProfile", err)
		}
	})

	t.Run("a rule naming a repo without an owner is rejected", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  private:
    user.email: "simon@example.test"

rules:
  - host: github.com
    repo: widget
    profile: private
`)

		_, err := config.Load()
		if !errors.Is(err, config.ErrInvalidProfile) {
			t.Fatalf("Load() error = %v, want it to wrap config.ErrInvalidProfile", err)
		}
	})

	t.Run("a rule naming an undefined profile is rejected", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
rules:
  - host: github.com
    profile: missing
`)

		_, err := config.Load()
		if !errors.Is(err, config.ErrInvalidProfile) {
			t.Fatalf("Load() error = %v, want it to wrap config.ErrInvalidProfile", err)
		}
	})

	t.Run("a defaultProfile naming an undefined profile is rejected", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), "defaultProfile: missing\n")

		_, err := config.Load()
		if !errors.Is(err, config.ErrInvalidProfile) {
			t.Fatalf("Load() error = %v, want it to wrap config.ErrInvalidProfile", err)
		}
	})
}

func TestProfile_Keys(t *testing.T) {
	t.Parallel()

	p := config.Profile{"user.email": "[email protected]", "user.name": "Example", "gpg.format": "ssh"}
	got := p.Keys()
	want := []string{"gpg.format", "user.email", "user.name"}
	if !equalStrings(got, want) {
		t.Fatalf("Profile.Keys() = %v, want %v, sorted", got, want)
	}
}

func TestConfig_AliasMap(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Hosts: map[string]config.Host{
			"dev.azure.com": {Aliases: []string{"ssh.dev.azure.com", "vs-ssh.visualstudio.com"}},
			"github.com":    {Aliases: []string{"github-work", "github-private"}},
		},
	}

	got := cfg.AliasMap()
	want := map[string]string{
		"ssh.dev.azure.com":       "dev.azure.com",
		"vs-ssh.visualstudio.com": "dev.azure.com",
		"github-work":             "github.com",
		"github-private":          "github.com",
	}
	if len(got) != len(want) {
		t.Fatalf("AliasMap() = %v, want %v", got, want)
	}
	for alias, host := range want {
		if got[alias] != host {
			t.Fatalf("AliasMap()[%q] = %q, want %q", alias, got[alias], host)
		}
	}
}

func TestConfig_LayoutFor(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Default: "{host}/{owner}/{repo}",
		Hosts: map[string]config.Host{
			"dev.azure.com": {Layout: "{host}/{org}/{project}/{repo}"},
		},
	}

	tests := []struct {
		name string
		host string
		want string
	}{
		{name: "a host with its own layout uses it", host: "dev.azure.com", want: "{host}/{org}/{project}/{repo}"},
		{name: "a host with its own layout is matched case insensitively", host: "Dev.Azure.Com", want: "{host}/{org}/{project}/{repo}"},
		{name: "an unconfigured host falls back to the default", host: "gitlab.com", want: "{host}/{owner}/{repo}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := cfg.LayoutFor(tt.host); got != tt.want {
				t.Fatalf("LayoutFor(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
