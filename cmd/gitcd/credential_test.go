// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// TestExistingTarget_NeverPrintsTheRemoteItHolds covers the two messages
// that name the remote of a directory a clone or an adopt would land in:
// clone onto an existing target and the adopt dry run onto it.
func TestExistingTarget_NeverPrintsTheRemoteItHolds(t *testing.T) {
	remotes := []struct {
		name string
		url  string
	}{
		{name: "question mark", url: "https://user:FAKE?SECRET@h.test/acme/widget.git"},
		{name: "number sign", url: "https://user:FAKE#SECRET@h.test/acme/widget.git"},
		{name: "slash before the at sign", url: "https://user:FAKESECRET/@h.test/acme/widget.git"},
		{name: "parsable credential", url: "https://user:FAKESECRET@h.test/acme/other.git"},
	}
	for _, tt := range remotes {
		t.Run(tt.name, func(t *testing.T) {
			home := setupEnv(t)
			target := filepath.Join(home, "Code", "h.test", "acme", "widget")
			initRepository(t, target)
			addRemote(t, target, tt.url)
			source := filepath.Join(home, "elsewhere", "widget")
			initRepository(t, source)
			addRemote(t, source, "https://h.test/acme/widget.git")

			commands := map[string][]string{
				"clone": {"clone", "https://h.test/acme/widget.git"},
				"adopt": {"adopt", "--dry-run", source},
			}
			for name, args := range commands {
				var stdout, stderr bytes.Buffer
				if code := run(args, &stdout, &stderr); code == 0 {
					t.Errorf("%s = 0, want a failure for a target with another remote", name)
				}
				for _, part := range []string{"FAKE", "SECRET"} {
					if strings.Contains(stdout.String(), part) || strings.Contains(stderr.String(), part) {
						t.Errorf("%s: output carries %q, stdout: %q, stderr: %q", name, part, stdout.String(), stderr.String())
					}
				}
			}
		})
	}
}

// TestCommands_NoOutputQuotesARemoteThatCannotBeParsed runs every command
// that reads or takes a remote against remotes that cannot be parsed, with
// a placeholder credential in the user name or password, and checks that
// no part of the credential reaches stdout or stderr.
func TestCommands_NoOutputQuotesARemoteThatCannotBeParsed(t *testing.T) {
	remotes := []struct {
		name string
		url  string
	}{
		{name: "question mark", url: "https://user:FAKE?SECRET@h.test/acme/widget.git"},
		{name: "number sign", url: "https://user:FAKE#SECRET@h.test/acme/widget.git"},
		{name: "space", url: "https://user:FAKE SECRET@h.test/acme/widget.git"},
		{name: "tab", url: "https://user:FAKE\tSECRET@h.test/acme/widget.git"},
		{name: "trailing slash", url: "https://user:FAKESECRET/@h.test/acme/widget.git"},
		{name: "slash in the middle", url: "https://user:FAKE/SECRET@h.test/acme/widget.git"},
		{name: "question mark in a user name", url: "https://FAKE?SECRET@h.test/acme/widget.git"},
		{name: "token as user name", url: "https://FAKESECRET@h.test/acme"},
	}
	for _, tt := range remotes {
		t.Run(tt.name, func(t *testing.T) {
			home := setupEnv(t)
			repo := filepath.Join(home, "Code", "h.test", "acme", "widget")
			initRepository(t, repo)
			addRemote(t, repo, tt.url)

			commands := map[string][]string{
				"clone":        {"clone", "--verbose", tt.url},
				"profile show": {"profile", "show", repo},
				"open":         {"open", "--print", "--verbose", repo},
				"list":         {"list", "--verbose"},
				"list json":    {"list", "--json", "--verbose"},
				"doctor":       {"doctor", "--verbose"},
				"adopt":        {"adopt", "--dry-run", "--verbose", repo},
				"cd":           {"cd", "--verbose", "widget"},
				"status":       {"status", "--verbose"},
			}
			for name, args := range commands {
				var stdout, stderr bytes.Buffer
				run(args, &stdout, &stderr)
				for _, part := range []string{"FAKE", "SECRET"} {
					if strings.Contains(stdout.String(), part) || strings.Contains(stderr.String(), part) {
						t.Errorf("%s with %s: output carries %q, stdout: %q, stderr: %q",
							name, tt.name, part, stdout.String(), stderr.String())
					}
				}
			}
		})
	}
}
