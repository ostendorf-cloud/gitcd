// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package profile_test

import (
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/profile"
	"github.com/ostendorf-cloud/gitcd/internal/remote"
)

// projectRules lists every rule shape a valid configuration can hold for
// dev.azure.com, most specific first. A result can then only come from the
// ranking, never from the order of the list.
func projectRules() []config.Rule {
	return []config.Rule{
		{Host: "dev.azure.com", Owner: "acme", Project: "billing", Repo: "widget", Profile: "owner-project-repo"},
		{Host: "dev.azure.com", Owner: "acme", Repo: "widget", Profile: "owner-repo"},
		{Host: "dev.azure.com", Owner: "acme", Project: "billing", Profile: "owner-project"},
		{Host: "dev.azure.com", Owner: "acme", Profile: "owner"},
		{Host: "dev.azure.com", Profile: "host"},
	}
}

func TestResolve_Project(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		rules       []config.Rule
		remote      remote.Remote
		wantProfile string
		wantProject string
	}{
		{
			name:        "the project rule applies to a repository of that project",
			rules:       projectRules(),
			remote:      remote.Remote{Host: "dev.azure.com", Owner: "acme", Project: "billing", Repo: "gadget"},
			wantProfile: "owner-project",
			wantProject: "billing",
		},
		{
			name:        "another project of the same organisation gets the owner rule",
			rules:       projectRules(),
			remote:      remote.Remote{Host: "dev.azure.com", Owner: "acme", Project: "ops", Repo: "gadget"},
			wantProfile: "owner",
		},
		{
			name:        "another organisation gets the host rule",
			rules:       projectRules(),
			remote:      remote.Remote{Host: "dev.azure.com", Owner: "zeta", Project: "billing", Repo: "gadget"},
			wantProfile: "host",
		},
		{
			name: "a repository rule outranks a project rule listed before it",
			rules: []config.Rule{
				{Host: "dev.azure.com", Owner: "acme", Project: "billing", Profile: "owner-project"},
				{Host: "dev.azure.com", Owner: "acme", Repo: "widget", Profile: "owner-repo"},
			},
			remote:      remote.Remote{Host: "dev.azure.com", Owner: "acme", Project: "billing", Repo: "widget"},
			wantProfile: "owner-repo",
		},
		{
			name:        "the rule that pins project and repo is the most specific",
			rules:       projectRules(),
			remote:      remote.Remote{Host: "dev.azure.com", Owner: "acme", Project: "billing", Repo: "widget"},
			wantProfile: "owner-project-repo",
			wantProject: "billing",
		},
		{
			name:        "a project with a space matches as written",
			rules:       []config.Rule{{Host: "dev.azure.com", Owner: "acme", Project: "My Project", Profile: "spaced"}},
			remote:      remote.Remote{Host: "dev.azure.com", Owner: "acme", Project: "My Project", Repo: "widget"},
			wantProfile: "spaced",
			wantProject: "My Project",
		},
		{
			name:        "a project matches by exact case, as owner and repo do",
			rules:       []config.Rule{{Host: "dev.azure.com", Owner: "acme", Project: "billing", Profile: "owner-project"}},
			remote:      remote.Remote{Host: "dev.azure.com", Owner: "acme", Project: "Billing", Repo: "widget"},
			wantProfile: "",
		},
		{
			name:        "a project rule never matches a remote without a project",
			rules:       []config.Rule{{Host: "dev.azure.com", Owner: "acme", Project: "billing", Profile: "owner-project"}},
			remote:      remote.Remote{Host: "dev.azure.com", Owner: "acme", Repo: "widget"},
			wantProfile: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := profile.Resolve(tt.remote, config.Config{Rules: tt.rules})
			if got.Profile != tt.wantProfile {
				t.Fatalf("Resolve() profile = %q, want %q", got.Profile, tt.wantProfile)
			}
			if tt.wantProfile == "" {
				if got.Rule != nil {
					t.Fatalf("Resolve() rule = %+v, want none", got.Rule)
				}
				return
			}
			if got.Rule == nil || got.Rule.Project != tt.wantProject {
				t.Fatalf("Resolve() rule = %+v, want the rule with project %q", got.Rule, tt.wantProject)
			}
		})
	}
}

func TestSortRules_Project(t *testing.T) {
	t.Parallel()

	got := profile.SortRules(projectRules())
	want := []string{"host", "owner", "owner-project", "owner-repo", "owner-project-repo"}
	if len(got) != len(want) {
		t.Fatalf("SortRules() = %+v, want %d entries", got, len(want))
	}
	for i, name := range want {
		if got[i].Profile != name {
			t.Fatalf("SortRules()[%d].Profile = %q, want %q, ascending specificity", i, got[i].Profile, name)
		}
	}
}
