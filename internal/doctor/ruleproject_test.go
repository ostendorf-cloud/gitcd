// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package doctor_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/doctor"
)

func TestCheckRepo_RuleProjectCase(t *testing.T) {
	home := setupGitEnv(t)
	root := filepath.Join(home, "code")

	tests := []struct {
		name        string
		repo        string
		ruleProject string
		wantFinding bool
	}{
		{name: "a project spelled in another case never applies and is reported", repo: "widget", ruleProject: "billing", wantFinding: true},
		{name: "a project spelled as the remote spells it is not reported", repo: "gadget", ruleProject: "Billing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoDir := filepath.Join(root, "dev.azure.com", "acme", "Billing", tt.repo)
			initRepository(t, repoDir)
			run(t, repoDir, "remote", "add", "origin", "https://dev.azure.com/acme/Billing/_git/"+tt.repo)

			cfg := config.Config{
				Root:    root,
				Default: "{host}/{owner}/{repo}",
				Hosts:   map[string]config.Host{"dev.azure.com": {Layout: "{host}/{org}/{project}/{repo}"}},
				Profiles: map[string]config.Profile{
					"work": {"user.email": "acme-work-identity"},
				},
				Rules: []config.Rule{{Host: "dev.azure.com", Owner: "acme", Project: tt.ruleProject, Profile: "work"}},
			}

			name := "dev.azure.com/acme/Billing/" + tt.repo
			findings, err := doctor.CheckRepo(context.Background(), cfg, home, name, repoDir)
			if err != nil {
				t.Fatalf("CheckRepo() returned an unexpected error: %v", err)
			}
			_, got := findKind(findings, doctor.KindRuleCaseMismatch)
			if got != tt.wantFinding {
				t.Fatalf("CheckRepo() = %+v, want a %s finding: %v", findings, doctor.KindRuleCaseMismatch, tt.wantFinding)
			}
		})
	}
}
