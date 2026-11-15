// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// redacted replaces the userinfo, the query and the fragment of a URL.
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
// it replaced by "***". The text of such a URL from its first "?" or "#" up
// to the end of its token is replaced by "***" too, the marker kept, as in
// "https://host/path?***". Every call site that logs a remote, builds an error
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
// The query and fragment rule has no list of parameter names. It starts after
// the userinfo. A token ends at white space, a quote or an angle bracket.
// A "scheme://" inside a query or fragment is part of it, not a new URL,
// and a URL after white space is read on its own. An scp style remote "user@host:path" is masked the same way
// when it is the whole string. Inside free text it has no scheme to find it
// by and is left alone. A URL with a query or a fragment differs from its
// redacted form, so git's output for it is withheld like for a credential.
//
// An "ssh" URL whose userinfo is a plain user name, as in
// "ssh://git@host/x", is left alone: the user name there names the account,
// not a secret. The userinfo is percent decoded before that decision, so
// "user%3aSECRET" counts as a password. The scp style "git@host:path"
// carries no scheme and is left alone for the same reason. The second and
// last exemption is the Azure DevOps clone URL whose user is the
// organisation, see isAzureOrganisationUser.
func Redact(s string) string {
	return redact(s, false)
}

// RedactRemote is Redact for a value that is one remote as a whole, not free
// text: a configured url, a push url, either side of a rewrite rule, a "-c"
// value, a clone argument or the remote a command lists. The userinfo rule is
// the same. The query and fragment rule differs: everything from the first
// "?" or "#" to the end of the string is replaced by "***", whatever
// characters follow, white space and quotes included, since nothing after it
// is text that merely follows a URL. It also masks a remote without a scheme,
// the scp style with or without a user, the bare "host/owner/repo" shorthand
// and a path, so a configured value of any shape that carries a "?" or "#"
// differs from its redacted form. Output of git goes through Redact.
func RedactRemote(s string) string {
	return redact(s, true)
}

// redact implements Redact and RedactRemote. whole selects the second.
func redact(s string, whole bool) string {
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
		if whole {
			if q := strings.IndexAny(s, "?#"); q >= 0 {
				return s[:q+1] + redacted
			}
			return s
		}
		return maskScpQuery(s)
	}
	// A whole remote whose "?" or "#" comes before its first "scheme://" has
	// no scheme of its own, as in the scp style with a URL in its query.
	if whole {
		if q := strings.IndexAny(s, "?#"); q >= 0 && q < locs[0][0] {
			return s[:q+1] + redacted
		}
	}

	var b strings.Builder
	pos := 0
	// scanFrom and scanEnd remember the last token end search: the first
	// token end at or after scanFrom is scanEnd, len(s) when there is none.
	// The initial values match no position.
	scanFrom, scanEnd := 1, 0
	for i, loc := range locs {
		if loc[0] < pos {
			continue
		}
		start := loc[1]
		end := len(s)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		// hier is where the text after the userinfo starts.
		hier := start
		if at := strings.LastIndexByte(s[start:end], '@'); at >= 0 {
			scheme := s[loc[0] : loc[1]-len("://")]
			userinfo := s[start : start+at]
			exempt := strings.EqualFold(scheme, "ssh") && isPlainUser(userinfo)
			if !exempt && !isAzureOrganisationUser(scheme, userinfo, s[start+at+1:end]) {
				b.WriteString(s[pos:loc[0]])
				b.WriteString(scheme + "://" + redacted + "@")
				pos = start + at + 1
			}
			hier = start + at + 1
		}
		// In free text the token ends at white space, a quote or an angle
		// bracket, and in a whole remote at the end of s. It is measured
		// past this region's end: a "scheme://" inside a query belongs to
		// the query, not to a new URL, so the nested one is skipped.
		tokenEnd := len(s)
		if !whole {
			// The first token end at or after hier is the one found for an
			// earlier region when it lies at or after hier, since nothing
			// between the two ends a token. Reusing it keeps the scan one
			// pass over s, however many regions it holds.
			if hier >= scanFrom && hier <= scanEnd {
				tokenEnd = scanEnd
			} else {
				if j := strings.IndexFunc(s[hier:], isTokenEnd); j >= 0 {
					tokenEnd = hier + j
				}
				scanFrom, scanEnd = hier, tokenEnd
			}
		}
		if q := strings.IndexAny(s[hier:min(end, tokenEnd)], "?#"); q >= 0 {
			b.WriteString(s[pos : hier+q+1])
			b.WriteString(redacted)
			pos = tokenEnd
		}
	}
	b.WriteString(s[pos:])
	return b.String()
}

// isTokenEnd reports whether r ends a URL in free text: white space, a
// quote or an angle bracket.
func isTokenEnd(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune(`'"<>`, r)
}

// scpPattern matches a whole string that is one scp style remote with a
// user, "user@host:path".
var scpPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+@[^\s/:@?#]+:\S*$`)

// maskScpQuery masks the query or fragment of s when s as a whole is an scp
// style remote. That form has no scheme to find it by, so inside free text
// it is left alone.
func maskScpQuery(s string) string {
	if !scpPattern.MatchString(s) {
		return s
	}
	if q := strings.IndexAny(s, "?#"); q >= 0 {
		return s[:q+1] + redacted
	}
	return s
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
//   - nothing after the host contains a "?" or a "#", so a URL with a query
//     or a fragment, where a secret may sit, is never exempt. A percent
//     encoded %3F or %23 is path text for git, not a query: it does not
//     stop the exemption, except in the organisation segment, where the
//     decoded name no longer equals a plain user.
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
	// A query or a fragment anywhere after the host may carry a secret, as in
	// "?access_token=...", and git echoes the whole URL: never exempt.
	if strings.ContainsAny(rest, "?#") {
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
