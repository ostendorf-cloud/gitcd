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
		{name: "query", input: "https://h.test/acme/widget.git?a=FAKE-SECRET", want: "https://h.test/acme/widget.git?***"},
		{name: "fragment", input: "https://h.test/acme/widget.git#FAKE-SECRET", want: "https://h.test/acme/widget.git#***"},
		{name: "query and fragment", input: "https://h.test/x?a=FAKE#SECRET", want: "https://h.test/x?***"},
		{name: "fragment then query", input: "https://h.test/x#a?FAKE-SECRET", want: "https://h.test/x#***"},
		{name: "query on the host", input: "https://h.test?a=FAKE-SECRET", want: "https://h.test?***"},
		{name: "empty query", input: "https://h.test/x?", want: "https://h.test/x?***"},
		{name: "empty fragment", input: "https://h.test/x#", want: "https://h.test/x#***"},
		{name: "already masked", input: "https://h.test/x?***", want: "https://h.test/x?***"},
		{name: "percent encoded query", input: "https://h.test/x?a=%46AKE%2dSECRET%3d", want: "https://h.test/x?***"},
		{name: "unicode query", input: "https://h.test/x?a=äöü-FAKE", want: "https://h.test/x?***"},
		{name: "userinfo and query", input: "https://u:FAKE@h.test/x?a=SECRET", want: "https://***@h.test/x?***"},
		{name: "ssh plain user and query", input: "ssh://git@h.test/acme/widget.git?a=FAKE-SECRET", want: "ssh://git@h.test/acme/widget.git?***"},
		{name: "ssh plain user and no query", input: "ssh://git@h.test/acme/widget.git", want: "ssh://git@h.test/acme/widget.git"},
		{
			name:  "azure documented form and query",
			input: "https://ORG@dev.azure.com/ORG/PROJECT/_git/REPO?a=FAKE-SECRET",
			want:  "https://***@dev.azure.com/ORG/PROJECT/_git/REPO?***",
		},
		{name: "scp style with a question mark", input: "git@h.test:acme/widget.git?a=FAKE-SECRET", want: "git@h.test:acme/widget.git?***"},
		{name: "scp style with a number sign", input: "git@h.test:acme/widget.git#FAKE-SECRET", want: "git@h.test:acme/widget.git#***"},
		{name: "scp style in free text is left alone", input: "see git@h.test:acme/widget.git?a=b", want: "see git@h.test:acme/widget.git?a=b"},
		{name: "text without a url keeps its question mark", input: "what is this? # not a url", want: "what is this? # not a url"},
		{name: "number sign in a path of a git url", input: "https://h.test/acme/a#b.git", want: "https://h.test/acme/a#***"},
		{
			name:  "query in free text ends at the quote",
			input: "fatal: unable to access 'https://h.test/x.git?a=FAKE-SECRET/': refused",
			want:  "fatal: unable to access 'https://h.test/x.git?***': refused",
		},
		{
			name:  "query in free text ends at white space",
			input: "fetching https://h.test/x?a=FAKE-SECRET now",
			want:  "fetching https://h.test/x?*** now",
		},
		{
			name:  "two urls with queries",
			input: "have https://h.test/x?a=FAKE want https://g.test/y?b=SECRET",
			want:  "have https://h.test/x?*** want https://g.test/y?***",
		},
		{
			name:  "first url without a query, second with",
			input: "have https://h.test/x want https://g.test/y?b=SECRET",
			want:  "have https://h.test/x want https://g.test/y?***",
		},
		{
			name:  "two urls run together by a quote",
			input: "'https://h.test/x?a=FAKE''https://g.test/y?b=SECRET'",
			want:  "'https://h.test/x?***''https://g.test/y?***'",
		},
		{name: "very long query", input: "https://h.test/x?" + strings.Repeat("A", 100000), want: "https://h.test/x?***"},
		{name: "url inside a query", input: "https://h.test/x?redirect=https://h2.test/cb&token=FAKE", want: "https://h.test/x?***"},
		{name: "url in brackets inside a query", input: "https://h.test/x?a=(https://h2.test/c)", want: "https://h.test/x?***"},
		{name: "url inside a fragment", input: "https://h.test/x#=https://h2.test/FAKE", want: "https://h.test/x#***"},
		{name: "scheme with a plus inside a query", input: "https://h.test/x?u=git+https://FAKE", want: "https://h.test/x?***"},
		{name: "ssh url inside a query", input: "https://h.test/x?a=ssh://git@h/x?b=FAKE", want: "https://h.test/x?***"},
		{
			name:  "url inside a query in free text, then another url",
			input: "see 'https://h.test/x?r=https://h2.test/FAKE' and https://g.test/y?b=FAKE",
			want:  "see 'https://h.test/x?***' and https://g.test/y?***",
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

func TestRedactRemote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "no query", input: "https://h.test/acme/widget.git", want: "https://h.test/acme/widget.git"},
		{name: "scp style", input: "git@h.test:acme/widget.git", want: "git@h.test:acme/widget.git"},
		{name: "userinfo", input: "https://u:FAKE@h.test/x", want: "https://***@h.test/x"},
		{name: "query", input: "https://h.test/x?a=FAKE", want: "https://h.test/x?***"},
		{name: "query with a space after it", input: "https://h.test/x?a=FAKE SECOND", want: "https://h.test/x?***"},
		{name: "query with a quote after it", input: "https://h.test/x?a=FAKE'SECOND\"THIRD", want: "https://h.test/x?***"},
		{name: "query with a no break space", input: "https://h.test/x?a=FAKE SECOND", want: "https://h.test/x?***"},
		{name: "query with a line separator", input: "https://h.test/x?a=FAKE SECOND", want: "https://h.test/x?***"},
		{name: "fragment with angle brackets", input: "https://h.test/x#FAKE<SECOND>", want: "https://h.test/x#***"},
		{name: "url inside a query", input: "https://h.test/x?r=https://h2.test/FAKE", want: "https://h.test/x?***"},
		{name: "ssh plain user and query", input: "ssh://git@h.test/x?a=FAKE", want: "ssh://git@h.test/x?***"},
		{name: "leading space", input: " https://h.test/x?a=FAKE", want: " https://h.test/x?***"},
		{name: "trailing newline", input: "https://h.test/x?a=FAKE\n", want: "https://h.test/x?***"},
		{name: "scp style with a user", input: "git@h.test:acme/widget.git?a=FAKE", want: "git@h.test:acme/widget.git?***"},
		{name: "scp style without a user", input: "h.test:acme/widget.git?a=FAKE", want: "h.test:acme/widget.git?***"},
		{name: "scp style with an unusual user", input: "my+user@h.test:acme/widget.git?a=FAKE", want: "my+user@h.test:acme/widget.git?***"},
		{name: "scp style with a space in the path", input: "git@h.test:acme/wid get.git?a=FAKE SECOND", want: "git@h.test:acme/wid get.git?***"},
		{name: "scp style with a trailing newline", input: "git@h.test:acme/widget.git#FAKE\n", want: "git@h.test:acme/widget.git#***"},
		{name: "shorthand", input: "h.test/acme/widget?a=FAKE", want: "h.test/acme/widget?***"},
		{name: "local path", input: "/srv/git/widget#FAKE", want: "/srv/git/widget#***"},
		{name: "already masked", input: "git@h.test:acme/widget.git?***", want: "git@h.test:acme/widget.git?***"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := remote.RedactRemote(tt.input)
			if got != tt.want {
				t.Fatalf("RedactRemote(%q) = %q, want %q", tt.input, got, tt.want)
			}
			if again := remote.RedactRemote(got); again != got {
				t.Fatalf("RedactRemote(RedactRemote(%q)) = %q, want %q", tt.input, again, got)
			}
		})
	}
}

// TestRedactRemote_NeverKeepsTheQueryPart builds one remote from every shape
// with a marker after the first "?" or "#", followed by every character that
// may end a token in free text.
func TestRedactRemote_NeverKeepsTheQueryPart(t *testing.T) {
	t.Parallel()

	const marker = "FAKEQUERYMARK"
	heads := []string{
		"https://h.test/acme/widget.git",
		"https://u:p@h.test/acme/widget.git",
		"ssh://git@h.test/acme/widget.git",
		"git@h.test:acme/widget.git",
		"h.test:acme/widget.git",
		"my+user@h.test:acme/widget.git",
		"git@h.test:acme/wid get.git",
		"h.test/acme/widget",
		"/srv/git/widget",
	}
	separators := []string{"?", "#", "?a=", "#a=", "?a=b#", "?https://h2.test/"}
	middles := []string{"", " ", "\t", "'", "\"", "<", ">", " ", " ", "\n", "https://h3.test/"}
	for _, head := range heads {
		for _, sep := range separators {
			for _, mid := range middles {
				for _, wrap := range []string{"", "\n", " ", "'"} {
					input := wrap + head + sep + mid + marker + wrap
					got := remote.RedactRemote(input)
					if strings.Contains(got, marker) {
						t.Fatalf("RedactRemote(%q) = %q, want no query part", input, got)
					}
				}
			}
		}
	}
}

// TestRedact_NeverKeepsTheQueryPart places a marker after the first "?" or
// "#" of several URL shapes and checks that it never survives, and that a
// second pass changes nothing.
func TestRedact_NeverKeepsTheQueryPart(t *testing.T) {
	t.Parallel()

	const marker = "FAKEQUERYMARK"
	heads := []string{
		"https://h.test/acme/widget.git",
		"https://h.test",
		"https://u:p@h.test/acme/widget.git",
		"ssh://git@h.test/acme/widget.git",
		"https://ORG@dev.azure.com/ORG/PROJECT/_git/REPO",
		"http://127.0.0.1:1/x",
		"git@h.test:acme/widget.git",
	}
	separators := []string{"?", "#", "?a=", "#a=", "?a=b&c=", "?a=b#", "#a?", "?%3f", "??", "?ä="}
	tails := []string{"", "/", ".git", "&x=1", "%2F", "/more/path"}
	for _, head := range heads {
		for _, sep := range separators {
			for _, tail := range tails {
				input := head + sep + marker + tail
				got := remote.Redact(input)
				if strings.Contains(got, marker) {
					t.Fatalf("Redact(%q) = %q, want no query part", input, got)
				}
				if again := remote.Redact(got); again != got {
					t.Fatalf("Redact(Redact(%q)) = %q, want %q", input, again, got)
				}
				quoted := "fatal: 'https://h.test/x" + sep + marker + tail + "' failed"
				if got := remote.Redact(quoted); strings.Contains(got, marker) {
					t.Fatalf("Redact(%q) = %q, want no query part", quoted, got)
				}
			}
		}
	}
}

func TestParse_QueryAndFragmentAreIgnoredInAUrl(t *testing.T) {
	t.Parallel()

	want := remote.Remote{Host: "h.test", Owner: "acme", Repo: "widget"}
	for _, input := range []string{
		"https://h.test/acme/widget.git?a=FAKE-SECRET",
		"https://h.test/acme/widget.git#FAKE-SECRET",
		"ssh://git@h.test/acme/widget.git?a=FAKE-SECRET",
	} {
		got, err := remote.Parse(input, nil)
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", input, err)
		}
		if got != want {
			t.Fatalf("Parse(%q) = %+v, want %+v", input, got, want)
		}
	}
}

func TestParse_QueryAndFragmentAreIgnoredInTheOtherForms(t *testing.T) {
	t.Parallel()

	want := remote.Remote{Host: "h.test", Owner: "acme", Repo: "widget"}
	for _, input := range []string{
		"git@h.test:acme/widget.git?a=FAKE-SECRET",
		"git@h.test:acme/widget.git#FAKE-SECRET",
		"git@h.test:acme/widget?a=b#c",
		"git@h.test:acme/widget.git?r=https://x.test/y",
		"h.test:acme/widget.git?a=FAKE-SECRET",
		"h.test/acme/widget?a=FAKE-SECRET",
		"h.test/acme/widget.git#FAKE-SECRET",
		"git@h.test:acme/widget.git?a=FAKE-SECRET\n",
	} {
		got, err := remote.Parse(input, nil)
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", input, err)
		}
		if got != want {
			t.Fatalf("Parse(%q) = %+v, want %+v", input, got, want)
		}
	}

	azure := remote.Remote{Host: "dev.azure.com", Owner: "ORG", Project: "PROJECT", Repo: "REPO"}
	got, err := remote.Parse("dev.azure.com/ORG/PROJECT/REPO?a=FAKE:SECRET", nil)
	if err != nil || got != azure {
		t.Fatalf("Parse() = %+v, %v, want %+v", got, err, azure)
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
		{name: "query and no owner", input: "https://h.test/widget?a=FAKE-SECRET"},
		{name: "fragment and no host", input: "https:///acme/widget#FAKE-SECRET"},
		{name: "query and a bad port", input: "https://h.test:badport/acme/widget?a=FAKE-SECRET", wantErr: remote.ErrMalformedURL},
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
