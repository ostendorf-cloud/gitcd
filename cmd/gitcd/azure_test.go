// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// The Azure DevOps clone dialog hands out an HTTPS URL that carries the
// organisation as the URL user. gitcd cannot tell it from a token used as a
// user name, so it treats it as a credential: displayed text shows "***" for
// the user and git's own output is withheld. These tests pin that fail
// closed behaviour and that the repository stays fully usable.
const (
	azureHTTPSWithUser = "https://ORG@dev.azure.com/ORG/PROJECT/_git/REPO"
	azureSSH           = "git@ssh.dev.azure.com:v3/ORG/PROJECT/REPO"
)

func TestAzureDevOpsUserinfoRemote(t *testing.T) {
	home := setupEnv(t)
	repo := filepath.Join(home, "Code", "dev.azure.com", "ORG", "PROJECT", "REPO")
	initRepository(t, repo)
	addRemote(t, repo, azureHTTPSWithUser)

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "profile show masks the user",
			args:       []string{"profile", "show", repo},
			wantStdout: "remote: https://***@dev.azure.com/ORG/PROJECT/_git/REPO",
		},
		{
			name:       "list masks the user",
			args:       []string{"list", "--json"},
			wantStdout: `"remote": "https://***@dev.azure.com/ORG/PROJECT/_git/REPO"`,
		},
		{
			name:       "open renders the web page without the user",
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
			name:       "sync fails closed and withholds git's output",
			args:       []string{"sync"},
			wantCode:   1,
			wantStderr: "its output is withheld because the remote carries a credential",
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
		})
	}
}

// TestAzureDevOpsFormsLandInOneDirectory dry runs clone for both clone
// dialog forms and requires the same destination.
func TestAzureDevOpsFormsLandInOneDirectory(t *testing.T) {
	home := setupEnv(t)
	want := filepath.Join(home, "Code", "dev.azure.com", "ORG", "PROJECT", "REPO") + "\n"
	for _, input := range []string{azureHTTPSWithUser, "https://dev.azure.com/ORG/PROJECT/_git/REPO", azureSSH} {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"clone", "--dry-run", input}, &stdout, &stderr); code != 0 {
			t.Fatalf("clone --dry-run %s = %d, stderr: %q", input, code, stderr.String())
		}
		if stdout.String() != want {
			t.Errorf("clone --dry-run %s printed %q, want %q", input, stdout.String(), want)
		}
	}
}
