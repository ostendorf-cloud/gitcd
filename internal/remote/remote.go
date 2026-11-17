// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package remote parses a git remote into host, owner, project and
// repository. It accepts an SSH URL, an HTTPS URL, the scp style
// "git@host:owner/repo.git", the Azure DevOps "_git" form and the bare
// "host/owner/repo" shorthand. The host is lowercased and resolved through
// an alias map, because two accounts on one forge are commonly driven by
// distinct SSH host aliases bound to distinct keys.
package remote

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Azure DevOps host spellings the parser recognises on sight, independent
// of any configured alias: the canonical host, its SSH host and the legacy
// Visual Studio SSH host. A configured alias mapping a further spelling to
// azureHost is honoured too, through isAzureDevOps's first check.
const (
	azureHost             = "dev.azure.com"
	azureSSHHost          = "ssh.dev.azure.com"
	azureVisualStudioHost = "vs-ssh.visualstudio.com"
)

// Remote is a git remote resolved into the fields a layout template draws
// from. Project is set only for a host whose path carries one, Azure DevOps
// in this milestone.
type Remote struct {
	// Host is lowercased and alias resolved.
	Host string
	// Owner may contain slashes, for a GitLab subgroup.
	Owner string
	// Project is set for Azure DevOps and empty everywhere else.
	Project string
	Repo    string
}

var (
	// ErrMalformedURL is returned for a URL net/url cannot parse. Parse
	// errors never quote any part of the input, since a failed parse
	// cannot tell a credential from the rest of it.
	ErrMalformedURL = errors.New("the remote is not a valid URL")
	// ErrAmbiguousUserinfo is returned for a URL with an "@" after its
	// authority that has no userinfo of its own, as in a path
	// "/acme/r3@v2.git". The "@" cannot be told apart from the end of a
	// credential that holds a "/", so the remote is refused. Writing the
	// "@" percent encoded, as %40, makes it parse.
	ErrAmbiguousUserinfo = errors.New(
		"the remote has an @ outside a user name, which cannot be told apart from a credential: write it percent encoded as %40",
	)
)

// Parse resolves input into a Remote. aliases maps a lowercased host alias
// to the lowercased canonical host it stands for; a nil or empty map
// disables alias resolution and every host is used as written, lowercased.
func Parse(input string, aliases map[string]string) (Remote, error) {
	trimmed := strings.TrimSpace(input)
	// A URL's query and fragment are split off by url.Parse. The scp style
	// and the shorthand have no such split, so it is made here: whatever
	// follows the first "?" or "#" is not part of the remote.
	if i := strings.IndexAny(trimmed, "?#"); i >= 0 && !strings.Contains(trimmed[:i], "://") {
		trimmed = strings.TrimSpace(trimmed[:i])
	}
	if trimmed == "" {
		return Remote{}, fmt.Errorf("remote: input is empty")
	}

	rawHost, rawPath, err := classify(trimmed)
	if err != nil {
		return Remote{}, err
	}
	if rawHost == "" {
		return Remote{}, fmt.Errorf("remote: the remote has no host")
	}

	rawHostLower := strings.ToLower(rawHost)
	host := rawHostLower
	if canonical, ok := aliases[rawHostLower]; ok {
		host = canonical
	}

	rawPath = strings.TrimSuffix(rawPath, ".git")
	segments := splitPathSegments(rawPath)

	if isAzureDevOps(host, rawHostLower) {
		sshForm := len(segments) > 0 && segments[0] == "v3"
		if sshForm {
			segments = segments[1:]
		}
		return parseAzure(host, segments, sshForm, IsShorthand(trimmed))
	}

	if len(segments) < 2 {
		return Remote{}, fmt.Errorf("remote: the remote has no owner and repository")
	}
	repo := segments[len(segments)-1]
	owner := strings.Join(segments[:len(segments)-1], "/")
	return Remote{Host: host, Owner: owner, Repo: repo}, nil
}

// IsShorthand reports whether input is the bare "host/owner/repo" form
// rather than a URL or the scp style syntax. Parse accepts it, but "git
// clone" cannot fetch it directly, since it names no protocol and, for an
// SSH host reached through an alias, no identity either: a caller that
// needs to clone a shorthand input must build a URL from the parsed Remote
// instead, with Remote.URL.
func IsShorthand(input string) bool {
	if strings.Contains(input, "://") {
		return false
	}
	colon := strings.Index(input, ":")
	slash := strings.Index(input, "/")
	return colon == -1 || (slash != -1 && colon >= slash)
}

// URL renders r back into a URL "git clone" can fetch, for protocol "ssh"
// or "https" ("" defaults to "https"). It is Parse's inverse, needed only
// for a remote Parse read from the bare shorthand, which carries no
// protocol and no SSH host alias of its own.
//
// An Azure DevOps SSH clone always targets the literal "ssh.dev.azure.com",
// regardless of r.Host: that is the service's one SSH endpoint, and a
// shorthand input never names an alias that could stand in for it.
func (r Remote) URL(protocol string) (string, error) {
	if protocol == "" {
		protocol = "https"
	}

	switch protocol {
	case "ssh":
		if r.Project != "" {
			// Percent encoded like the HTTPS form. Whether the SSH endpoint
			// also takes a raw space is not verified, the encoded form is
			// the one Parse and the rule conditions read back either way.
			return fmt.Sprintf("git@%s:v3/%s/%s/%s", azureSSHHost, r.Owner, url.PathEscape(r.Project), r.Repo), nil
		}
		return fmt.Sprintf("git@%s:%s/%s.git", r.Host, r.Owner, r.Repo), nil
	case "https":
		if r.Project != "" {
			// A project with a space is written %20 in an Azure DevOps HTTPS
			// URL, and Parse reads it back decoded.
			return fmt.Sprintf("https://%s/%s/%s/_git/%s", r.Host, r.Owner, url.PathEscape(r.Project), r.Repo), nil
		}
		return fmt.Sprintf("https://%s/%s/%s.git", r.Host, r.Owner, r.Repo), nil
	default:
		return "", fmt.Errorf("remote: %q is not ssh or https", protocol)
	}
}

// WebURL renders r into the URL of its web page: the same shape as its
// "https" clone URL, minus a trailing ".git", since that is the one path
// shape GitHub, GitLab, Bitbucket, Gitea and most other forges share for
// a repository's own page, Azure DevOps's "_git" path already included.
// A host with a different shape of its own is not something this
// package knows: the generic form is reported anyway, so a caller still
// opens something plausible rather than nothing at all.
func (r Remote) WebURL() string {
	httpsURL, _ := r.URL("https")
	return strings.TrimSuffix(httpsURL, ".git")
}

// classify splits input into a raw, un-lowercased host and a raw path, for
// an SSH or HTTPS URL, the scp style "[user@]host:path" and the bare
// "host/path" shorthand. It does not resolve aliases or interpret the path.
func classify(input string) (host, path string, err error) {
	if strings.Contains(input, "://") {
		// The error url.Parse returns quotes parts of the input, so it is
		// replaced by a fixed one instead of being passed on.
		u, perr := url.Parse(input)
		if perr != nil {
			return "", "", fmt.Errorf("remote: %w", ErrMalformedURL)
		}
		// Redact reads an "@" after the authority as userinfo, a "/"
		// inside a credential included. When Go's own parse found no
		// userinfo but Redact did, the input is one of those shapes and
		// url.Parse took the credential for the host or the path. It is
		// refused, not parsed, so the credential never becomes a host
		// that later shows up in a path or an error.
		// Any "@" counts, one in a query or fragment too: a user name
		// holding a "?" would otherwise parse with its first part as the
		// host.
		if u.User == nil && strings.Contains(input, "@") {
			return "", "", fmt.Errorf("remote: %w", ErrAmbiguousUserinfo)
		}
		return u.Hostname(), u.Path, nil
	}

	colon := strings.Index(input, ":")
	slash := strings.Index(input, "/")
	if colon != -1 && (slash == -1 || colon < slash) {
		hostPart := input[:colon]
		if at := strings.LastIndex(hostPart, "@"); at != -1 {
			hostPart = hostPart[at+1:]
		}
		return hostPart, input[colon+1:], nil
	}

	if slash == -1 {
		return "", "", fmt.Errorf("remote: not a recognised remote")
	}
	return input[:slash], input[slash+1:], nil
}

// splitPathSegments trims the leading and trailing slash from path and
// splits what remains on "/", dropping an empty segment a doubled slash
// would otherwise leave behind.
func splitPathSegments(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	parts := strings.Split(path, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// isAzureDevOps reports whether host or rawHost names an Azure DevOps
// remote: host is the alias resolved, canonical host, rawHost is the host
// exactly as the input spelled it, before resolution.
func isAzureDevOps(host, rawHost string) bool {
	if host == azureHost {
		return true
	}
	switch rawHost {
	case azureSSHHost, azureVisualStudioHost:
		return true
	default:
		return false
	}
}

// unescapeSegment percent decodes one path segment, returning it as written
// when it holds a malformed escape: the segment is then taken literally,
// the way git itself passes the path on.
func unescapeSegment(segment string) string {
	decoded, err := url.PathUnescape(segment)
	if err != nil {
		return segment
	}
	return decoded
}

// parseAzure reads the Azure DevOps SSH path "{org}/{project}/{repo}" when
// sshForm is set, the leading "v3" segment already stripped by the caller;
// otherwise the HTTPS path "{org}/{project}/_git/{repo}" from segments, or,
// when input was the bare "host/org/project/repo" shorthand, the same three
// segments with no "_git" to require, since the shorthand has no room to
// carry it and that is exactly what the "{host}/{org}/{project}/{repo}"
// layout renders back out: a caller round tripping its own rendered path
// must be able to read it back. The "v3" prefix is what tells the SSH and
// HTTPS shapes apart otherwise: without it, a path three segments long is
// malformed rather than guessed at as SSH shaped, unless it was the
// shorthand to begin with.
func parseAzure(host string, segments []string, sshForm, shorthand bool) (Remote, error) {
	if sshForm {
		if len(segments) != 3 {
			return Remote{}, fmt.Errorf("remote: not a recognised Azure DevOps SSH path")
		}
		// net/url decodes the HTTPS path, the scp style path is raw, and the
		// Azure DevOps SSH URL can carry a name with a space written as %20.
		// Decoding here gives both forms of one repository the same project.
		return Remote{
			Host: host, Owner: unescapeSegment(segments[0]), Project: unescapeSegment(segments[1]), Repo: unescapeSegment(segments[2]),
		}, nil
	}
	if shorthand && len(segments) == 3 {
		// Decoded like the SSH form, so "my%20project" is the project
		// "my project", and Remote.URL does not encode it a second time.
		return Remote{
			Host: host, Owner: unescapeSegment(segments[0]), Project: unescapeSegment(segments[1]), Repo: unescapeSegment(segments[2]),
		}, nil
	}
	if len(segments) != 4 || segments[2] != "_git" {
		return Remote{}, fmt.Errorf("remote: not a recognised Azure DevOps HTTPS path")
	}
	return Remote{Host: host, Owner: segments[0], Project: segments[1], Repo: segments[3]}, nil
}
