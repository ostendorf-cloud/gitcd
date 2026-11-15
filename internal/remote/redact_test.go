// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package remote_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/remote"
)

func TestRedact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "no userinfo", input: "https://github.com/acme/widget.git", want: "https://github.com/acme/widget.git"},
		{name: "user and password", input: "https://user:FAKE-SECRET@github.com/acme/widget.git", want: "https://***@github.com/acme/widget.git"},
		{name: "token as user name", input: "https://FAKE-SECRET@github.com/acme/widget.git", want: "https://***@github.com/acme/widget.git"},
		{name: "password with an at sign", input: "https://user:p@ss@github.com/acme/widget.git", want: "https://***@github.com/acme/widget.git"},
		{name: "password with a slash", input: "https://user:FAKE/SECRET@127.0.0.1:1/acme/widget.git", want: "https://***@127.0.0.1:1/acme/widget.git"},
		{name: "token with a slash as user name", input: "https://FAKE/SECRET@host.test/acme/widget.git", want: "https://***@host.test/acme/widget.git"},
		{name: "azure devops organisation as user", input: "https://ORG@dev.azure.com/ORG/PROJECT/_git/REPO", want: "https://ORG@dev.azure.com/ORG/PROJECT/_git/REPO"},
		{name: "ssh user only", input: "ssh://git@github.com/acme/widget.git", want: "ssh://git@github.com/acme/widget.git"},
		{name: "ssh with password", input: "ssh://git:FAKE-SECRET@github.com/acme/widget.git", want: "ssh://***@github.com/acme/widget.git"},
		{name: "ssh with an encoded colon", input: "ssh://user%3aFAKE-SECRET@host.test:1/acme/widget.git", want: "ssh://***@host.test:1/acme/widget.git"},
		{name: "ssh with an upper case encoded colon", input: "ssh://user%3AFAKE-SECRET@host.test/acme/widget.git", want: "ssh://***@host.test/acme/widget.git"},
		{name: "ssh with an encoded slash", input: "ssh://FAKE%2fSECRET@host.test/acme/widget.git", want: "ssh://***@host.test/acme/widget.git"},
		{name: "ssh with a bad escape", input: "ssh://FAKE%zzSECRET@host.test/acme/widget.git", want: "ssh://***@host.test/acme/widget.git"},
		{name: "scp style", input: "git@github.com:acme/widget.git", want: "git@github.com:acme/widget.git"},
		{name: "shorthand", input: "github.com/acme/widget", want: "github.com/acme/widget"},
		{name: "at sign in the query is redacted", input: "https://github.com/acme/widget.git?email=a@b.test", want: "https://***@b.test"},
		{name: "at sign in the fragment is redacted", input: "https://github.com/acme/widget.git#a@b", want: "https://***@b"},
		{name: "reported shape with a slash and a question mark", input: "https://user:SEC/RE?T@h.invalid/acme/widget.git", want: "https://***@h.invalid/acme/widget.git"},
		{name: "reported shape with a slash and a number sign", input: "https://user:SEC/RE#T@h.invalid/acme/widget.git", want: "https://***@h.invalid/acme/widget.git"},
		{name: "password holding a scheme separator", input: "https://user:SEC://RET@127.0.0.1:1/x", want: "https://***@127.0.0.1:1/x"},
		{name: "two urls in quotes", input: "'https://a:b@h.test/x' and \"http://c:d@g.test/y\"", want: "'https://***@h.test/x' and \"http://***@g.test/y\""},
		{name: "space in the password", input: "https://user:FAKE SECRET@h.test/x", want: "https://***@h.test/x"},
		{name: "tab in the user name", input: "https://FAKE\tSECRET@h.test/x", want: "https://***@h.test/x"},
		{name: "path segment starting with an at sign is read as userinfo", input: "https://github.com/@acme/widget.git", want: "https://***@acme/widget.git"},
		{name: "question mark in the password", input: "https://user:FAKE?SECRET@h.test/acme/widget.git", want: "https://***@h.test/acme/widget.git"},
		{name: "number sign in the password", input: "https://user:FAKE#SECRET@h.test/acme/widget.git", want: "https://***@h.test/acme/widget.git"},
		{name: "slash before the at sign", input: "https://user:FAKESECRET/@h.test/acme/widget.git", want: "https://***@h.test/acme/widget.git"},
		{name: "question mark in a user name", input: "https://FAKE?SECRET@h.test/acme/widget.git", want: "https://***@h.test/acme/widget.git"},
		{name: "at sign in the middle of a path is read as userinfo", input: "https://github.com/acme/wid@get", want: "https://***@get"},
		{
			name:  "free text with a url",
			input: "fatal: unable to access 'https://user:FAKE-SECRET@host.test/x.git/': refused",
			want:  "fatal: unable to access 'https://***@host.test/x.git/': refused",
		},
		{
			name:  "two urls",
			input: "have https://a:b@h.test/x want http://c@h.test/y",
			want:  "have https://***@h.test/x want http://***@h.test/y",
		},
		{
			name:  "url that follows white space after a credential",
			input: "https://u:FAKE-SECRET@h.test/x then https://h.test/y@z",
			want:  "https://***@h.test/x then https://***@z",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := remote.Redact(tt.input); got != tt.want {
				t.Fatalf("Redact(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestRedact_NeverKeepsTheSecretPart builds userinfo strings from every
// character that a URL parser treats specially, in one or two characters at
// the start, the middle and the end, and checks that neither secret part
// survives.
func TestRedact_NeverKeepsTheSecretPart(t *testing.T) {
	t.Parallel()

	chars := []string{"/", "?", "#", "[", "]", `\`, "%", ":", " ", "\t"}
	var specials []string
	for _, a := range chars {
		specials = append(specials, a)
		for _, b := range chars {
			specials = append(specials, a+b)
		}
	}

	const first, second = "FAKEPRE", "SECRETPOST"
	for _, scheme := range []string{"https", "http", "ssh"} {
		for _, special := range specials {
			userinfos := []string{
				special + second,
				first + special,
				first + special + second,
				"user:" + first + special + second,
				"user:" + special + second,
				first + special + second + special,
			}
			for _, userinfo := range userinfos {
				input := scheme + "://" + userinfo + "@h.test/acme/widget.git"
				got := remote.Redact(input)
				if strings.Contains(got, first) || strings.Contains(got, second) {
					t.Fatalf("Redact(%q) = %q, want no secret part", input, got)
				}
			}
		}
	}
}

func TestParse_ErrorsDoNotCarryTheCredential(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{name: "no owner", input: "https://user:FAKE-SECRET@github.com/widget"},
		{name: "no host", input: "https://user:FAKE-SECRET@/acme/widget"},
		{name: "unparsable url", input: "https://user:FAKE-SECRET@github.com:badport/acme/widget", wantErr: remote.ErrMalformedURL},
		{name: "azure path", input: "https://user:FAKE-SECRET@dev.azure.com/acme/project"},
		{name: "question mark in the password", input: "https://user:FAKE?SECRET@h.test/acme/widget.git", wantErr: remote.ErrMalformedURL},
		{name: "number sign in the password", input: "https://user:FAKE#SECRET@h.test/acme/widget.git", wantErr: remote.ErrMalformedURL},
		{name: "space in the password", input: "https://user:FAKE SECRET@h.test/acme/widget.git", wantErr: remote.ErrMalformedURL},
		{name: "tab in the password", input: "https://user:FAKE\tSECRET@h.test/acme/widget.git", wantErr: remote.ErrMalformedURL},
		{name: "trailing slash before the at sign", input: "https://user:FAKESECRET/@h.test/acme/widget.git", wantErr: remote.ErrMalformedURL},
		{name: "question mark in a user name", input: "https://FAKE?SECRET@h.test/acme/widget.git", wantErr: remote.ErrAmbiguousUserinfo},
		{name: "at sign in the path", input: "https://h.test/acme/r3@v2.git", wantErr: remote.ErrAmbiguousUserinfo},
		{name: "password with a slash", input: "https://user:FAKE/SECRET@127.0.0.1:1/acme/widget.git", wantErr: remote.ErrMalformedURL},
		{name: "token with a slash as user name", input: "https://FAKE/SECRET@github.com/acme/widget.git", wantErr: remote.ErrAmbiguousUserinfo},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := remote.Parse(tt.input, nil)
			if err == nil {
				t.Fatalf("Parse(%q) succeeded, want an error", tt.input)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("Parse(%q) error = %v, want %v", tt.input, err, tt.wantErr)
			}
			// No part of the input, not only the credential, is quoted.
			for _, part := range []string{"FAKE", "SECRET", "h.test", "acme", "widget", "r3"} {
				if strings.Contains(err.Error(), part) {
					t.Fatalf("Parse(%q) error quotes %q of the input: %v", tt.input, part, err)
				}
			}
		})
	}
}

func TestParse_PercentEncodedAtSignInThePath(t *testing.T) {
	t.Parallel()

	got, err := remote.Parse("https://h.test/acme/r3%40v2.git", nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := remote.Remote{Host: "h.test", Owner: "acme", Repo: "r3@v2"}
	if got != want {
		t.Fatalf("Parse() = %+v, want %+v", got, want)
	}
}

func TestParse_UserinfoDoesNotChangeTheResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		withUserinfo string
		plain        string
	}{
		{
			name:         "azure devops organisation as user",
			withUserinfo: "https://ORG@dev.azure.com/ORG/PROJECT/_git/REPO",
			plain:        "https://dev.azure.com/ORG/PROJECT/_git/REPO",
		},
		{
			name:         "user and password",
			withUserinfo: "https://user:FAKE-SECRET@github.com/acme/widget.git",
			plain:        "https://github.com/acme/widget.git",
		},
		{
			name:         "ssh user",
			withUserinfo: "ssh://git@github.com/acme/widget.git",
			plain:        "ssh://github.com/acme/widget.git",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := remote.Parse(tt.withUserinfo, nil)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.withUserinfo, err)
			}
			want, err := remote.Parse(tt.plain, nil)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.plain, err)
			}
			if got != want {
				t.Fatalf("Parse(%q) = %+v, want %+v", tt.withUserinfo, got, want)
			}
		})
	}
}
