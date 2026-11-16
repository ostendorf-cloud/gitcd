// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package git_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/git"
	"github.com/ostendorf-cloud/gitcd/internal/testenv"
)

// setupGitEnv isolates every git invocation in the test from the real
// user's configuration and from the network: a dedicated global config
// file, no system config, and a controlled HOME so "~" in a pattern
// expands from a path the test owns.
func setupGitEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	globalConfig := filepath.Join(home, ".gitconfig")
	if err := os.WriteFile(globalConfig, []byte("[user]\n\tname = Test User\n\temail = [email protected]\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", globalConfig, err)
	}
	testenv.SetHome(t, home)
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// initRepository creates a git repository under dir with one committed
// file, so a test has a source to clone from without reaching the network.
func initRepository(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("example\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	run(t, dir, "add", "README.md")
	run(t, dir, "commit", "-m", "add README")
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func TestClone(t *testing.T) {
	setupGitEnv(t)

	t.Run("clones a local repository into a new destination", func(t *testing.T) {
		root := t.TempDir()
		source := filepath.Join(root, "source")
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Fatalf("create %s: %v", source, err)
		}
		initRepository(t, source)

		dest := filepath.Join(root, "nested", "dest")
		if err := git.Clone(context.Background(), source, dest, io.Discard); err != nil {
			t.Fatalf("Clone(%q, %q) returned an unexpected error: %v", source, dest, err)
		}

		if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
			t.Fatalf("cloned repository is missing its content: %v", err)
		}
		if !git.IsRepository(dest) {
			t.Fatalf("IsRepository(%q) = false, want true after a successful clone", dest)
		}
	})

	t.Run("wraps the underlying error for a source that does not exist", func(t *testing.T) {
		root := t.TempDir()
		dest := filepath.Join(root, "dest")

		err := git.Clone(context.Background(), filepath.Join(root, "missing"), dest, io.Discard)
		if err == nil {
			t.Fatal("Clone() succeeded, want an error for a missing source")
		}
	})

	// TestClone/streams_the_clone_s_own_progress_to_the_given_writer guards
	// against "CombinedOutput", which buffers a clone's progress until the
	// command finishes and, before this fix, discarded it entirely on
	// success: a caller had no way to show the user anything while a large
	// clone was still running. Clone must now write git's own progress to
	// the given writer unconditionally, not only fold it into an error.
	t.Run("streams the clone's own progress to the given writer", func(t *testing.T) {
		root := t.TempDir()
		source := filepath.Join(root, "source")
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Fatalf("create %s: %v", source, err)
		}
		initRepository(t, source)

		dest := filepath.Join(root, "dest")
		var progress bytes.Buffer
		if err := git.Clone(context.Background(), source, dest, &progress); err != nil {
			t.Fatalf("Clone(%q, %q) returned an unexpected error: %v", source, dest, err)
		}

		if progress.Len() == 0 {
			t.Fatal("Clone() wrote nothing to the given writer, want git's own clone progress streamed to it")
		}
	})
}

func TestCloneWithConfig(t *testing.T) {
	setupGitEnv(t)

	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatalf("create %s: %v", source, err)
	}
	initRepository(t, source)

	dest := filepath.Join(root, "dest")
	pin := map[string]string{"user.email": "[email protected]", "user.name": "Pinned User"}
	if err := git.CloneWithConfig(context.Background(), source, dest, pin, io.Discard); err != nil {
		t.Fatalf("CloneWithConfig() returned an unexpected error: %v", err)
	}

	value, ok, err := git.ShowOrigin(context.Background(), dest, "user.email")
	if err != nil {
		t.Fatalf("ShowOrigin() returned an unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("ShowOrigin() ok = false, want true: the clone should have pinned user.email")
	}
	if value.Value != "[email protected]" {
		t.Fatalf("ShowOrigin() value = %q, want %q, the value CloneWithConfig pinned", value.Value, "[email protected]")
	}
}

func TestClone_FailureDoesNotCarryTheRemoteCredential(t *testing.T) {
	setupGitEnv(t)

	missing := filepath.Join(t.TempDir(), "missing")
	tests := []struct {
		name string
		url  string
	}{
		{name: "user and password", url: "https://user:token123@example.test/acme/widget.git"},
		{name: "token as user name", url: "https://token123@example.test/acme/widget.git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Rewrite the URL to a missing local path, so the clone fails
			// without any network access.
			prefix := tt.url[:strings.LastIndex(tt.url, "/acme")+1]
			config := map[string]string{"url.file://" + missing + "/.insteadOf": prefix}
			var progress bytes.Buffer
			err := git.CloneWithConfig(context.Background(), tt.url, filepath.Join(t.TempDir(), "dest"), config, &progress)
			if err == nil {
				t.Fatal("CloneWithConfig() succeeded, want a failure")
			}
			if strings.Contains(err.Error(), "token123") {
				t.Fatalf("CloneWithConfig() error carries the credential: %v", err)
			}
		})
	}
}

func TestFetchError_DoesNotCarryTheRemoteCredential(t *testing.T) {
	t.Parallel()

	// The output is what git prints on a Linux runner, where it repeats a
	// part of the credential as plain text after the URL.
	const couldNotResolve = "fatal: unable to access 'https://h.invalid/x/': Could not resolve host: FAKE"
	// Each entry is what "git config -z --get-regexp" returns for the
	// repository: the key, a newline, the value, a NUL.
	tests := []struct {
		name   string
		config string
		output string
	}{
		{
			name:   "origin with a credential",
			config: "remote.origin.url\nhttps://user:FAKE/SEC?RET@h.invalid/acme/widget.git\x00",
			output: "fatal: unable to access 'https://user:FAKE/SEC?RET@h.invalid/acme/widget.git/': Could not resolve host: FAKE",
		},
		{
			name:   "token with a slash as the user name",
			config: "remote.origin.url\nhttps://FAKE/SEC#RET@h.invalid/acme/widget.git\x00",
			output: "fatal: unable to access 'https://FAKE/SEC#RET@h.invalid/acme/widget.git/': Could not resolve host: FAKE",
		},
		{
			name:   "could not read a user name",
			config: "remote.origin.url\nhttps://user:FAKESECRET@h.invalid/acme/widget.git\x00",
			output: "fatal: could not read Username for 'https://h.invalid': terminal prompts disabled\nSEC RET",
		},
		{
			name:   "url that does not parse",
			config: "remote.origin.url\nhttps://user:FAKE SEC?RET@h.invalid/x\x00",
			output: "fatal: unable to access: bad URL FAKE SEC RET",
		},
		{
			name:   "only upstream carries a credential",
			config: "remote.upstream.url\nhttps://user:FAKESECRET@h.invalid/acme/widget.git\x00",
			output: couldNotResolve,
		},
		{
			name: "a tracked fork carries a credential and origin is clean",
			config: "remote.origin.url\nhttps://h.test/acme/widget.git\x00" +
				"remote.fork.url\nhttps://user:FAKESECRET@h.invalid/me/widget.git\x00",
			output: couldNotResolve,
		},
		{
			name: "a push url carries a credential",
			config: "remote.origin.url\nhttps://h.test/acme/widget.git\x00" +
				"remote.origin.pushurl\nhttps://user:FAKESECRET@h.invalid/acme/widget.git\x00",
			output: couldNotResolve,
		},
		{
			name: "a rewrite rule base carries a credential",
			config: "remote.origin.url\nhttps://h.test/acme/widget.git\x00" +
				"url.https://user:FAKESECRET@h.invalid/.insteadof\nhttps://h.test/\x00",
			output: couldNotResolve,
		},
		{
			name: "a rewrite rule prefix carries a credential",
			config: "remote.origin.url\nhttps://h.test/acme/widget.git\x00" +
				"url.https://h.test/.pushinsteadof\nhttps://user:FAKESECRET@h.invalid/\x00",
			output: couldNotResolve,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			run := func(...string) ([]byte, error) { return []byte(tt.config), nil }
			withhold := git.ConfiguredURLsCarryCredential(run)
			if !withhold {
				t.Fatalf("ConfiguredURLsCarryCredential() = false, want true")
			}
			err := git.FetchError("/repo", errors.New("exit status 128"), tt.output, withhold)
			for _, part := range []string{"FAKE", "SEC", "RET"} {
				if strings.Contains(err.Error(), part) {
					t.Fatalf("FetchError() carries %q: %v", part, err)
				}
			}
			if !strings.Contains(err.Error(), "exit status 128") {
				t.Fatalf("FetchError() = %v, want the exit status kept", err)
			}
		})
	}
}

func TestConfiguredURLsCarryCredential(t *testing.T) {
	setupGitEnv(t)

	noMatch := exec.CommandContext(context.Background(), "git", "config", "--get", "no.such.key").Run()
	var exitErr *exec.ExitError
	if !errors.As(noMatch, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("git config --get of a missing key = %v, want exit status 1", noMatch)
	}

	tests := []struct {
		name string
		out  string
		err  error
		want bool
	}{
		{name: "clean repository", out: "remote.origin.url\nhttps://h.test/acme/widget.git\x00remote.origin.fetch\n+refs/heads/*\x00", want: false},
		{name: "ssh user only", out: "remote.origin.url\nssh://git@h.test/acme/widget.git\x00", want: false},
		{name: "scp style", out: "remote.origin.url\ngit@h.test:acme/widget.git\x00", want: false},
		{name: "rewrite rule without a credential", out: "url.git@h.test:.insteadof\nhttps://h.test/\x00", want: false},
		{name: "no configured url at all", err: noMatch, want: false},
		{name: "configuration cannot be read", err: errors.New("boom"), want: true},
		{name: "query in the url", out: "remote.origin.url\nhttps://h.test/acme/widget.git?a=b\x00", want: true},
		{name: "fragment in the url", out: "remote.origin.url\nhttps://h.test/acme/widget.git#a\x00", want: true},
		{name: "query in a push url", out: "remote.origin.url\nhttps://h.test/a/w.git\x00remote.origin.pushurl\nhttps://h.test/a/w.git?a=b\x00", want: true},
		{name: "query in another remote", out: "remote.origin.url\nhttps://h.test/a/w.git\x00remote.other.url\nhttps://g.test/a/w.git?a=b\x00", want: true},
		{name: "query in a rewrite base", out: "url.https://h.test/x?a=b.insteadof\nhttps://elsewhere.test/\x00", want: true},
		{name: "query in a rewrite base written in upper case", out: "URL.https://h.test/x?a=b.INSTEADOF\nhttps://elsewhere.test/\x00", want: true},
		{name: "query in a push rewrite base written in mixed case", out: "Url.https://h.test/x?a=b.PushInsteadOf\nhttps://elsewhere.test/\x00", want: true},
		{name: "query in a proxy", out: "http.proxy\nh.test:3128/x?a=b\x00", want: true},
		{name: "query in a proxy for a url", out: "http.https://h.test/.proxy\nh.test:3128/x?a=b\x00", want: true},
		{name: "query in a remote proxy", out: "remote.origin.proxy\nh.test:3128/x#a\x00", want: true},
		{name: "user and password in a proxy without a scheme", out: "http.proxy\nuser:pw@h.test:3128\x00", want: true},
		{name: "plain proxy", out: "http.proxy\nh.test:3128\x00", want: false},
		{name: "empty proxy", out: "http.proxy\n\x00", want: false},
		{name: "query in a bundle uri", out: "fetch.bundleuri\nfile:///missing/b?a=b\x00", want: true},
		{name: "query in a bundle list uri", out: "bundle.one.uri\nfile:///missing/b?a=b\x00", want: true},
		{name: "plain bundle uri", out: "fetch.bundleuri\nfile:///missing/b\x00", want: false},
		{name: "key that is no rewrite rule with a query in its subsection", out: "url.https://h.test/?tok.foo\nplain\x00", want: false},
		{name: "number sign in a value under a key that holds no url", out: "user.name\nTeam #1\x00core.gitproxy\nssh #1\x00", want: false},
		{name: "query in a rewrite value", out: "url.git@h.test:.insteadof\nhttps://h.test/x?a=b\x00", want: true},
		{name: "query in a push rewrite base", out: "url.https://h.test/x?a=b.pushinsteadof\nhttps://elsewhere.test/\x00", want: true},
		{name: "query in a push rewrite value", out: "url.git@h.test:.pushinsteadof\nhttps://h.test/x#a\x00", want: true},
		{name: "scp style with a query", out: "remote.origin.url\ngit@h.test:acme/widget.git?a=b\x00", want: true},
		{name: "scp style without a user and a query", out: "remote.origin.url\nh.test:acme/widget.git?a=b\x00", want: true},
		{name: "scp style with an unusual user and a query", out: "remote.origin.url\nmy+user@h.test:acme/widget.git?a=b\x00", want: true},
		{name: "scp style with a space and a query", out: "remote.origin.url\ngit@h.test:acme/wid get.git?a=b c\x00", want: true},
		{name: "shorthand with a query", out: "remote.origin.url\nh.test/acme/widget?a=b\x00", want: true},
		{name: "query followed by a space and a quote", out: "remote.origin.url\nhttps://h.test/x?a=b 'c\x00", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := func(...string) ([]byte, error) { return []byte(tt.out), tt.err }
			if got := git.ConfiguredURLsCarryCredential(run); got != tt.want {
				t.Fatalf("ConfiguredURLsCarryCredential() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigPairsCarryCredential(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config map[string]string
		want   bool
	}{
		{name: "user name with a number sign", config: map[string]string{"user.name": "Team #1"}, want: false},
		{name: "signing key with a question mark", config: map[string]string{"user.signingkey": "what? #2"}, want: false},
		{name: "plain email", config: map[string]string{"user.email": "dev@h.test"}, want: false},
		{name: "rewrite value with a query", config: map[string]string{"url.file:///missing/.insteadOf": "https://h.test/x?a=b"}, want: true},
		{name: "rewrite value with a fragment and no scheme", config: map[string]string{"url.file:///missing/.pushInsteadOf": "h.test:acme/x#a"}, want: true},
		{name: "rewrite base with a query", config: map[string]string{"url.file:///missing/?tok.insteadOf": "https://h.test/"}, want: true},
		{name: "remote url with a query", config: map[string]string{"remote.origin.url": "h.test/acme/x?a"}, want: true},
		{name: "other key holding a url with a query", config: map[string]string{"core.hooksPath": "https://h.test/x?a"}, want: true},
		{name: "other key holding a credential", config: map[string]string{"user.name": "https://u:p@h.test/x"}, want: true},
		{name: "upper case section and query in the base", config: map[string]string{"URL.file:///missing/?tok.insteadOf": "https://h.test/"}, want: true},
		{name: "mixed case section and query in the base", config: map[string]string{"Url.file:///missing/?tok.insteadOf": "https://h.test/"}, want: true},
		{name: "upper case variable and query in the base", config: map[string]string{"url.file:///missing/?tok.INSTEADOF": "https://h.test/"}, want: true},
		{name: "mixed case push variable and query in the base", config: map[string]string{"url.file:///missing/?tok.PushInsteadOf": "https://h.test/"}, want: true},
		{name: "upper case rewrite with a query in the value", config: map[string]string{"URL.file:///missing/.INSTEADOF": "https://h.test/x?a=b"}, want: true},
		{name: "mixed case rewrite with a query in the value", config: map[string]string{"Url.file:///missing/.PushInsteadOf": "h.test:acme/x#a"}, want: true},
		{name: "upper case rewrite without a query", config: map[string]string{"URL.file:///missing/.INSTEADOF": "https://h.test/"}, want: false},
		{name: "upper case remote url with a query", config: map[string]string{"REMOTE.origin.URL": "h.test/acme/x?a"}, want: true},
		{name: "http proxy with a query and no scheme", config: map[string]string{"http.proxy": "proxy.test:3128/x?a=b"}, want: true},
		{name: "upper case http proxy with a query and no scheme", config: map[string]string{"HTTP.PROXY": "proxy.test:3128/x?a=b"}, want: true},
		{name: "mixed case http proxy with a fragment", config: map[string]string{"Http.Proxy": "proxy.test:3128#a"}, want: true},
		{name: "http proxy with a query and a scheme", config: map[string]string{"http.proxy": "http://proxy.test:3128/?a=b"}, want: true},
		{name: "http proxy with a credential", config: map[string]string{"http.proxy": "http://u:p@proxy.test:3128"}, want: true},
		{name: "http proxy for a url with a query", config: map[string]string{"http.https://h.test/.proxy": "proxy.test:3128/x?a=b"}, want: true},
		{name: "remote proxy with a query", config: map[string]string{"remote.origin.proxy": "proxy.test:3128?a=b"}, want: true},
		{name: "bundle uri with a query", config: map[string]string{"fetch.bundleURI": "h.test/b.bundle?a=b"}, want: true},
		{name: "bundle list uri with a query", config: map[string]string{"bundle.one.uri": "h.test/b.bundle?a=b"}, want: true},
		{name: "http proxy without a query", config: map[string]string{"http.proxy": "proxy.test:3128"}, want: false},
		{name: "upper case http proxy without a query", config: map[string]string{"HTTP.PROXY": "http://proxy.test:3128"}, want: false},
		{name: "http proxy for a url without a query", config: map[string]string{"http.https://h.test/.proxy": "proxy.test:3128"}, want: false},
		{name: "proxy command with a number sign", config: map[string]string{"core.gitProxy": "ssh -p 22 #1"}, want: false},
		{name: "credential helper with a question mark", config: map[string]string{"credential.helper": "cache --timeout 1?"}, want: false},
		{name: "extra header with a number sign", config: map[string]string{"http.extraHeader": "X-Team: #1"}, want: false},
		{name: "http key with a url subsection and a plain value", config: map[string]string{"http.https://h.test/?tok.sslVerify": "false"}, want: false},
		{name: "key that is no rewrite rule with a query in its subsection", config: map[string]string{"url.https://h.test/?tok.foo": "plain"}, want: false},
		{name: "proxy without a scheme with a user and a password", config: map[string]string{"http.proxy": "user:pw@h.test:3128"}, want: true},
		{name: "proxy without a scheme with a user and a password and a path", config: map[string]string{"http.proxy": "user:pw@h.test:3128/x"}, want: true},
		{name: "proxy without a scheme with a path and a query", config: map[string]string{"http.proxy": "h.test:3128/p?q"}, want: true},
		{name: "proxy with a scheme with a user and a password", config: map[string]string{"http.proxy": "socks5://user:pw@h.test:1080"}, want: true},
		{name: "proxy without a scheme with a user and no password", config: map[string]string{"http.proxy": "user@h.test:3128"}, want: true},
		{name: "remote proxy without a scheme with a user and a password", config: map[string]string{"remote.origin.proxy": "user:pw@h.test:3128"}, want: true},
		{name: "upper case proxy key without a scheme with a user and a password", config: map[string]string{"HTTP.PROXY": "user:pw@h.test:3128"}, want: true},
		{name: "proxy without a scheme and without a user", config: map[string]string{"http.proxy": "h.test:3128"}, want: false},
		{name: "proxy with a scheme and without a user", config: map[string]string{"http.proxy": "socks5://h.test:1080"}, want: false},
		{name: "empty proxy", config: map[string]string{"http.proxy": ""}, want: false},
		{name: "empty remote proxy", config: map[string]string{"remote.origin.proxy": ""}, want: false},
		{name: "proxy auth method", config: map[string]string{"http.proxyAuthMethod": "basic"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := git.ConfigPairsCarryCredential(tt.config); got != tt.want {
				t.Fatalf("ConfigPairsCarryCredential(%v) = %v, want %v", tt.config, got, tt.want)
			}
		})
	}
}

func TestFetch_WithholdsOutputWhenOnlyAnotherRemoteCarriesACredential(t *testing.T) {
	setupGitEnv(t)

	repo := t.TempDir()
	initRepository(t, repo)
	run(t, repo, "remote", "add", "upstream", "https://user:FAKESECRET@127.0.0.1:1/acme/widget.git")

	err := git.Fetch(context.Background(), repo)
	if err == nil {
		t.Fatal("Fetch() succeeded, want a failure for an unreachable remote")
	}
	if strings.Contains(err.Error(), "FAKE") || !strings.Contains(err.Error(), "withheld") {
		t.Fatalf("Fetch() error = %v, want git's output withheld", err)
	}
}

func TestFetchError_KeepsTheOutputForACleanRepository(t *testing.T) {
	t.Parallel()

	err := git.FetchError("/repo", errors.New("exit status 128"), "fatal: no route to host", false)
	if !strings.Contains(err.Error(), "no route to host") {
		t.Fatalf("FetchError() = %v, want git's output kept", err)
	}
}

func TestClone_WithACredentialStreamsNothingFromGit(t *testing.T) {
	setupGitEnv(t)

	missing := filepath.Join(t.TempDir(), "missing")
	remoteURL := "https://user:FAKE/SEC?RET@h.invalid/acme/widget.git"
	config := map[string]string{"url.file://" + missing + "/.insteadOf": "https://user:FAKE/SEC?RET@h.invalid/"}
	var progress bytes.Buffer
	err := git.CloneWithConfig(context.Background(), remoteURL, filepath.Join(t.TempDir(), "dest"), config, &progress)
	if err == nil {
		t.Fatal("CloneWithConfig() succeeded, want a failure")
	}
	if progress.Len() != 0 {
		t.Fatalf("progress = %q, want git's output withheld", progress.String())
	}
	for _, part := range []string{"FAKE", "SEC", "RET"} {
		if strings.Contains(err.Error(), part) {
			t.Fatalf("CloneWithConfig() error carries %q: %v", part, err)
		}
	}
}

// TestClone_WithAQueryStreamsNothingFromGit covers the clone URL and a pair
// given with "-c": a query or a fragment counts like userinfo.
func TestClone_WithAQueryStreamsNothingFromGit(t *testing.T) {
	setupGitEnv(t)

	missing := filepath.Join(t.TempDir(), "missing")
	tests := []struct {
		name      string
		remoteURL string
		config    map[string]string
	}{
		{
			name:      "clone url with a query",
			remoteURL: "https://h.invalid/acme/widget.git?a=FAKEQUERY",
			config:    map[string]string{"url.file://" + missing + "/.insteadOf": "https://h.invalid/acme/widget.git?a=FAKEQUERY"},
		},
		{
			name:      "clone url with a fragment",
			remoteURL: "https://h.invalid/acme/widget.git#FAKEQUERY",
			config:    map[string]string{"url.file://" + missing + "/.insteadOf": "https://h.invalid/acme/widget.git#FAKEQUERY"},
		},
		{
			name:      "pair with a query in the rewritten prefix",
			remoteURL: "file://" + missing + "/widget.git",
			config:    map[string]string{"url.file://" + missing + "/.insteadOf": "https://h.invalid/?FAKEQUERY"},
		},
		{
			name:      "pair with a query in the base",
			remoteURL: "https://h.invalid/acme/widget.git",
			config:    map[string]string{"url.file://" + missing + "/?FAKEQUERY.insteadOf": "https://h.invalid/"},
		},
		{
			name:      "pair with an upper case section and a query in the base",
			remoteURL: "https://h.invalid/acme/widget.git",
			config:    map[string]string{"URL.file://" + missing + "/?FAKEQUERY.INSTEADOF": "https://h.invalid/"},
		},
		{
			name:      "pair with a mixed case section and a query in the value",
			remoteURL: "file://" + missing + "/widget.git",
			config:    map[string]string{"Url.file://" + missing + "/.InsteadOf": "https://h.invalid/?FAKEQUERY"},
		},
		{
			name:      "pair with a mixed case push rewrite and a query in the base",
			remoteURL: "https://h.invalid/acme/widget.git",
			config:    map[string]string{"uRL.file://" + missing + "/?FAKEQUERY.PushInsteadOf": "https://h.invalid/"},
		},
		{
			// Only the key says this value is a proxy: it has no scheme.
			name:      "pair with an upper case proxy key and a query in a value without a scheme",
			remoteURL: "file://" + missing + "/widget.git",
			config:    map[string]string{"HTTP.PROXY": "proxy.invalid:3128/x?FAKEQUERY"},
		},
		{
			name:      "pair with a proxy without a scheme and a password",
			remoteURL: "file://" + missing + "/widget.git",
			config:    map[string]string{"http.proxy": "FAKEQUERY:FAKESECRET@proxy.invalid:3128"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var progress bytes.Buffer
			err := git.CloneWithConfig(context.Background(), tt.remoteURL, filepath.Join(t.TempDir(), "dest"), tt.config, &progress)
			if err == nil {
				t.Fatal("CloneWithConfig() succeeded, want a failure")
			}
			if progress.Len() != 0 {
				t.Fatalf("progress = %q, want git's output withheld", progress.String())
			}
			if !strings.Contains(err.Error(), "withheld") || strings.Contains(err.Error(), "FAKEQUERY") {
				t.Fatalf("CloneWithConfig() error = %v, want the output withheld and no query", err)
			}
		})
	}
}

// requireNoTestKeysInGlobalConfig fails when the global configuration file of
// the test already holds a value a row wrote: every row starts from the file
// setupGitEnv creates.
func requireNoTestKeysInGlobalConfig(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile(os.Getenv("GIT_CONFIG_GLOBAL"))
	if err != nil {
		t.Fatalf("read the global configuration: %v", err)
	}
	if strings.Contains(string(data), "FAKE") {
		t.Fatalf("global configuration carries a value of an earlier row:\n%s", data)
	}
}

// TestFetch_KeepsTheOutputForAFreeTextValueInTheConfiguration puts a "#" and
// a "?" into values of keys that hold no URL, in the repository and in the
// global file: git's output stays.
func TestFetch_KeepsTheOutputForAFreeTextValueInTheConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		val    string
		global bool
	}{
		{name: "user name with a number sign", key: "user.name", val: "Team #1"},
		{name: "user name with a number sign in the global file", key: "user.name", val: "Team #1", global: true},
		{name: "editor with a question mark", key: "core.editor", val: "vi ?", global: false},
		{name: "editor with a question mark in the global file", key: "core.editor", val: "vi ?", global: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupGitEnv(t)
			repo := t.TempDir()
			initRepository(t, repo)
			run(t, repo, "remote", "add", "origin", "https://127.0.0.1:1/acme/widget.git")
			if tt.global {
				run(t, repo, "config", "--global", tt.key, tt.val)
			} else {
				run(t, repo, "config", tt.key, tt.val)
			}

			err := git.Fetch(context.Background(), repo)
			if err == nil {
				t.Fatal("Fetch() succeeded, want a failure for an unreachable remote")
			}
			if strings.Contains(err.Error(), "withheld") || !strings.Contains(err.Error(), "127.0.0.1") {
				t.Fatalf("Fetch() error = %v, want git's output kept", err)
			}
		})
	}
}

// TestFetch_WithholdsOutputForAQueryInTheConfiguration puts a query into each
// URL key git may read.
func TestFetch_WithholdsOutputForAQueryInTheConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		val    string
		global bool
	}{
		{name: "proxy with a path and a query", key: "http.proxy", val: "127.0.0.1:1/x?FAKEQUERY"},
		{name: "proxy with a path and a query in the global file", key: "http.proxy", val: "127.0.0.1:1/x?FAKEQUERY", global: true},
		{name: "proxy for a url", key: "http.https://127.0.0.1:1/.proxy", val: "127.0.0.1:1/x#FAKEQUERY"},
		{name: "proxy for a url in the global file", key: "http.https://127.0.0.1:1/.proxy", val: "127.0.0.1:1/x#FAKEQUERY", global: true},
		{name: "remote proxy", key: "remote.origin.proxy", val: "127.0.0.1:1/x?FAKEQUERY"},
		{name: "remote proxy in the global file", key: "remote.origin.proxy", val: "127.0.0.1:1/x?FAKEQUERY", global: true},
		{name: "proxy with a user and a password and no scheme", key: "http.proxy", val: "FAKEQUERY:FAKESECRET@127.0.0.1:1"},
		{name: "proxy with a user and a password and no scheme in the global file", key: "http.proxy", val: "FAKEQUERY:FAKESECRET@127.0.0.1:1", global: true},
		{name: "bundle uri", key: "fetch.bundleURI", val: "file:///missing/b.bundle?FAKEQUERY"},
		{name: "bundle uri in the global file", key: "fetch.bundleURI", val: "file:///missing/b.bundle?FAKEQUERY", global: true},
		{name: "bundle list uri", key: "bundle.one.uri", val: "file:///missing/b.bundle?FAKEQUERY"},
		{name: "bundle list uri in the global file", key: "bundle.one.uri", val: "file:///missing/b.bundle?FAKEQUERY", global: true},
		{name: "push url", key: "remote.origin.pushurl", val: "https://127.0.0.1:1/x.git?a=FAKEQUERY"},
		{name: "another remote", key: "remote.other.url", val: "https://127.0.0.1:1/x.git?a=FAKEQUERY"},
		{name: "rewrite base", key: "url.https://127.0.0.1:1/x?a=FAKEQUERY.insteadOf", val: "https://elsewhere.test/"},
		{name: "rewrite value", key: "url.https://127.0.0.1:1/.insteadOf", val: "https://elsewhere.test/?a=FAKEQUERY"},
		{name: "push rewrite base", key: "url.https://127.0.0.1:1/x#FAKEQUERY.pushInsteadOf", val: "https://elsewhere.test/"},
		{name: "push rewrite value", key: "url.https://127.0.0.1:1/.pushInsteadOf", val: "https://elsewhere.test/#FAKEQUERY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Each row has its own global configuration file: a key written
			// to it must not be visible to any other row.
			setupGitEnv(t)
			requireNoTestKeysInGlobalConfig(t)
			repo := t.TempDir()
			initRepository(t, repo)
			run(t, repo, "remote", "add", "origin", "https://127.0.0.1:1/acme/widget.git")
			if tt.global {
				run(t, repo, "config", "--global", tt.key, tt.val)
			} else {
				run(t, repo, "config", tt.key, tt.val)
			}

			err := git.Fetch(context.Background(), repo)
			if err == nil {
				t.Fatal("Fetch() succeeded, want a failure for an unreachable remote")
			}
			if strings.Contains(err.Error(), "FAKEQUERY") || !strings.Contains(err.Error(), "withheld") {
				t.Fatalf("Fetch() error = %v, want git's output withheld", err)
			}
		})
	}
}

// TestClone_AzureDevOpsUserinfo clones the Azure DevOps HTTPS forms. Git's
// output is streamed and kept only for the documented form, whose user is
// the organisation. Every form with a token withholds it and shows "***".
func TestClone_AzureDevOpsUserinfo(t *testing.T) {
	const token = "TOKEN-PLACEHOLDER-123"
	tests := []struct {
		name         string
		url          string
		wantShown    string
		wantWithheld bool
	}{
		{
			name:      "documented form",
			url:       "https://ORG@dev.azure.com/ORG/PROJECT/_git/REPO",
			wantShown: "https://ORG@dev.azure.com/ORG/PROJECT/_git/REPO",
		},
		{
			name:         "user and token",
			url:          "https://USER:" + token + "@dev.azure.com/ORG/PROJECT/_git/REPO",
			wantShown:    "https://***@dev.azure.com/ORG/PROJECT/_git/REPO",
			wantWithheld: true,
		},
		{
			name:         "token in the user slot",
			url:          "https://" + token + "@dev.azure.com/ORG/PROJECT/_git/REPO",
			wantShown:    "https://***@dev.azure.com/ORG/PROJECT/_git/REPO",
			wantWithheld: true,
		},
		{
			name:         "empty user and token",
			url:          "https://:" + token + "@dev.azure.com/ORG/PROJECT/_git/REPO",
			wantShown:    "https://***@dev.azure.com/ORG/PROJECT/_git/REPO",
			wantWithheld: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupGitEnv(t)

			// Redirects the URL to a path that does not exist, so no network is used.
			missing := filepath.Join(t.TempDir(), "missing")
			runGit := exec.CommandContext(context.Background(), "git", "config", "--global", "url."+testenv.FileURL(missing)+".insteadOf", tt.url)
			if out, err := runGit.CombinedOutput(); err != nil {
				t.Fatalf("git config: %v: %s", err, out)
			}
			var progress bytes.Buffer
			err := git.Clone(context.Background(), tt.url, filepath.Join(t.TempDir(), "dest"), &progress)
			if err == nil {
				t.Fatal("Clone() succeeded, want a failure")
			}
			if tt.wantWithheld && progress.Len() != 0 {
				t.Errorf("progress = %q, want git's output withheld", progress.String())
			}
			if !tt.wantWithheld && progress.Len() == 0 {
				t.Errorf("progress is empty, want git's output streamed")
			}
			if got := strings.Contains(err.Error(), "withheld"); got != tt.wantWithheld {
				t.Errorf("withheld = %v, want %v, error: %v", got, tt.wantWithheld, err)
			}
			if !strings.Contains(err.Error(), tt.wantShown) {
				t.Errorf("error = %v, want the URL shown as %q", err, tt.wantShown)
			}
			if strings.Contains(err.Error(), token) || strings.Contains(progress.String(), token) {
				t.Errorf("output carries the token, error: %v, progress: %q", err, progress.String())
			}
		})
	}
}

// TestConfiguredURLsCarryCredential_AzureDevOps covers the configuration a
// documented form shares with a token: every URL counts, in every scope.
func TestConfiguredURLsCarryCredential_AzureDevOps(t *testing.T) {
	t.Parallel()

	const (
		documented = "https://ORG@dev.azure.com/ORG/PROJECT/_git/REPO"
		tokenURL   = "https://TOKEN-PLACEHOLDER-123@dev.azure.com/ORG/PROJECT/_git/REPO"
	)
	tests := []struct {
		name   string
		config string
		want   bool
	}{
		{name: "documented form only", config: "remote.origin.url\n" + documented, want: false},
		{name: "documented form with a push url of the same kind", config: "remote.origin.url\n" + documented + "\x00remote.origin.pushurl\n" + documented, want: false},
		{name: "documented form and a token push url", config: "remote.origin.url\n" + documented + "\x00remote.origin.pushurl\n" + tokenURL, want: true},
		{name: "documented form and another remote with a token", config: "remote.origin.url\n" + documented + "\x00remote.other.url\n" + tokenURL, want: true},
		{name: "rewrite of the documented form", config: "url.file:///missing.insteadof\n" + documented, want: false},
		{name: "rewrite to a token url", config: "url." + tokenURL + ".insteadof\nhttps://elsewhere.test/", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			run := func(...string) ([]byte, error) { return []byte(tt.config + "\x00"), nil }
			if got := git.ConfiguredURLsCarryCredential(run); got != tt.want {
				t.Fatalf("ConfiguredURLsCarryCredential() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRemoteURL(t *testing.T) {
	setupGitEnv(t)

	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatalf("create %s: %v", source, err)
	}
	initRepository(t, source)

	dest := filepath.Join(root, "dest")
	if err := git.Clone(context.Background(), source, dest, io.Discard); err != nil {
		t.Fatalf("Clone(%q, %q) returned an unexpected error: %v", source, dest, err)
	}

	got, err := git.RemoteURL(context.Background(), dest)
	if err != nil {
		t.Fatalf("RemoteURL(%q) returned an unexpected error: %v", dest, err)
	}
	if got != source {
		t.Fatalf("RemoteURL(%q) = %q, want %q", dest, got, source)
	}
}

// TestRemoteURL_IsNotResolvedThroughAnInsteadOfRewrite guards against
// "git remote get-url", which expands a configured "url.<base>.insteadOf"
// rewrite: RemoteURL must return the literal value "git clone" stored,
// the one that was actually requested, not the rewritten one a corporate
// mirror or an SSH host alias might point at instead. gitcd compares this
// value, parsed, against the remote it was just asked to clone, and a
// rewritten target is not guaranteed to parse at all.
func TestRemoteURL_IsNotResolvedThroughAnInsteadOfRewrite(t *testing.T) {
	setupGitEnv(t)

	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatalf("create %s: %v", source, err)
	}
	initRepository(t, source)

	requested := "https://example.test/acme/widget.git"
	run(t, root, "config", "--global", "url."+testenv.FileURL(source)+".insteadOf", requested)

	dest := filepath.Join(root, "dest")
	if err := git.Clone(context.Background(), requested, dest, io.Discard); err != nil {
		t.Fatalf("Clone(%q, %q) returned an unexpected error: %v", requested, dest, err)
	}

	got, err := git.RemoteURL(context.Background(), dest)
	if err != nil {
		t.Fatalf("RemoteURL(%q) returned an unexpected error: %v", dest, err)
	}
	if got != requested {
		t.Fatalf("RemoteURL(%q) = %q, want the literal requested %q, not the rewritten target", dest, got, requested)
	}
}

// TestRemoteURL_FoldsGitsOwnStderrIntoTheError guards against RemoteURL
// wrapping a failing "git config --get" only as the standard library's
// own bare "exit status 128", dropping the diagnostic line git itself
// printed saying why. An ordinary empty, non repository directory
// produces no stderr at all for this exact invocation ("git config
// --get" without "--local" simply reports "key not found" rather than
// requiring a repository), so it cannot exercise this fold; an orphaned
// linked worktree, the same fixture config_test.go's own "folds git's
// own stderr" test for Dir uses, reliably does.
func TestRemoteURL_FoldsGitsOwnStderrIntoTheError(t *testing.T) {
	setupGitEnv(t)

	root := t.TempDir()
	initRepository(t, root)
	worktree := filepath.Join(root, "..", "worktree")
	run(t, root, "worktree", "add", worktree)

	// The main checkout is gone: every git invocation inside the
	// worktree now fails with git's own diagnostic ("fatal: not a git
	// repository: (null)"), rather than succeeding.
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("remove %s: %v", root, err)
	}

	_, err := git.RemoteURL(context.Background(), worktree)
	if err == nil {
		t.Fatal("RemoteURL() on an orphaned linked worktree = nil error, want one naming why")
	}
	if !strings.Contains(err.Error(), "fatal:") {
		t.Fatalf("RemoteURL() error = %q, want git's own stderr folded in, not a bare \"exit status 128\"", err.Error())
	}
}

func TestIsRepository(t *testing.T) {
	setupGitEnv(t)

	t.Run("an ordinary clone is a repository", func(t *testing.T) {
		root := t.TempDir()
		source := filepath.Join(root, "source")
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Fatalf("create %s: %v", source, err)
		}
		initRepository(t, source)

		if !git.IsRepository(source) {
			t.Fatalf("IsRepository(%q) = false, want true", source)
		}
	})

	t.Run("an ordinary directory without a dot git entry is not a repository", func(t *testing.T) {
		dir := t.TempDir()
		if git.IsRepository(dir) {
			t.Fatalf("IsRepository(%q) = true, want false for a plain directory", dir)
		}
	})

	t.Run("a linked worktree is a repository even though its dot git is a file", func(t *testing.T) {
		root := t.TempDir()
		source := filepath.Join(root, "source")
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Fatalf("create %s: %v", source, err)
		}
		initRepository(t, source)

		worktree := filepath.Join(root, "worktree")
		run(t, source, "worktree", "add", worktree)

		info, err := os.Stat(filepath.Join(worktree, ".git"))
		if err != nil {
			t.Fatalf("stat the worktree's dot git entry: %v", err)
		}
		if info.IsDir() {
			t.Fatalf("worktree's dot git entry is a directory, want a file, to exercise the case IsRepository must also cover")
		}
		if !git.IsRepository(worktree) {
			t.Fatalf("IsRepository(%q) = false, want true for a linked worktree", worktree)
		}
	})
}

func TestIsBareRepository(t *testing.T) {
	setupGitEnv(t)

	t.Run("a bare repository is reported as bare", func(t *testing.T) {
		bare := t.TempDir()
		run(t, bare, "init", "--bare", "--initial-branch=main")

		if git.IsRepository(bare) {
			t.Fatalf("IsRepository(%q) = true, want false for a bare repository", bare)
		}
		if !git.IsBareRepository(context.Background(), bare) {
			t.Fatalf("IsBareRepository(%q) = false, want true", bare)
		}
	})

	t.Run("an ordinary clone is not bare", func(t *testing.T) {
		root := t.TempDir()
		initRepository(t, root)

		if git.IsBareRepository(context.Background(), root) {
			t.Fatalf("IsBareRepository(%q) = true, want false for an ordinary clone", root)
		}
	})

	t.Run("a plain directory that is not a repository at all is not bare", func(t *testing.T) {
		dir := t.TempDir()
		if git.IsBareRepository(context.Background(), dir) {
			t.Fatalf("IsBareRepository(%q) = true, want false for a plain directory", dir)
		}
	})
}

func TestWorktrees(t *testing.T) {
	setupGitEnv(t)

	t.Run("an ordinary clone with no linked worktree reports exactly itself", func(t *testing.T) {
		root := t.TempDir()
		source := filepath.Join(root, "source")
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Fatalf("create %s: %v", source, err)
		}
		initRepository(t, source)

		paths, err := git.Worktrees(context.Background(), source)
		if err != nil {
			t.Fatalf("Worktrees() returned an unexpected error: %v", err)
		}
		if len(paths) != 1 {
			t.Fatalf("Worktrees() = %v, want exactly one entry", paths)
		}
	})

	t.Run("a main checkout with a linked worktree reports both", func(t *testing.T) {
		root := t.TempDir()
		source := filepath.Join(root, "source")
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Fatalf("create %s: %v", source, err)
		}
		initRepository(t, source)

		worktree := filepath.Join(root, "worktree")
		run(t, source, "worktree", "add", worktree)

		paths, err := git.Worktrees(context.Background(), source)
		if err != nil {
			t.Fatalf("Worktrees() returned an unexpected error: %v", err)
		}
		if len(paths) != 2 {
			t.Fatalf("Worktrees() = %v, want exactly two entries", paths)
		}

		fromWorktree, err := git.Worktrees(context.Background(), worktree)
		if err != nil {
			t.Fatalf("Worktrees() from the linked worktree returned an unexpected error: %v", err)
		}
		if len(fromWorktree) != 2 {
			t.Fatalf("Worktrees() from the linked worktree = %v, want exactly two entries", fromWorktree)
		}
	})
}
