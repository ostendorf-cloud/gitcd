// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"net/url"
	"regexp"
	"strings"
)

// redacted replaces the userinfo of a URL that may carry a credential.
const redacted = "***"

// schemePattern matches "scheme://", where a URL's authority starts.
var schemePattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://`)

// plainUserPattern matches a user name that cannot carry a secret: letters,
// digits, dot, underscore and hyphen.
var plainUserPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// urlBoundary holds the characters that may stand directly before a URL in
// free text.
const urlBoundary = " \t\r\n'\"<>()[]{},;="

// Redact returns s with the userinfo of every "scheme://userinfo@host" URL in
// it replaced by "***". Every call site that logs a remote, builds an error
// that names one or prints one uses it, so a token or a password a remote
// carries never leaves the process through gitcd itself. s may be a single
// URL or free text such as git's own error output.
//
// It does not rely on a well formed authority, and it has no special cases.
// After "scheme://", the text up to the next "scheme://" or the end of s is
// one region, and everything in it up to its last "@" is userinfo, whatever
// characters it holds: a "/", "?", "#", white space and the rest may all
// sit unescaped in a password or token. The price is that every other "@"
// is read as userinfo too and redacted: one in a path, a query, or in text
// that follows the URL, as in "https://host/a@b", "https://host/x?m=a@b"
// or "https://host/x: see admin@corp". A display function prefers hiding
// too much to leaking, and Parse refuses an "@" outside a user name.
//
// A second "scheme://" starts a new region only when white space, a quote,
// a bracket, "=" or "," precedes it. A password holding "://", as in
// "https://user:SEC://RET@host/x", is redacted whole. The cost: two URLs
// run together with no such character between them are read as one.
//
// An "ssh" URL whose userinfo is a plain user name, as in
// "ssh://git@host/x", is left alone: the user name there names the account,
// not a secret. The userinfo is percent decoded before that decision, so
// "user%3aSECRET" counts as a password. The scp style "git@host:path"
// carries no scheme and is left alone for the same reason. The second and
// last exemption is the Azure DevOps clone URL whose user is the
// organisation, see isAzureOrganisationUser.
func Redact(s string) string {
	var locs [][]int
	for _, loc := range schemePattern.FindAllStringIndex(s, -1) {
		// A "scheme://" that follows a character other than white space or
		// a quote or bracket is part of the text before it, as in a
		// password "SEC://RET", and does not start a new URL. The first
		// one always counts.
		if len(locs) == 0 || strings.ContainsRune(urlBoundary, rune(s[loc[0]-1])) {
			locs = append(locs, loc)
		}
	}
	if locs == nil {
		return s
	}

	var b strings.Builder
	pos := 0
	for i, loc := range locs {
		if loc[0] < pos {
			continue
		}
		start := loc[1]
		end := len(s)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		at := strings.LastIndexByte(s[start:end], '@')
		if at < 0 {
			continue
		}
		scheme := s[loc[0] : loc[1]-len("://")]
		userinfo := s[start : start+at]
		if strings.EqualFold(scheme, "ssh") && isPlainUser(userinfo) {
			continue
		}
		if isAzureOrganisationUser(scheme, userinfo, s[start+at+1:end]) {
			continue
		}
		b.WriteString(s[pos:loc[0]])
		b.WriteString(scheme + "://" + redacted + "@")
		pos = start + at + 1
	}
	b.WriteString(s[pos:])
	return b.String()
}

// isAzureOrganisationUser reports whether "scheme://userinfo@rest" is the
// clone URL Azure DevOps itself hands out, "https://ORG@dev.azure.com/ORG/...",
// whose user is the organisation and no secret. It is the one exemption from
// redaction beside a plain ssh user, and the only place that decides it:
// Redact calls it, and the withholding of git's output asks Redact, so the
// two cannot disagree. All of these must hold, else the userinfo stays
// redacted:
//   - the scheme is https and the host is exactly dev.azure.com, no port
//   - the userinfo has no password: it is a plain user name once percent
//     decoded
//   - the first path segment is there and equals the decoded user, ignoring
//     case
//
// rest is the text after the "@", up to the end of the region Redact looks
// at. A token in the user slot passes only if the path repeats it as its
// first segment, where it is visible in the URL anyway.
func isAzureOrganisationUser(scheme, userinfo, rest string) bool {
	if !strings.EqualFold(scheme, "https") {
		return false
	}
	user, err := url.PathUnescape(userinfo)
	if err != nil || !plainUserPattern.MatchString(user) {
		return false
	}
	host, path, ok := strings.Cut(rest, "/")
	if !ok || !strings.EqualFold(host, azureHost) {
		return false
	}
	// The organisation segment ends at the next separator or at any
	// character that cannot be part of a URL.
	org, _, _ := strings.Cut(path, "/")
	if i := strings.IndexAny(org, urlBoundary+"?#"); i >= 0 {
		org = org[:i]
	}
	org, err = url.PathUnescape(org)
	return err == nil && org != "" && strings.EqualFold(user, org)
}

// isPlainUser reports whether userinfo, once percent decoded, is a plain
// user name: no colon, no slash, nothing a secret would need.
func isPlainUser(userinfo string) bool {
	decoded, err := url.PathUnescape(userinfo)
	if err != nil {
		return false
	}
	return plainUserPattern.MatchString(decoded)
}
