// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/testenv"
)

// The Azure DevOps clone dialog hands out an HTTPS URL that carries the
// organisation as the URL user, no secret. People also put a token into the
// URL in other ways. gitcd shows the documented form as it is and treats
// every other userinfo as a credential. These tests pin both sides.
const (
	azureToken         = "TOKEN-PLACEHOLDER-123"
	azureHTTPSWithUser = "https://ORG@dev.azure.com/ORG/PROJECT/_git/REPO"
	azureSSH           = "git@ssh.dev.azure.com:v3/ORG/PROJECT/REPO"
	azureWithheld      = "its output is withheld because the remote carries a credential"
)

// azureRemote is the repository's own path under the default root.
func azureRepoPath(home string) string {
	return filepath.Join(home, "Code", "dev.azure.com", "ORG", "PROJECT", "REPO")
}

// redirectToMissing makes git resolve url to a path that does not exist, so
// a fetch or clone fails fast and offline instead of reaching dev.azure.com.
func redirectToMissing(t *testing.T, home, url string) {
	t.Helper()
	missing := filepath.Join(home, "missing")
	runGit(t, home, "config", "--global", "url."+testenv.FileURL(missing)+".insteadOf", url)
}

func TestAzureDevOpsDocumentedRemote(t *testing.T) {
	home := setupEnv(t)
	repo := azureRepoPath(home)
	initRepository(t, repo)
	addRemote(t, repo, azureHTTPSWithUser)
	redirectToMissing(t, home, azureHTTPSWithUser)

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
		noStderr   string
	}{
		{
			name:       "profile show keeps the organisation",
			args:       []string{"profile", "show", repo},
			wantStdout: "remote: " + azureHTTPSWithUser,
		},
		{
			name:       "list keeps the organisation",
			args:       []string{"list", "--json"},
			wantStdout: `"remote": "` + azureHTTPSWithUser + `"`,
		},
		{
			name:       "open renders the web page",
			args:       []string{"open", "--print", "REPO"},
			wantStdout: "https://dev.azure.com/ORG/PROJECT/_git/REPO\n",
		},
		{
			name:       "adopt finds the repository at its rendered path",
			args:       []string{"adopt", "--dry-run", repo},
			wantStderr: "already at the path the configured layout renders",
		},
		{
			name:       "clone of the same repository in the ssh form finds it",
			args:       []string{"clone", azureSSH},
			wantStdout: repo + "\n",
		},
		{
			name:       "clone of the same repository in the https form finds it",
			args:       []string{"clone", azureHTTPSWithUser},
			wantStdout: repo + "\n",
		},
		{
			name:     "sync keeps git's output",
			args:     []string{"sync"},
			wantCode: 1,
			noStderr: azureWithheld,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Fatalf("%v = %d, want %d, stdout: %q, stderr: %q", tt.args, code, tt.wantCode, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantStdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
			if tt.noStderr != "" && strings.Contains(stderr.String(), tt.noStderr) {
				t.Errorf("stderr = %q, want git's output kept", stderr.String())
			}
		})
	}
}

// TestAzureDevOpsClone_KeepsGitOutputOnlyForTheDocumentedForm clones a
// repository that is not there, through a rewrite to a path that does not
// exist, and checks what the error holds.
func TestAzureDevOpsClone_KeepsGitOutputOnlyForTheDocumentedForm(t *testing.T) {
	tests := []struct {
		name         string
		url          string
		wantWithheld bool
	}{
		{name: "documented form", url: azureHTTPSWithUser},
		{name: "user and token", url: "https://USER:" + azureToken + "@dev.azure.com/ORG/PROJECT/_git/REPO", wantWithheld: true},
		{name: "token in the user slot", url: "https://" + azureToken + "@dev.azure.com/ORG/PROJECT/_git/REPO", wantWithheld: true},
		{name: "empty user and token", url: "https://:" + azureToken + "@dev.azure.com/ORG/PROJECT/_git/REPO", wantWithheld: true},
		{name: "organisation and token", url: "https://ORG:" + azureToken + "@dev.azure.com/ORG/PROJECT/_git/REPO", wantWithheld: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := setupEnv(t)
			redirectToMissing(t, home, tt.url)

			var stdout, stderr bytes.Buffer
			code := run([]string{"clone", "--verbose", tt.url}, &stdout, &stderr)
			if code == 0 {
				t.Fatalf("clone = 0, want a failure, stderr: %q", stderr.String())
			}
			if got := strings.Contains(stderr.String(), azureWithheld); got != tt.wantWithheld {
				t.Errorf("withheld = %v, want %v, stderr: %q", got, tt.wantWithheld, stderr.String())
			}
			if strings.Contains(stdout.String()+stderr.String(), azureToken) {
				t.Errorf("output carries the token, stdout: %q, stderr: %q", stdout.String(), stderr.String())
			}
		})
	}
}

// TestAzureDevOpsTokenForms_NoCommandPrintsTheToken runs every command that
// reads or takes a remote against each way of putting a token into the URL.
func TestAzureDevOpsTokenForms_NoCommandPrintsTheToken(t *testing.T) {
	forms := []struct {
		name string
		url  string
	}{
		{name: "user and token", url: "https://USER:" + azureToken + "@dev.azure.com/ORG/PROJECT/_git/REPO"},
		{name: "token in the user slot", url: "https://" + azureToken + "@dev.azure.com/ORG/PROJECT/_git/REPO"},
		{name: "empty user and token", url: "https://:" + azureToken + "@dev.azure.com/ORG/PROJECT/_git/REPO"},
		{name: "organisation and token", url: "https://ORG:" + azureToken + "@dev.azure.com/ORG/PROJECT/_git/REPO"},
	}
	for _, tt := range forms {
		t.Run(tt.name, func(t *testing.T) {
			home := setupEnv(t)
			repo := azureRepoPath(home)
			initRepository(t, repo)
			addRemote(t, repo, tt.url)
			redirectToMissing(t, home, tt.url)

			commands := map[string][]string{
				"clone":        {"clone", "--verbose", tt.url},
				"clone dry":    {"clone", "--dry-run", "--verbose", tt.url},
				"path":         {"path", "--verbose", tt.url},
				"profile show": {"profile", "show", repo},
				"open":         {"open", "--print", "--verbose", "REPO"},
				"list":         {"list", "--verbose"},
				"list json":    {"list", "--json", "--verbose"},
				"doctor":       {"doctor", "--verbose"},
				"adopt":        {"adopt", "--dry-run", "--verbose", repo},
				"sync":         {"sync", "--verbose"},
				"status":       {"status", "--verbose"},
			}
			for name, args := range commands {
				var stdout, stderr bytes.Buffer
				run(args, &stdout, &stderr)
				if strings.Contains(stdout.String()+stderr.String(), azureToken) {
					t.Errorf("%s: output carries the token, stdout: %q, stderr: %q", name, stdout.String(), stderr.String())
				}
			}

			var stdout, stderr bytes.Buffer
			run([]string{"sync"}, &stdout, &stderr)
			if !strings.Contains(stderr.String(), azureWithheld) {
				t.Errorf("sync: git's output is not withheld, stderr: %q", stderr.String())
			}
		})
	}
}

// TestAzureDevOpsSync_WithholdsWhenAnyConfiguredUrlCarriesAToken puts a
// token into a second URL of a repository whose origin is the documented
// form: a push URL, another remote and a rewrite rule.
func TestAzureDevOpsSync_WithholdsWhenAnyConfiguredUrlCarriesAToken(t *testing.T) {
	tokenURL := "https://" + azureToken + "@dev.azure.com/ORG/PROJECT/_git/OTHER"
	tests := []struct {
		name  string
		setup func(t *testing.T, repo string)
	}{
		{name: "push url", setup: func(t *testing.T, repo string) {
			runGit(t, repo, "config", "remote.origin.pushurl", tokenURL)
		}},
		{name: "another remote", setup: func(t *testing.T, repo string) {
			runGit(t, repo, "remote", "add", "other", tokenURL)
		}},
		{name: "rewrite rule base", setup: func(t *testing.T, repo string) {
			runGit(t, repo, "config", "url."+tokenURL+".insteadOf", "https://elsewhere.test/")
		}},
		{name: "rewrite rule prefix", setup: func(t *testing.T, repo string) {
			runGit(t, repo, "config", "url.https://elsewhere.test/.insteadOf", tokenURL)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := setupEnv(t)
			repo := azureRepoPath(home)
			initRepository(t, repo)
			addRemote(t, repo, azureHTTPSWithUser)
			redirectToMissing(t, home, azureHTTPSWithUser)
			tt.setup(t, repo)

			var stdout, stderr bytes.Buffer
			if code := run([]string{"sync"}, &stdout, &stderr); code == 0 {
				t.Fatalf("sync = 0, want a failure, stderr: %q", stderr.String())
			}
			if !strings.Contains(stderr.String(), azureWithheld) {
				t.Errorf("git's output is not withheld, stderr: %q", stderr.String())
			}
			if strings.Contains(stdout.String()+stderr.String(), azureToken) {
				t.Errorf("output carries the token, stderr: %q", stderr.String())
			}
		})
	}
}

// TestAzureDevOpsFormsLandInOneDirectory dry runs clone for the clone dialog
// forms and requires the same destination.
func TestAzureDevOpsFormsLandInOneDirectory(t *testing.T) {
	home := setupEnv(t)
	want := azureRepoPath(home) + "\n"
	inputs := []string{
		azureHTTPSWithUser,
		"https://dev.azure.com/ORG/PROJECT/_git/REPO",
		azureSSH,
		"https://USER:" + azureToken + "@dev.azure.com/ORG/PROJECT/_git/REPO",
		"https://" + azureToken + "@dev.azure.com/ORG/PROJECT/_git/REPO",
		"https://:" + azureToken + "@dev.azure.com/ORG/PROJECT/_git/REPO",
	}
	for _, input := range inputs {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"clone", "--dry-run", input}, &stdout, &stderr); code != 0 {
			t.Fatalf("clone --dry-run = %d, stderr: %q", code, stderr.String())
		}
		if stdout.String() != want {
			t.Errorf("clone --dry-run printed %q, want %q", stdout.String(), want)
		}
	}
}
