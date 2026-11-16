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

// allConfigPattern selects every configuration entry. Which entries hold a
// URL is decided in one place, urlLikeKey, so the entries read from the
// configuration files and the "-c" pairs cannot disagree about it.
const allConfigPattern = `.`

// configuredURLsCarryCredential reports whether any URL the configuration
// visible to run can make git use carries a credential. That is the value of
// every key urlLikeKey names, and both the base and the rewritten prefix of
// every insteadOf and pushInsteadOf rule, from every configuration scope,
// since "git fetch" may use the branch's tracked remote, or the only
// remote there is, not just "origin". When the configuration cannot be
// read, it fails closed and reports true. No match at all is not a
// failure: git exits 1 for it.
func configuredURLsCarryCredential(run runner) bool {
	out, err := run("config", "-z", "--get-regexp", allConfigPattern)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false
		}
		return true
	}
	for _, entry := range strings.Split(string(out), "\x00") {
		key, value, _ := strings.Cut(entry, "\n")
		if key == "" || !urlLikeKey(key) {
			continue
		}
		if valueCarriesCredential(key, value) {
			return true
		}
		// The base of a rewrite rule is part of its key.
		if base, ok := rewriteBase(key); ok && carriesCredential(base) {
			return true
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
			if valueCarriesCredential(key, value) {
				return true
			}
		} else if remote.Redact(value) != value {
			// Any other value, such as a user name, is free text: a "?" or
			// a "#" in it is no query.
			return true
		}
		if base, ok := rewriteBase(key); ok && carriesCredential(base) {
			return true
		}
	}
	return false
}

// urlLikeKey reports whether the value of a configuration key is a URL, a
// proxy URL or a remote's address, so a "?" or "#" in it is a query or a
// fragment. A value under any other key is free text, such as a user name
// "Team #1". It is the one place that decides it: the entries read from the
// configuration files and the "-c" pairs both go through it.
//
// Section and variable names are matched case insensitively, as git does.
// The subsection, between the first and the last dot, is a name or a URL.
//
//	key                       value                           why
//	remote.<name>.url         the remote's URL                address git contacts
//	remote.<name>.pushurl     the push URL                    address git pushes to
//	submodule.<name>.url      a submodule's URL               address git contacts
//	<section>.url             a URL (any section)             see below
//	url.<base>.insteadOf      the prefix that is rewritten    URL prefix
//	url.<base>.pushInsteadOf  the prefix that is rewritten    URL prefix
//	http.proxy                a proxy, host[:port][/path]     curl accepts it without a scheme
//	http.<url>.proxy          a proxy, as above               curl accepts it without a scheme
//	remote.<name>.proxy       a proxy, as above               curl accepts it without a scheme
//	fetch.bundleURI           a bundle URI                    address git downloads from
//	bundle.<id>.uri           a bundle URI                    address git downloads from
//
// The ".url" suffix is not limited to the sections above. It also matches a
// key such as "alias.url" or "gitweb.url", whose value is then read as a
// URL: a value with a "?" or a "#" under such a key is treated as a query.
//
// Left out on purpose: core.gitProxy and credential.helper hold a command,
// and http.extraHeader a header line, all free text. A key whose subsection
// is a URL, as in "http.<url>.sslVerify", only selects which URL a setting
// applies to: only its value is read, and for "url.<base>.insteadOf" the
// base, see rewriteBase.
func urlLikeKey(key string) bool {
	key = strings.ToLower(key)
	if key == "fetch.bundleuri" || isProxyKey(key) {
		return true
	}
	for _, suffix := range []string{".url", ".pushurl", ".insteadof", ".pushinsteadof"} {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return strings.HasPrefix(key, "bundle.") && strings.HasSuffix(key, ".uri")
}

// isProxyKey reports whether key sets a proxy: "http.proxy",
// "http.<url>.proxy" or "remote.<name>.proxy". The section and the variable
// are matched case insensitively.
func isProxyKey(key string) bool {
	key = strings.ToLower(key)
	return (strings.HasPrefix(key, "http.") || strings.HasPrefix(key, "remote.")) && strings.HasSuffix(key, ".proxy")
}

// valueCarriesCredential reports whether the value of the key holds a
// credential, a query or a fragment. A proxy may be written without a scheme,
// as "user:secret@host:3128" or "host:3128/path?query", which curl reads as a
// proxy URL: it is checked as if it began with "http://", so its user name,
// password, query and fragment are found. A user name without a password
// counts too, as it does in any other URL. An empty value, which turns the
// proxy off, and a plain "host:port" carry nothing.
func valueCarriesCredential(key, value string) bool {
	if isProxyKey(key) && value != "" && !strings.Contains(value, "://") {
		value = "http://" + value
	}
	return carriesCredential(value)
}

// rewriteBase returns the base of a "url.<base>.insteadOf" or
// "url.<base>.pushInsteadOf" key, which is part of the key itself. The
// section and the variable are matched case insensitively, the base keeps
// its case, as git does.
func rewriteBase(key string) (string, bool) {
	const section = "url."
	if len(key) < len(section) || !strings.EqualFold(key[:len(section)], section) {
		return "", false
	}
	rest := key[len(section):]
	i := strings.LastIndexByte(rest, '.')
	if i < 0 {
		return "", false
	}
	switch strings.ToLower(rest[i+1:]) {
	case "insteadof", "pushinsteadof":
		return rest[:i], true
	}
	return "", false
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
