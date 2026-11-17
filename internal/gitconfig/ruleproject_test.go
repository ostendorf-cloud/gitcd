// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package gitconfig_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
)

func TestRender_RuleProject(t *testing.T) {
	t.Parallel()

	gitdir := func(rel string) string {
		return `gitdir/i:` + filepath.ToSlash(filepath.Join(rootDir, rel)) + `"`
	}
	azureAliases := []string{"ssh.dev.azure.com", "vs-ssh.visualstudio.com"}

	tests := []struct {
		name    string
		layout  string
		rule    config.Rule
		wants   []string
		forbids []string
	}{
		{
			name:   "the path condition ends at the repository and every URL form carries the project",
			layout: "{host}/{org}/{project}/{repo}",
			rule:   config.Rule{Host: "dev.azure.com", Owner: "acme", Project: "billing", Profile: "private"},
			wants: []string{
				gitdir("dev.azure.com/acme/billing/*/**"),
				`hasconfig:remote.*.url:git@dev.azure.com:v3/acme/billing/**"`,
				`hasconfig:remote.*.url:git@ssh.dev.azure.com:v3/acme/billing/**"`,
				`hasconfig:remote.*.url:git@vs-ssh.visualstudio.com:v3/acme/billing/**"`,
				`hasconfig:remote.*.url:https://dev.azure.com/acme/billing/**"`,
				`hasconfig:remote.*.url:https://*@dev.azure.com/acme/billing/**"`,
				`hasconfig:remote.*.url:https://*@vs-ssh.visualstudio.com/acme/billing/**"`,
			},
			forbids: []string{
				gitdir("dev.azure.com/acme/billing/*/*/**"),
				`hasconfig:remote.*.url:ssh://git@dev.azure.com`,
			},
		},
		{
			name:   "a project and a repo pin both segments in every URL form",
			layout: "{host}/{org}/{project}/{repo}",
			rule:   config.Rule{Host: "dev.azure.com", Owner: "acme", Project: "billing", Repo: "widget", Profile: "private"},
			wants: []string{
				gitdir("dev.azure.com/acme/billing/widget/**"),
				`hasconfig:remote.*.url:git@dev.azure.com:v3/acme/billing/widget"`,
				`hasconfig:remote.*.url:git@ssh.dev.azure.com:v3/acme/billing/widget"`,
				`hasconfig:remote.*.url:https://dev.azure.com/acme/billing/_git/widget"`,
				`hasconfig:remote.*.url:https://*@dev.azure.com/acme/billing/_git/widget"`,
				`hasconfig:remote.*.url:https://*@vs-ssh.visualstudio.com/acme/billing/_git/widget"`,
			},
			forbids: []string{`acme/*/widget`, `acme/*/_git/widget`},
		},
		{
			name:   "a project with a space is written as a space and as %20 in the URL forms and as a space in the path",
			layout: "{host}/{org}/{project}/{repo}",
			rule:   config.Rule{Host: "dev.azure.com", Owner: "acme", Project: "My Project", Profile: "private"},
			wants: []string{
				gitdir("dev.azure.com/acme/My Project/*/**"),
				`hasconfig:remote.*.url:git@ssh.dev.azure.com:v3/acme/My Project/**"`,
				`hasconfig:remote.*.url:git@ssh.dev.azure.com:v3/acme/My%20Project/**"`,
				`hasconfig:remote.*.url:https://dev.azure.com/acme/My Project/**"`,
				`hasconfig:remote.*.url:https://dev.azure.com/acme/My%20Project/**"`,
				`hasconfig:remote.*.url:https://*@dev.azure.com/acme/My%20Project/**"`,
				`hasconfig:remote.*.url:git@vs-ssh.visualstudio.com:v3/acme/My%20Project/**"`,
			},
		},
		{
			name:   "a project with parentheses is written literally and in both encodings",
			layout: "{host}/{org}/{project}/{repo}",
			rule:   config.Rule{Host: "dev.azure.com", Owner: "acme", Project: "Billing (EU)", Profile: "private"},
			wants: []string{
				gitdir("dev.azure.com/acme/Billing (EU)/*/**"),
				`hasconfig:remote.*.url:https://dev.azure.com/acme/Billing (EU)/**"`,
				`hasconfig:remote.*.url:https://dev.azure.com/acme/Billing%20(EU)/**"`,
				`hasconfig:remote.*.url:https://dev.azure.com/acme/Billing%20%28EU%29/**"`,
				`hasconfig:remote.*.url:git@ssh.dev.azure.com:v3/acme/Billing%20(EU)/**"`,
				`hasconfig:remote.*.url:git@ssh.dev.azure.com:v3/acme/Billing%20%28EU%29/**"`,
			},
		},
		{
			name:   "a project with an ampersand is rendered literally and percent encoded",
			layout: "{host}/{org}/{project}/{repo}",
			rule:   config.Rule{Host: "dev.azure.com", Owner: "acme", Project: "R&D", Profile: "private"},
			wants: []string{
				`hasconfig:remote.*.url:https://dev.azure.com/acme/R&D/**"`,
				`hasconfig:remote.*.url:https://dev.azure.com/acme/R%26D/**"`,
				`hasconfig:remote.*.url:git@ssh.dev.azure.com:v3/acme/R%26D/**"`,
			},
		},
		{
			name:   "a project with ! @ and ^ is rendered strictly encoded with the @ as %40",
			layout: "{host}/{org}/{project}/{repo}",
			rule:   config.Rule{Host: "dev.azure.com", Owner: "acme", Project: "Ops! @^", Profile: "private"},
			wants: []string{
				`hasconfig:remote.*.url:https://dev.azure.com/acme/Ops%21%20%40%5E/**"`,
				`hasconfig:remote.*.url:https://dev.azure.com/acme/Ops%21%20@%5E/**"`,
				`hasconfig:remote.*.url:https://dev.azure.com/acme/Ops!%20@^/**"`,
				`hasconfig:remote.*.url:https://dev.azure.com/acme/Ops!%20%40^/**"`,
			},
		},
		{
			name:   "a non ASCII project is rendered with upper and lower case hex",
			layout: "{host}/{org}/{project}/{repo}",
			rule:   config.Rule{Host: "dev.azure.com", Owner: "acme", Project: "Café", Profile: "private"},
			wants: []string{
				`hasconfig:remote.*.url:https://dev.azure.com/acme/Café/**"`,
				`hasconfig:remote.*.url:https://dev.azure.com/acme/Caf%C3%A9/**"`,
				`hasconfig:remote.*.url:https://dev.azure.com/acme/Caf%c3%a9/**"`,
				`hasconfig:remote.*.url:git@ssh.dev.azure.com:v3/acme/Caf%c3%a9/**"`,
			},
		},
		{
			name:   "a project with a space and a repo",
			layout: "{host}/{org}/{project}/{repo}",
			rule:   config.Rule{Host: "dev.azure.com", Owner: "acme", Project: "My Project", Repo: "widget", Profile: "private"},
			wants: []string{
				`hasconfig:remote.*.url:git@dev.azure.com:v3/acme/My%20Project/widget"`,
				`hasconfig:remote.*.url:git@dev.azure.com:v3/acme/My Project/widget"`,
				`hasconfig:remote.*.url:https://dev.azure.com/acme/My%20Project/_git/widget"`,
				`hasconfig:remote.*.url:https://dev.azure.com/acme/My Project/_git/widget"`,
			},
		},
		{
			name:   "a layout with the project elsewhere keeps the project in its own position",
			layout: "{host}/{project}/{org}/{repo}",
			rule:   config.Rule{Host: "dev.azure.com", Owner: "acme", Project: "billing", Profile: "private"},
			wants:  []string{gitdir("dev.azure.com/billing/acme/*/**")},
		},
		{
			name:    "a layout with no project placeholder renders no path condition, which would match every project",
			layout:  "{host}/{org}/{repo}",
			rule:    config.Rule{Host: "dev.azure.com", Owner: "acme", Project: "billing", Profile: "private"},
			wants:   []string{`hasconfig:remote.*.url:https://dev.azure.com/acme/billing/**"`},
			forbids: []string{`gitdir/i:`},
		},
		{
			name:   "an owner rule without a project still wildcards the project segment",
			layout: "{host}/{org}/{project}/{repo}",
			rule:   config.Rule{Host: "dev.azure.com", Owner: "acme", Profile: "private"},
			wants: []string{
				gitdir("dev.azure.com/acme/*/*/**"),
				`hasconfig:remote.*.url:https://dev.azure.com/acme/**"`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Config{
				Root:     rootDir,
				Default:  "{host}/{owner}/{repo}",
				Hosts:    map[string]config.Host{"dev.azure.com": {Layout: tt.layout, Aliases: azureAliases}},
				Profiles: map[string]config.Profile{"private": {"user.email": "simon@example.test"}},
				Rules:    []config.Rule{tt.rule},
			}
			rendered, err := gitconfig.Render(cfg, homeDir)
			if err != nil {
				t.Fatalf("Render() returned an unexpected error: %v", err)
			}
			includes := rendered[gitconfig.IncludePath(homeDir)]
			for _, want := range tt.wants {
				if !strings.Contains(includes, want) {
					t.Fatalf("Render() include file is missing %q:\n%s", want, includes)
				}
			}
			for _, forbid := range tt.forbids {
				if strings.Contains(includes, forbid) {
					t.Fatalf("Render() include file has %q, want it absent:\n%s", forbid, includes)
				}
			}
		})
	}
}
