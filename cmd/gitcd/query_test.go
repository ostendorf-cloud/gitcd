// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// A query or a fragment of a remote is masked in everything gitcd prints,
// and git's own output is withheld for such a remote, as for userinfo.
const queryMarker = "FAKEQUERYMARK"

var queryRemotes = []struct {
	name string
	url  string
	want string
	// repo is the repository's name, "widget" when empty.
	repo string
	// noClone leaves the form out of the clone test, where gitcd would
	// build a clean URL from it or the input cannot be passed on.
	noClone bool
}{
	{name: "https query", url: "https://h.test/acme/widget.git?a=" + queryMarker, want: "https://h.test/acme/widget.git?***"},
	{name: "https fragment", url: "https://h.test/acme/widget.git#" + queryMarker, want: "https://h.test/acme/widget.git#***"},
	{name: "https both", url: "https://h.test/acme/widget.git?a=b#" + queryMarker, want: "https://h.test/acme/widget.git?***"},
	{name: "ssh plain user", url: "ssh://git@h.test/acme/widget.git?a=" + queryMarker, want: "ssh://git@h.test/acme/widget.git?***"},
	{name: "azure documented form", url: azureHTTPSWithUser + "?a=" + queryMarker, want: "https://***@dev.azure.com/ORG/PROJECT/_git/REPO?***"},
	{name: "scp style with a user", url: "git@h.test:acme/widget.git?a=" + queryMarker, want: "git@h.test:acme/widget.git?***"},
	{name: "scp style with a fragment", url: "git@h.test:acme/widget.git#" + queryMarker, want: "git@h.test:acme/widget.git#***"},
	{name: "scp style without a user", url: "h.test:acme/widget.git?a=" + queryMarker, want: "h.test:acme/widget.git?***"},
	{name: "scp style with an unusual user", url: "my+user@h.test:acme/widget.git?a=" + queryMarker, want: "my+user@h.test:acme/widget.git?***"},
	{name: "shorthand", url: "h.test/acme/widget?a=" + queryMarker, want: "h.test/acme/widget?***", noClone: true},
	{
		name: "path with a space", url: "git@h.test:acme/wid get.git?a=b " + queryMarker,
		want: "git@h.test:acme/wid get.git?***", repo: "wid get", noClone: true,
	},
	{
		name: "trailing newline", url: "git@h.test:acme/widget.git?a=" + queryMarker + "\n",
		want: "git@h.test:acme/widget.git?***", noClone: true,
	},
}

func queryRepoPath(home, url, repo string) string {
	if strings.Contains(url, "dev.azure.com") {
		return azureRepoPath(home)
	}
	if repo == "" {
		repo = "widget"
	}
	return filepath.Join(home, "Code", "h.test", "acme", repo)
}

func TestCommands_NoOutputCarriesTheQueryOfARemote(t *testing.T) {
	for _, tt := range queryRemotes {
		t.Run(tt.name, func(t *testing.T) {
			home := setupEnv(t)
			repo := queryRepoPath(home, tt.url, tt.repo)
			initRepository(t, repo)
			addRemote(t, repo, tt.url)
			redirectToMissing(t, home, tt.url)

			commands := map[string][]string{
				"clone":        {"clone", "--verbose", tt.url},
				"clone dry":    {"clone", "--dry-run", "--verbose", tt.url},
				"profile show": {"profile", "show", repo},
				"open":         {"open", "--print", "--verbose", repo},
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
				if strings.Contains(stdout.String()+stderr.String(), queryMarker) {
					t.Errorf("%s: output carries the query, stdout: %q, stderr: %q", name, stdout.String(), stderr.String())
				}
			}
		})
	}
}

func TestCommands_ShowTheMaskedQuery(t *testing.T) {
	for _, tt := range queryRemotes {
		t.Run(tt.name, func(t *testing.T) {
			home := setupEnv(t)
			repo := queryRepoPath(home, tt.url, tt.repo)
			initRepository(t, repo)
			addRemote(t, repo, tt.url)

			var stdout, stderr bytes.Buffer
			if code := run([]string{"profile", "show", repo}, &stdout, &stderr); code != 0 {
				t.Fatalf("profile show = %d, stderr: %q", code, stderr.String())
			}
			if !strings.Contains(stdout.String(), "remote: "+tt.want) {
				t.Errorf("profile show stdout = %q, want it to contain %q", stdout.String(), "remote: "+tt.want)
			}

			stdout.Reset()
			stderr.Reset()
			if code := run([]string{"list", "--json"}, &stdout, &stderr); code != 0 {
				t.Fatalf("list --json = %d, stderr: %q", code, stderr.String())
			}
			if !strings.Contains(stdout.String(), `"remote": "`+tt.want+`"`) {
				t.Errorf("list --json stdout = %q, want it to contain %q", stdout.String(), tt.want)
			}
		})
	}
}

// TestClone_DestinationIgnoresTheQuery checks that the path a clone would
// use, and the path adopt and doctor expect, never holds the query.
func TestClone_DestinationIgnoresTheQuery(t *testing.T) {
	for _, tt := range queryRemotes {
		t.Run(tt.name, func(t *testing.T) {
			home := setupEnv(t)
			repo := queryRepoPath(home, tt.url, tt.repo)

			var stdout, stderr bytes.Buffer
			if code := run([]string{"clone", "--dry-run", "--verbose", tt.url}, &stdout, &stderr); code != 0 {
				t.Fatalf("clone --dry-run = %d, stderr: %q", code, stderr.String())
			}
			if stdout.String() != repo+"\n" {
				t.Errorf("clone --dry-run printed %q, want %q", stdout.String(), repo+"\n")
			}

			initRepository(t, repo)
			addRemote(t, repo, tt.url)
			for _, args := range [][]string{{"adopt", "--dry-run", repo}, {"doctor"}} {
				stdout.Reset()
				stderr.Reset()
				run(args, &stdout, &stderr)
				if strings.Contains(stdout.String()+stderr.String(), queryMarker) {
					t.Errorf("%v: output carries the query, stdout: %q, stderr: %q", args, stdout.String(), stderr.String())
				}
				if strings.Contains(stdout.String()+stderr.String(), "path-mismatch") {
					t.Errorf("%v: reports a path mismatch, stdout: %q, stderr: %q", args, stdout.String(), stderr.String())
				}
			}
			stdout.Reset()
			stderr.Reset()
			run([]string{"adopt", "--dry-run", repo}, &stdout, &stderr)
			if !strings.Contains(stderr.String(), "already at the path the configured layout renders") {
				t.Errorf("adopt: stderr = %q, want the repository found at its rendered path", stderr.String())
			}
		})
	}
}

func TestSyncAndClone_WithholdGitOutputForAQuery(t *testing.T) {
	for _, tt := range queryRemotes {
		t.Run(tt.name, func(t *testing.T) {
			home := setupEnv(t)
			repo := queryRepoPath(home, tt.url, tt.repo)
			initRepository(t, repo)
			addRemote(t, repo, tt.url)
			redirectToMissing(t, home, tt.url)

			var stdout, stderr bytes.Buffer
			if code := run([]string{"sync"}, &stdout, &stderr); code == 0 {
				t.Fatalf("sync = 0, want a failure, stderr: %q", stderr.String())
			}
			if !strings.Contains(stderr.String(), azureWithheld) {
				t.Errorf("sync: git's output is not withheld, stderr: %q", stderr.String())
			}
		})
		t.Run(tt.name+" clone", func(t *testing.T) {
			if tt.noClone {
				t.Skip("gitcd clones a clean URL built from this form, or cannot pass it on")
			}
			home := setupEnv(t)
			redirectToMissing(t, home, tt.url)

			var stdout, stderr bytes.Buffer
			if code := run([]string{"clone", tt.url}, &stdout, &stderr); code == 0 {
				t.Fatalf("clone = 0, want a failure, stderr: %q", stderr.String())
			}
			if !strings.Contains(stderr.String(), azureWithheld) {
				t.Errorf("clone: git's output is not withheld, stderr: %q", stderr.String())
			}
		})
	}
}

func TestSync_WithholdsWhenAConfiguredUrlHasAQuery(t *testing.T) {
	queryURL := "https://h.test/acme/other.git?a=" + queryMarker
	tests := []struct {
		name  string
		setup func(t *testing.T, repo string)
	}{
		{name: "push url", setup: func(t *testing.T, repo string) {
			runGit(t, repo, "config", "remote.origin.pushurl", queryURL)
		}},
		{name: "another remote", setup: func(t *testing.T, repo string) {
			runGit(t, repo, "remote", "add", "other", queryURL)
		}},
		{name: "rewrite rule base", setup: func(t *testing.T, repo string) {
			runGit(t, repo, "config", "url."+queryURL+".insteadOf", "https://elsewhere.test/")
		}},
		{name: "rewrite rule prefix", setup: func(t *testing.T, repo string) {
			runGit(t, repo, "config", "url.https://elsewhere.test/.insteadOf", queryURL)
		}},
		{name: "push rewrite rule prefix", setup: func(t *testing.T, repo string) {
			runGit(t, repo, "config", "url.https://elsewhere.test/.pushInsteadOf", queryURL)
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
			if strings.Contains(stdout.String()+stderr.String(), queryMarker) {
				t.Errorf("output carries the query, stderr: %q", stderr.String())
			}
		})
	}
}
