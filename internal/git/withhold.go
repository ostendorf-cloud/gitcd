// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	"github.com/ostendorf-cloud/gitcd/internal/remote"
)

// withheldOutput replaces git's output in an error when the remote it
// worked on carries a credential.
const withheldOutput = "git failed; its output is withheld because the remote carries a credential"

// runner runs git with args and returns its standard output. It is a value
// so a test can stand in for git.
type runner func(args ...string) ([]byte, error)

// gitIn returns a runner that runs git in the repository at path, or in the
// current directory when path is empty.
func gitIn(ctx context.Context, path string) runner {
	return func(args ...string) ([]byte, error) {
		if path != "" {
			args = append([]string{"-C", path}, args...)
		}
		return exec.CommandContext(ctx, "git", args...).Output()
	}
}

// carriesCredential reports whether remoteURL has userinfo, a query or a
// fragment, which is whenever remote.RedactRemote changes it, or is a URL that
// could not be parsed and so may hold a credential gitcd cannot find.
func carriesCredential(remoteURL string) bool {
	if remote.RedactRemote(remoteURL) != remoteURL {
		return true
	}
	_, err := remote.Parse(remoteURL, nil)
	return errors.Is(err, remote.ErrMalformedURL) || errors.Is(err, remote.ErrAmbiguousUserinfo)
}

// urlConfigPattern selects every configuration entry that can put a URL
// in front of git: the URL and push URL of any remote, and the rewrite
// rules "url.<base>.insteadOf" and "url.<base>.pushInsteadOf", whose base
// replaces a prefix of the remote's own URL.
const urlConfigPattern = `^(remote\..*\.(url|pushurl)|url\..*\.(insteadof|pushinsteadof))$`

// configuredURLsCarryCredential reports whether any URL the configuration
// visible to run can make git use carries a credential. That is every
// remote's url and pushurl, and both the base and the rewritten prefix of
// every insteadOf and pushInsteadOf rule, from every configuration scope,
// since "git fetch" may use the branch's tracked remote, or the only
// remote there is, not just "origin". When the configuration cannot be
// read, it fails closed and reports true. No match at all is not a
// failure: git exits 1 for it.
func configuredURLsCarryCredential(run runner) bool {
	out, err := run("config", "-z", "--get-regexp", urlConfigPattern)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false
		}
		return true
	}
	for _, entry := range strings.Split(string(out), "\x00") {
		key, value, _ := strings.Cut(entry, "\n")
		if key == "" {
			continue
		}
		if carriesCredential(value) {
			return true
		}
		// The base of a rewrite rule is part of its key.
		if rest, ok := strings.CutPrefix(key, "url."); ok {
			if i := strings.LastIndexByte(rest, '.'); i >= 0 && carriesCredential(rest[:i]) {
				return true
			}
		}
	}
	return false
}

// configPairsCarryCredential reports whether a "-c key=value" pair of config
// puts a credential in front of git: in its value, or in the base of a
// "url.<base>.insteadOf" key.
func configPairsCarryCredential(config map[string]string) bool {
	for key, value := range config {
		if urlLikeKey(key) || strings.Contains(value, "://") {
			if carriesCredential(value) {
				return true
			}
		} else if remote.Redact(value) != value {
			// Any other value, such as a user name, is free text: a "?" or
			// a "#" in it is no query.
			return true
		}
		if rest, ok := strings.CutPrefix(key, "url."); ok {
			if i := strings.LastIndexByte(rest, '.'); i >= 0 && carriesCredential(rest[:i]) {
				return true
			}
		}
	}
	return false
}

// urlLikeKey reports whether a configuration key holds a URL: a remote's url
// or pushurl, or the "insteadOf" and "pushInsteadOf" of a rewrite rule.
func urlLikeKey(key string) bool {
	key = strings.ToLower(key)
	for _, suffix := range []string{".url", ".pushurl", ".insteadof", ".pushinsteadof"} {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}

// foldOutput returns the text of git's output to fold into an error for a
// command that talks to a remote, or withheldOutput when withhold is set.
// It is the one place every error that carries git output goes through.
// When a remote carries a credential, no part of the output is kept: git
// may repeat part of the credential outside any URL, as in "Could not
// resolve host: <part of the password>", which no pattern on the text can
// find. Otherwise the output is kept with any URL userinfo redacted.
func foldOutput(output string, withhold bool) string {
	if withhold {
		return withheldOutput
	}
	return remote.Redact(strings.TrimSpace(output))
}
