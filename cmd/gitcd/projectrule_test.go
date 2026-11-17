// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
	"github.com/ostendorf-cloud/gitcd/internal/testenv"
)

// projectRulesConfig holds a host rule, a rule for one project of one
// organisation and a rule for a project whose name carries a space. Every
// profile sets only user.email, which is what git is asked for.
const projectRulesConfig = `
profiles:
  host-wide:
    user.email: host-wide@example.test
  billing:
    user.email: billing@example.test
  spaced:
    user.email: spaced@example.test
  paren:
    user.email: paren@example.test
  rnd:
    user.email: rnd@example.test
  bang:
    user.email: bang@example.test
  cafe:
    user.email: cafe@example.test

rules:
  - host: dev.azure.com
    owner: acme
    project: "Billing (EU)"
    profile: paren
  - host: dev.azure.com
    owner: acme
    project: "R&D"
    profile: rnd
  - host: dev.azure.com
    owner: acme
    project: "Ops! @^~ Team"
    profile: bang
  - host: dev.azure.com
    owner: acme
    project: "Café"
    profile: cafe
  - host: dev.azure.com
    profile: host-wide
  - host: dev.azure.com
    owner: acme
    project: billing
    profile: billing
  - host: dev.azure.com
    owner: acme
    project: My Project
    profile: spaced
`

// applyAndInclude runs "gitcd apply" and replaces the global git
// configuration with one that includes what it rendered, nothing else.
//
// A caller creates its repositories first: initRepository commits, and the
// commit needs the identity setupEnv writes into the global file. The
// replacement leaves no identity there, so every user.email git resolves
// afterwards comes from a profile.
func applyAndInclude(t *testing.T, home string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := run([]string{"apply"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apply = %d, want 0, stderr: %q", code, stderr.String())
	}
	global := os.Getenv("GIT_CONFIG_GLOBAL")
	writeFile(t, global, "[include]\n\tpath = "+filepath.ToSlash(gitconfig.IncludePath(home))+"\n")
}

func TestProjectRule_GitResolvesTheProjectProfile(t *testing.T) {
	tests := []struct {
		name string
		// layoutPath is the repository's directory below the clone root, ""
		// for a directory outside the root, where only a URL condition can
		// select the profile.
		layoutPath  string
		remote      string
		wantProfile string
		wantRule    string
	}{
		{
			name:        "https remote of the project, in the layout path",
			layoutPath:  "dev.azure.com/acme/billing/widget",
			remote:      "https://dev.azure.com/acme/billing/_git/widget",
			wantProfile: "billing",
			wantRule:    `host="dev.azure.com" owner="acme" project="billing"`,
		},
		{
			name:        "https remote with the organisation as user, in the layout path",
			layoutPath:  "dev.azure.com/acme/billing/widget",
			remote:      "https://acme@dev.azure.com/acme/billing/_git/widget",
			wantProfile: "billing",
			wantRule:    `project="billing"`,
		},
		{
			name:        "ssh remote of the project, in the layout path",
			layoutPath:  "dev.azure.com/acme/billing/widget",
			remote:      "git@ssh.dev.azure.com:v3/acme/billing/widget",
			wantProfile: "billing",
			wantRule:    `project="billing"`,
		},
		{
			name:        "legacy ssh host remote of the project, in the layout path",
			layoutPath:  "dev.azure.com/acme/billing/widget",
			remote:      "git@vs-ssh.visualstudio.com:v3/acme/billing/widget",
			wantProfile: "billing",
			wantRule:    `project="billing"`,
		},
		{
			name:        "https remote of the project, outside the layout path",
			remote:      "https://dev.azure.com/acme/billing/_git/widget",
			wantProfile: "billing",
			wantRule:    `project="billing"`,
		},
		{
			name:        "ssh remote of the project, outside the layout path",
			remote:      "git@ssh.dev.azure.com:v3/acme/billing/widget",
			wantProfile: "billing",
			wantRule:    `project="billing"`,
		},
		{
			name:        "another project of the same organisation gets the host rule, in the layout path",
			layoutPath:  "dev.azure.com/acme/ops/widget",
			remote:      "https://dev.azure.com/acme/ops/_git/widget",
			wantProfile: "host-wide",
			wantRule:    `(rule: host="dev.azure.com")`,
		},
		{
			name:        "another project of the same organisation gets the host rule, ssh, outside the layout path",
			remote:      "git@ssh.dev.azure.com:v3/acme/ops/widget",
			wantProfile: "host-wide",
			wantRule:    `(rule: host="dev.azure.com")`,
		},
		{
			name:        "a project whose name starts with the rule's project gets the host rule",
			layoutPath:  "dev.azure.com/acme/billing-archive/widget",
			remote:      "https://dev.azure.com/acme/billing-archive/_git/widget",
			wantProfile: "host-wide",
			wantRule:    `(rule: host="dev.azure.com")`,
		},
		{
			name:        "the same project name in another organisation gets the host rule, in the layout path",
			layoutPath:  "dev.azure.com/zeta/billing/widget",
			remote:      "https://dev.azure.com/zeta/billing/_git/widget",
			wantProfile: "host-wide",
			wantRule:    `(rule: host="dev.azure.com")`,
		},
		{
			name:        "the same project name in another organisation gets the host rule, ssh, outside the layout path",
			remote:      "git@ssh.dev.azure.com:v3/zeta/billing/widget",
			wantProfile: "host-wide",
			wantRule:    `(rule: host="dev.azure.com")`,
		},
		{
			name:        "https remote of a project with a space written %20, in the layout path",
			layoutPath:  "dev.azure.com/acme/My Project/widget",
			remote:      "https://dev.azure.com/acme/My%20Project/_git/widget",
			wantProfile: "spaced",
			wantRule:    `project="My Project"`,
		},
		{
			name:        "ssh remote of a project with a space written %20, in the layout path",
			layoutPath:  "dev.azure.com/acme/My Project/widget",
			remote:      "git@ssh.dev.azure.com:v3/acme/My%20Project/widget",
			wantProfile: "spaced",
			wantRule:    `project="My Project"`,
		},
		{
			name:        "ssh remote of a project with a space written as a space, in the layout path",
			layoutPath:  "dev.azure.com/acme/My Project/widget",
			remote:      "git@ssh.dev.azure.com:v3/acme/My Project/widget",
			wantProfile: "spaced",
			wantRule:    `project="My Project"`,
		},
		{
			name:        "https remote of a project with a space written as a space, outside the layout path",
			remote:      "https://dev.azure.com/acme/My Project/_git/widget",
			wantProfile: "spaced",
			wantRule:    `project="My Project"`,
		},
		{
			name:        "https remote of a project with a space written %20, outside the layout path",
			remote:      "https://dev.azure.com/acme/My%20Project/_git/widget",
			wantProfile: "spaced",
			wantRule:    `project="My Project"`,
		},
		{
			name:        "ssh remote of a project with a space written %20, outside the layout path",
			remote:      "git@ssh.dev.azure.com:v3/acme/My%20Project/widget",
			wantProfile: "spaced",
			wantRule:    `project="My Project"`,
		},
		{
			name:        "ssh remote of a project with a space written as a space, outside the layout path",
			remote:      "git@ssh.dev.azure.com:v3/acme/My Project/widget",
			wantProfile: "spaced",
			wantRule:    `project="My Project"`,
		},
		{
			name:        "https remote of a project with parentheses, minimally encoded, in the layout path",
			layoutPath:  "dev.azure.com/acme/Billing (EU)/widget",
			remote:      "https://dev.azure.com/acme/Billing%20(EU)/_git/widget",
			wantProfile: "paren",
			wantRule:    `project="Billing (EU)"`,
		},
		{
			name:        "ssh remote of a project with parentheses, fully encoded, outside the layout path",
			remote:      "git@ssh.dev.azure.com:v3/acme/Billing%20%28EU%29/widget",
			wantProfile: "paren",
			wantRule:    `project="Billing (EU)"`,
		},
		{
			name:        "ssh remote of a project with parentheses, written literally, outside the layout path",
			remote:      "git@ssh.dev.azure.com:v3/acme/Billing (EU)/widget",
			wantProfile: "paren",
			wantRule:    `project="Billing (EU)"`,
		},
		{
			name:        "https remote of a project with an ampersand, in the layout path",
			layoutPath:  "dev.azure.com/acme/R&D/widget",
			remote:      "https://dev.azure.com/acme/R&D/_git/widget",
			wantProfile: "rnd",
			wantRule:    `project="R&D"`,
		},
		{
			name:        "https remote of a project with an encoded ampersand, outside the layout path",
			remote:      "https://dev.azure.com/acme/R%26D/_git/widget",
			wantProfile: "rnd",
			wantRule:    `project="R&D"`,
		},
		{
			name:        "ssh remote of a project with an encoded ampersand, outside the layout path",
			remote:      "git@ssh.dev.azure.com:v3/acme/R%26D/widget",
			wantProfile: "rnd",
			wantRule:    `project="R&D"`,
		},
		{
			name:        "https remote of a project with ! @ ^ and ~, strictly encoded, outside the layout path",
			remote:      "https://dev.azure.com/acme/Ops%21%20%40%5E~%20Team/_git/widget",
			wantProfile: "bang",
			wantRule:    `project="Ops! @^~ Team"`,
		},
		{
			name:        "ssh remote of a project with an ampersand, outside the layout path",
			remote:      "git@ssh.dev.azure.com:v3/acme/R&D/widget",
			wantProfile: "rnd",
			wantRule:    `project="R&D"`,
		},
		{
			name:        "https remote of a project with ! @ ^ and ~, in the layout path",
			layoutPath:  "dev.azure.com/acme/Ops! @^~ Team/widget",
			remote:      "https://dev.azure.com/acme/Ops!%20%40^~%20Team/_git/widget",
			wantProfile: "bang",
			wantRule:    `project="Ops! @^~ Team"`,
		},
		{
			name:        "https remote of a project with ! @ ^ and ~, @ encoded, outside the layout path",
			remote:      "https://dev.azure.com/acme/Ops!%20%40^~%20Team/_git/widget",
			wantProfile: "bang",
			wantRule:    `project="Ops! @^~ Team"`,
		},
		{
			name:        "ssh remote of a project with ! @ ^ and ~, minimally encoded, outside the layout path",
			remote:      "git@ssh.dev.azure.com:v3/acme/Ops!%20@^~%20Team/widget",
			wantProfile: "bang",
			wantRule:    `project="Ops! @^~ Team"`,
		},
		{
			name:        "ssh remote of a project with ! @ ^ and ~, fully encoded, outside the layout path",
			remote:      "git@ssh.dev.azure.com:v3/acme/Ops%21%20@%5E~%20Team/widget",
			wantProfile: "bang",
			wantRule:    `project="Ops! @^~ Team"`,
		},
		{
			name:        "https remote of a non ASCII project with upper case hex, outside the layout path",
			remote:      "https://dev.azure.com/acme/Caf%C3%A9/_git/widget",
			wantProfile: "cafe",
			wantRule:    `project="Café"`,
		},
		{
			name:        "https remote of a non ASCII project with lower case hex, outside the layout path",
			remote:      "https://dev.azure.com/acme/Caf%c3%a9/_git/widget",
			wantProfile: "cafe",
			wantRule:    `project="Café"`,
		},
		{
			name:        "ssh remote of a non ASCII project with lower case hex, outside the layout path",
			remote:      "git@ssh.dev.azure.com:v3/acme/Caf%c3%a9/widget",
			wantProfile: "cafe",
			wantRule:    `project="Café"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := setupEnv(t)
			writeFile(t, filepath.Join(home, ".gitcd.yaml"), projectRulesConfig)

			repo := filepath.Join(home, "elsewhere", "checkout")
			if tt.layoutPath != "" {
				repo = filepath.Join(home, "Code", filepath.FromSlash(tt.layoutPath))
			}
			initRepository(t, repo)
			addRemote(t, repo, tt.remote)
			applyAndInclude(t, home)

			if got := runGitOutput(t, repo, "config", "user.email"); got != tt.wantProfile+"@example.test" {
				t.Errorf("git user.email = %q, want the profile %q", got, tt.wantProfile)
			}

			var stdout, stderr bytes.Buffer
			if code := run([]string{"profile", "show", repo}, &stdout, &stderr); code != 0 {
				t.Fatalf("profile show = %d, want 0, stderr: %q", code, stderr.String())
			}
			if want := "expected profile: " + tt.wantProfile + " ("; !strings.Contains(stdout.String(), want) {
				t.Errorf("profile show stdout = %q, want it to contain %q", stdout.String(), want)
			}
			if !strings.Contains(stdout.String(), tt.wantRule) {
				t.Errorf("profile show stdout = %q, want it to name the rule %q", stdout.String(), tt.wantRule)
			}
			if !strings.Contains(stdout.String(), "user.email: "+tt.wantProfile+"@example.test") {
				t.Errorf("profile show stdout = %q, want the effective email of profile %q", stdout.String(), tt.wantProfile)
			}

			if tt.layoutPath == "" {
				return
			}
			stdout.Reset()
			stderr.Reset()
			code := run([]string{"doctor"}, &stdout, &stderr)
			if strings.Contains(stdout.String(), "identity-drift") || code != 0 {
				t.Errorf("doctor = %d, want 0 and no identity drift, stdout: %q, stderr: %q", code, stdout.String(), stderr.String())
			}
		})
	}
}

// TestProjectRule_DoctorReportsNoDriftForEitherRepository puts a repository
// of the rule's project and one of another project of the organisation under
// one root, the case issue 85 reported: doctor flagged the first one.
func TestProjectRule_DoctorReportsNoDriftForEitherRepository(t *testing.T) {
	home := setupEnv(t)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), projectRulesConfig)

	inProject := filepath.Join(home, "Code", "dev.azure.com", "acme", "billing", "widget")
	other := filepath.Join(home, "Code", "dev.azure.com", "acme", "ops", "gadget")
	initRepository(t, inProject)
	addRemote(t, inProject, "https://dev.azure.com/acme/billing/_git/widget")
	initRepository(t, other)
	addRemote(t, other, "git@ssh.dev.azure.com:v3/acme/ops/gadget")
	applyAndInclude(t, home)

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor"}, &stdout, &stderr)
	if code != 0 || strings.Contains(stdout.String(), "identity-drift") {
		t.Fatalf("doctor = %d, want 0 and no identity drift, stdout: %q, stderr: %q", code, stdout.String(), stderr.String())
	}
}

// TestProjectRule_CloneOfAnEscapedShorthandBuildsOneEscape clones the bare
// shorthand with %20 in the project. git rewrites only the exact HTTPS URL
// with a single %20 to a local repository, so the clone succeeds only when
// the URL the command builds is not double encoded.
func TestProjectRule_CloneOfAnEscapedShorthandBuildsOneEscape(t *testing.T) {
	home := setupEnv(t)
	source := filepath.Join(home, "source")
	initRepository(t, source)
	runGit(t, home, "config", "--global",
		"url."+testenv.FileURL(source)+".insteadOf", "https://dev.azure.com/acme/my%20project/_git/svc")

	var stdout, stderr bytes.Buffer
	code := run([]string{"clone", "dev.azure.com/acme/my%20project/svc"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("clone = %d, want 0, stderr: %q", code, stderr.String())
	}
	want := filepath.Join(home, "Code", "dev.azure.com", "acme", "my project", "svc")
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Errorf("clone stdout = %q, want %q", got, want)
	}
}

func TestProjectRule_OwnerWithSlashIsRefused(t *testing.T) {
	home := setupEnv(t)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  billing:
    user.email: billing@example.test

rules:
  - host: dev.azure.com
    owner: acme/billing
    profile: billing
`)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"apply"}, &stdout, &stderr); code == 0 {
		t.Fatalf("apply = 0, want a refusal, stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "project") {
		t.Errorf("apply stderr = %q, want the refusal to name the project field", stderr.String())
	}
}
