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

// Parse resolves input into a Remote. aliases maps a lowercased host alias
// to the lowercased canonical host it stands for; a nil or empty map
// disables alias resolution and every host is used as written, lowercased.
func Parse(input string, aliases map[string]string) (Remote, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return Remote{}, fmt.Errorf("remote: input is empty")
	}

	rawHost, rawPath, err := classify(trimmed)
	if err != nil {
		return Remote{}, err
	}
	if rawHost == "" {
		return Remote{}, fmt.Errorf("remote: %q has no host", trimmed)
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
		return parseAzure(host, segments, sshForm, IsShorthand(trimmed), trimmed)
	}

	if len(segments) < 2 {
		return Remote{}, fmt.Errorf("remote: %q has no owner and repository", trimmed)
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
			return fmt.Sprintf("git@%s:v3/%s/%s/%s", azureSSHHost, r.Owner, r.Project, r.Repo), nil
		}
		return fmt.Sprintf("git@%s:%s/%s.git", r.Host, r.Owner, r.Repo), nil
	case "https":
		if r.Project != "" {
			return fmt.Sprintf("https://%s/%s/%s/_git/%s", r.Host, r.Owner, r.Project, r.Repo), nil
		}
		return fmt.Sprintf("https://%s/%s/%s.git", r.Host, r.Owner, r.Repo), nil
	default:
		return "", fmt.Errorf("remote: %q is not ssh or https", protocol)
	}
}

// classify splits input into a raw, un-lowercased host and a raw path, for
// an SSH or HTTPS URL, the scp style "[user@]host:path" and the bare
// "host/path" shorthand. It does not resolve aliases or interpret the path.
func classify(input string) (host, path string, err error) {
	if strings.Contains(input, "://") {
		u, perr := url.Parse(input)
		if perr != nil {
			return "", "", fmt.Errorf("remote: parse %q: %w", input, perr)
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
		return "", "", fmt.Errorf("remote: %q is not a recognised remote", input)
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
func parseAzure(host string, segments []string, sshForm, shorthand bool, input string) (Remote, error) {
	if sshForm {
		if len(segments) != 3 {
			return Remote{}, fmt.Errorf("remote: %q is not a recognised Azure DevOps SSH path", input)
		}
		return Remote{Host: host, Owner: segments[0], Project: segments[1], Repo: segments[2]}, nil
	}
	if shorthand && len(segments) == 3 {
		return Remote{Host: host, Owner: segments[0], Project: segments[1], Repo: segments[2]}, nil
	}
	if len(segments) != 4 || segments[2] != "_git" {
		return Remote{}, fmt.Errorf("remote: %q is not a recognised Azure DevOps HTTPS path", input)
	}
	return Remote{Host: host, Owner: segments[0], Project: segments[1], Repo: segments[3]}, nil
}
