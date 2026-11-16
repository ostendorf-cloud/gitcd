// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package remote_test

import (
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/remote"
)

const azureToken = "TOKEN-PLACEHOLDER-123"

// TestParse_AzureDevOpsTokenForms parses every way a token is put into an
// Azure DevOps URL and requires the same remote as the documented form, and
// no part of the token in anything Parse or Redact return.
func TestParse_AzureDevOpsTokenForms(t *testing.T) {
	t.Parallel()

	want := remote.Remote{Host: "dev.azure.com", Owner: "ORG", Project: "PROJECT", Repo: "REPO"}
	tests := []struct {
		name  string
		input string
		// shown is what Redact must return.
		shown string
	}{
		{
			name:  "documented form, the organisation as user",
			input: "https://ORG@dev.azure.com/ORG/PROJECT/_git/REPO",
			shown: "https://ORG@dev.azure.com/ORG/PROJECT/_git/REPO",
		},
		{
			name:  "user and token",
			input: "https://USER:" + azureToken + "@dev.azure.com/ORG/PROJECT/_git/REPO",
			shown: "https://***@dev.azure.com/ORG/PROJECT/_git/REPO",
		},
		{
			name:  "token in the user slot",
			input: "https://" + azureToken + "@dev.azure.com/ORG/PROJECT/_git/REPO",
			shown: "https://***@dev.azure.com/ORG/PROJECT/_git/REPO",
		},
		{
			name:  "empty user and token",
			input: "https://:" + azureToken + "@dev.azure.com/ORG/PROJECT/_git/REPO",
			shown: "https://***@dev.azure.com/ORG/PROJECT/_git/REPO",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := remote.Parse(tt.input, nil)
			if err != nil {
				t.Fatalf("Parse(%q) returned an unexpected error: %v", tt.input, err)
			}
			if got != want {
				t.Fatalf("Parse(%q) = %+v, want %+v", tt.input, got, want)
			}
			for _, text := range []string{
				remote.Redact(tt.input),
				remote.Redact("fatal: unable to access '" + tt.input + "/': refused"),
				got.WebURL(),
			} {
				if strings.Contains(text, azureToken) {
					t.Errorf("output %q carries the token", text)
				}
			}
			if shown := remote.Redact(tt.input); shown != tt.shown {
				t.Errorf("Redact(%q) = %q, want %q", tt.input, shown, tt.shown)
			}
		})
	}
}

// TestParse_AzureDevOpsTokenFormErrorsDoNotQuoteTheToken feeds the token
// forms with a path Parse rejects and checks the errors.
func TestParse_AzureDevOpsTokenFormErrorsDoNotQuoteTheToken(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"https://USER:" + azureToken + "@dev.azure.com/ORG/PROJECT/REPO",
		"https://" + azureToken + "@dev.azure.com/ORG/_git/REPO",
		"https://:" + azureToken + "@dev.azure.com/",
		"https://" + azureToken + "@/ORG/PROJECT/_git/REPO",
		"https://USER:" + azureToken + "@dev.azure.com/ORG/PROJECT/_git/REPO?x=a@b",
	}
	for _, input := range inputs {
		_, err := remote.Parse(input, nil)
		if err != nil && strings.Contains(err.Error(), azureToken) {
			t.Errorf("Parse(%q) error %q quotes the token", input, err)
		}
	}
}

// TestRedact_AzureDevOpsExemption tries to smuggle a token through the one
// exemption: the https URL on dev.azure.com whose user, with no password,
// equals the organisation segment of the path.
func TestRedact_AzureDevOpsExemption(t *testing.T) {
	t.Parallel()

	const tail = "/PROJECT/_git/REPO"
	tests := []struct {
		name  string
		input string
		want  string
	}{
		// What is exempt.
		{name: "documented form", input: "https://ORG@dev.azure.com/ORG" + tail, want: "https://ORG@dev.azure.com/ORG" + tail},
		{name: "case differs", input: "https://org@dev.azure.com/ORG" + tail, want: "https://org@dev.azure.com/ORG" + tail},
		{name: "host in upper case", input: "https://ORG@DEV.AZURE.COM/ORG" + tail, want: "https://ORG@DEV.AZURE.COM/ORG" + tail},
		{name: "escaped user decodes to the organisation", input: "https://%4FRG@dev.azure.com/ORG" + tail, want: "https://%4FRG@dev.azure.com/ORG" + tail},
		{name: "escaped organisation segment", input: "https://ORG@dev.azure.com/%4FRG" + tail, want: "https://ORG@dev.azure.com/%4FRG" + tail},
		{name: "hyphen and dot in the organisation", input: "https://my-org.x@dev.azure.com/my-org.x" + tail, want: "https://my-org.x@dev.azure.com/my-org.x" + tail},
		{
			name:  "inside git's error text",
			input: "fatal: unable to access 'https://ORG@dev.azure.com/ORG" + tail + "/': refused",
			want:  "fatal: unable to access 'https://ORG@dev.azure.com/ORG" + tail + "/': refused",
		},
		// Everything else stays redacted.
		{name: "user equal to the organisation with a password", input: "https://ORG:" + azureToken + "@dev.azure.com/ORG" + tail, want: "https://***@dev.azure.com/ORG" + tail},
		{name: "user equal to the organisation with an empty password", input: "https://ORG:@dev.azure.com/ORG" + tail, want: "https://***@dev.azure.com/ORG" + tail},
		{name: "empty user with a password", input: "https://:" + azureToken + "@dev.azure.com/ORG" + tail, want: "https://***@dev.azure.com/ORG" + tail},
		{name: "empty user", input: "https://@dev.azure.com/ORG" + tail, want: "https://***@dev.azure.com/ORG" + tail},
		{name: "token in the user slot", input: "https://" + azureToken + "@dev.azure.com/ORG" + tail, want: "https://***@dev.azure.com/ORG" + tail},
		{name: "user differs from the organisation", input: "https://USER@dev.azure.com/ORG" + tail, want: "https://***@dev.azure.com/ORG" + tail},
		{name: "user is a prefix of the organisation", input: "https://OR@dev.azure.com/ORG" + tail, want: "https://***@dev.azure.com/ORG" + tail},
		{name: "organisation is a prefix of the user", input: "https://ORGX@dev.azure.com/ORG" + tail, want: "https://***@dev.azure.com/ORG" + tail},
		{name: "escaped colon after the organisation", input: "https://ORG%3A" + azureToken + "@dev.azure.com/ORG" + tail, want: "https://***@dev.azure.com/ORG" + tail},
		{name: "escaped slash after the organisation", input: "https://ORG%2F" + azureToken + "@dev.azure.com/ORG" + tail, want: "https://***@dev.azure.com/ORG" + tail},
		{name: "bad escape in the user", input: "https://ORG%zz@dev.azure.com/ORG" + tail, want: "https://***@dev.azure.com/ORG" + tail},
		{name: "user only in a later path segment", input: "https://" + azureToken + "@dev.azure.com/ORG/" + azureToken + "/_git/REPO", want: "https://***@dev.azure.com/ORG/" + azureToken + "/_git/REPO"},
		{name: "user as a prefix of the first segment", input: "https://ORG@dev.azure.com/ORG" + azureToken + tail, want: "https://***@dev.azure.com/ORG" + azureToken + tail},
		{name: "no organisation segment", input: "https://ORG@dev.azure.com", want: "https://***@dev.azure.com"},
		{name: "empty organisation segment", input: "https://ORG@dev.azure.com//ORG" + tail, want: "https://***@dev.azure.com//ORG" + tail},
		{name: "ssh host", input: "https://ORG@ssh.dev.azure.com/ORG" + tail, want: "https://***@ssh.dev.azure.com/ORG" + tail},
		{name: "legacy visual studio host", input: "https://ORG@vs-ssh.visualstudio.com/ORG" + tail, want: "https://***@vs-ssh.visualstudio.com/ORG" + tail},
		{name: "visual studio host", input: "https://ORG@ORG.visualstudio.com/ORG" + tail, want: "https://***@ORG.visualstudio.com/ORG" + tail},
		{name: "host with a port", input: "https://ORG@dev.azure.com:8443/ORG" + tail, want: "https://***@dev.azure.com:8443/ORG" + tail},
		{name: "host with a longer name", input: "https://ORG@dev.azure.com.test/ORG" + tail, want: "https://***@dev.azure.com.test/ORG" + tail},
		{name: "host as a prefix of another", input: "https://ORG@x.dev.azure.com/ORG" + tail, want: "https://***@x.dev.azure.com/ORG" + tail},
		{name: "plain http", input: "http://ORG@dev.azure.com/ORG" + tail, want: "http://***@dev.azure.com/ORG" + tail},
		{name: "other scheme", input: "git+https://ORG@dev.azure.com/ORG" + tail, want: "git+https://***@dev.azure.com/ORG" + tail},
		{name: "query after the host", input: "https://ORG@dev.azure.com?x=/ORG" + tail, want: "https://***@dev.azure.com?***"},
		{name: "query after the path", input: "https://ORG@dev.azure.com/ORG" + tail + "?access_token=" + azureToken, want: "https://***@dev.azure.com/ORG" + tail + "?***"},
		{name: "fragment after the path", input: "https://ORG@dev.azure.com/ORG" + tail + "#" + azureToken, want: "https://***@dev.azure.com/ORG" + tail + "#***"},
		{name: "query after the organisation segment", input: "https://ORG@dev.azure.com/ORG?t=" + azureToken + "/PROJECT/_git/REPO", want: "https://***@dev.azure.com/ORG?***"},
		{name: "query holding the organisation name", input: "https://ORG@dev.azure.com/ORG" + tail + "?x=ORG", want: "https://***@dev.azure.com/ORG" + tail + "?***"},
		{name: "query inside git's error text", input: "fatal: unable to access 'https://ORG@dev.azure.com/ORG" + tail + "?k=" + azureToken + "/'", want: "fatal: unable to access 'https://***@dev.azure.com/ORG" + tail + "?***'"},
		// Encoded, they are path text for git: still exempt in a later
		// segment, redacted in the organisation segment.
		{name: "encoded question mark in a later segment", input: "https://ORG@dev.azure.com/ORG/PROJECT%3Fx/_git/REPO", want: "https://ORG@dev.azure.com/ORG/PROJECT%3Fx/_git/REPO"},
		{name: "encoded number sign in a later segment", input: "https://ORG@dev.azure.com/ORG/PROJECT%23x/_git/REPO", want: "https://ORG@dev.azure.com/ORG/PROJECT%23x/_git/REPO"},
		{name: "encoded question mark in the user", input: "https://ORG%3Fx@dev.azure.com/ORG" + tail, want: "https://***@dev.azure.com/ORG" + tail},
		{name: "encoded number sign in the organisation segment", input: "https://ORG@dev.azure.com/ORG%23x" + tail, want: "https://***@dev.azure.com/ORG%23x" + tail},
		{
			name:  "an at sign later in the text moves the end of the userinfo",
			input: "https://ORG@dev.azure.com/ORG" + tail + "?e=" + azureToken + "@x",
			want:  "https://***@x",
		},
		{
			name:  "exempt url followed by a token url",
			input: "have https://ORG@dev.azure.com/ORG" + tail + " want https://" + azureToken + "@dev.azure.com/ORG" + tail,
			want:  "have https://ORG@dev.azure.com/ORG" + tail + " want https://***@dev.azure.com/ORG" + tail,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := remote.Redact(tt.input)
			if got != tt.want {
				t.Fatalf("Redact(%q) = %q, want %q", tt.input, got, tt.want)
			}
			if strings.Contains(got, azureToken) && !strings.Contains(tt.want, azureToken) {
				t.Fatalf("Redact(%q) = %q carries the token", tt.input, got)
			}
		})
	}
}
