// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package remote_test

import (
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/remote"
)

var azureAliases = map[string]string{
	"ssh.dev.azure.com":       "dev.azure.com",
	"vs-ssh.visualstudio.com": "dev.azure.com",
}

// TestParse_AzureDevOpsProjectWithSpace requires every spelling of one
// repository to parse to the same project, whichever way the space is
// written, since a rule and a layout path both compare against it.
func TestParse_AzureDevOpsProjectWithSpace(t *testing.T) {
	t.Parallel()

	want := remote.Remote{Host: "dev.azure.com", Owner: "acme", Project: "My Project", Repo: "widget"}
	tests := []struct {
		name  string
		input string
	}{
		{name: "https with the space percent encoded", input: "https://dev.azure.com/acme/My%20Project/_git/widget"},
		{name: "https with the organisation as user", input: "https://acme@dev.azure.com/acme/My%20Project/_git/widget"},
		{name: "https with a literal space", input: "https://dev.azure.com/acme/My Project/_git/widget"},
		{name: "ssh with the space percent encoded", input: "git@ssh.dev.azure.com:v3/acme/My%20Project/widget"},
		{name: "ssh with a literal space", input: "git@ssh.dev.azure.com:v3/acme/My Project/widget"},
		{name: "legacy ssh host with the space percent encoded", input: "git@vs-ssh.visualstudio.com:v3/acme/My%20Project/widget"},
		{name: "shorthand with a literal space", input: "dev.azure.com/acme/My Project/widget"},
		{name: "shorthand with the space percent encoded", input: "dev.azure.com/acme/My%20Project/widget"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := remote.Parse(tt.input, azureAliases)
			if err != nil {
				t.Fatalf("Parse(%q) returned an unexpected error: %v", tt.input, err)
			}
			if got != want {
				t.Fatalf("Parse(%q) = %+v, want %+v", tt.input, got, want)
			}
		})
	}
}

func TestRemote_URL_EncodesAProjectWithASpace(t *testing.T) {
	t.Parallel()

	r := remote.Remote{Host: "dev.azure.com", Owner: "acme", Project: "My Project", Repo: "widget"}
	got, err := r.URL("https")
	if err != nil {
		t.Fatalf("URL() returned an unexpected error: %v", err)
	}
	if want := "https://dev.azure.com/acme/My%20Project/_git/widget"; got != want {
		t.Fatalf("URL() = %q, want %q", got, want)
	}
	back, err := remote.Parse(got, nil)
	if err != nil || back != r {
		t.Fatalf("Parse(URL()) = %+v, %v, want %+v", back, err, r)
	}
}

// TestRemote_URL_OfAShorthandWithAnEscapedProject requires the URL built
// from a shorthand that already carries %20 to hold one escape, not the
// double encoded %2520.
func TestRemote_URL_OfAShorthandWithAnEscapedProject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		protocol  string
		wantURL   string
		shorthand string
	}{
		{
			name:      "https from an escaped shorthand",
			protocol:  "https",
			shorthand: "dev.azure.com/acme/my%20project/svc",
			wantURL:   "https://dev.azure.com/acme/my%20project/_git/svc",
		},
		{
			name:      "ssh from an escaped shorthand",
			protocol:  "ssh",
			shorthand: "dev.azure.com/acme/my%20project/svc",
			wantURL:   "git@ssh.dev.azure.com:v3/acme/my%20project/svc",
		},
		{
			name:      "https from a shorthand with a literal space",
			protocol:  "https",
			shorthand: "dev.azure.com/acme/my project/svc",
			wantURL:   "https://dev.azure.com/acme/my%20project/_git/svc",
		},
		{
			name:      "https from a shorthand with no escape",
			protocol:  "https",
			shorthand: "dev.azure.com/acme/billing/svc",
			wantURL:   "https://dev.azure.com/acme/billing/_git/svc",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, err := remote.Parse(tt.shorthand, nil)
			if err != nil {
				t.Fatalf("Parse(%q) returned an unexpected error: %v", tt.shorthand, err)
			}
			got, err := r.URL(tt.protocol)
			if err != nil {
				t.Fatalf("URL(%q) returned an unexpected error: %v", tt.protocol, err)
			}
			if got != tt.wantURL {
				t.Fatalf("URL(%q) = %q, want %q", tt.protocol, got, tt.wantURL)
			}
		})
	}
}
