// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package layout renders a path template against a remote and maps a path
// back to the remote it encodes. A template is a "/" separated list of
// literal segments and placeholders: "{host}", "{owner}" ("{org}" is
// accepted as the same field, for an Azure DevOps template), "{project}"
// and "{repo}".
package layout

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ostendorf-cloud/gitcd/internal/remote"
)

var placeholderPattern = regexp.MustCompile(`\{(\w+)\}`)

// fieldName normalises a template placeholder to the Remote field it reads:
// "org" is accepted as a synonym for "owner", because the Azure DevOps
// layout in the design reads more naturally with "{org}".
func fieldName(name string) string {
	if name == "org" {
		return "owner"
	}
	return name
}

// Render substitutes every placeholder in tmpl with the matching field of
// r and returns the resulting path, "/" separated and without a leading or
// trailing slash. It fails on an unknown placeholder or one whose field is
// empty on r, since an empty segment would otherwise collapse two
// slashes together.
func Render(tmpl string, r remote.Remote) (string, error) {
	fields := map[string]string{
		"host":    r.Host,
		"owner":   r.Owner,
		"project": r.Project,
		"repo":    r.Repo,
	}

	var missing, unknown string
	result := placeholderPattern.ReplaceAllStringFunc(tmpl, func(match string) string {
		name := fieldName(placeholderPattern.FindStringSubmatch(match)[1])
		value, known := fields[name]
		if !known {
			unknown = name
			return match
		}
		if value == "" {
			missing = name
		}
		return value
	})
	if unknown != "" {
		return "", fmt.Errorf("layout: template %q uses the unknown placeholder %q", tmpl, unknown)
	}
	if missing != "" {
		return "", fmt.Errorf("layout: template %q needs %q, which this remote has none of", tmpl, missing)
	}

	return strings.Trim(result, "/"), nil
}

// Template names one host's path template, "" for the default applied to
// every host without one of its own.
type Template struct {
	// Host is the canonical host this template applies to, or "" for the
	// default template.
	Host     string
	Template string
}

// ParsePath maps path, "/" separated and relative to the configured root,
// back to the Remote it was rendered from. It tries every host specific
// template before the default, since a host specific template is the more
// precise match. An owner placeholder absorbs any path segment a fixed
// placeholder does not claim, so a GitLab subgroup owner round trips.
func ParsePath(path string, templates []Template, defaultTemplate string) (remote.Remote, error) {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return remote.Remote{}, fmt.Errorf("layout: path is empty")
	}
	parts := strings.Split(trimmed, "/")

	for _, t := range templates {
		// A host specific template's own rendered host is a literal, so a
		// path cannot match it unless that literal is actually the path's
		// first segment; matchTemplate itself only checks the shape.
		if t.Host == "" || len(parts) == 0 || parts[0] != t.Host {
			continue
		}
		if r, ok := matchTemplate(t.Template, parts); ok {
			r.Host = t.Host
			return r, nil
		}
	}

	if r, ok := matchTemplate(defaultTemplate, parts); ok {
		return r, nil
	}

	return remote.Remote{}, fmt.Errorf("layout: %q matches no configured template", path)
}

// matchTemplate tries to align parts against tmpl's segments, returning the
// Remote it implies and whether the shapes matched at all.
func matchTemplate(tmpl string, parts []string) (remote.Remote, bool) {
	segs := strings.Split(strings.Trim(tmpl, "/"), "/")

	ownerIdx := -1
	for i, s := range segs {
		if name, ok := placeholderName(s); ok && fieldName(name) == "owner" {
			ownerIdx = i
			break
		}
	}

	overflow := len(parts) - len(segs)
	switch {
	case overflow == 0:
		// Exact shape, nothing to absorb.
	case overflow > 0 && ownerIdx >= 0:
		// The extra segments belong to the owner placeholder, a subgroup.
	default:
		return remote.Remote{}, false
	}

	r := remote.Remote{}
	partIdx := 0
	for _, seg := range segs {
		name, isPlaceholder := placeholderName(seg)
		if !isPlaceholder {
			if partIdx >= len(parts) || parts[partIdx] != seg {
				return remote.Remote{}, false
			}
			partIdx++
			continue
		}

		span := 1
		if ownerIdx >= 0 && fieldName(name) == "owner" {
			span = 1 + overflow
		}
		if partIdx+span > len(parts) {
			return remote.Remote{}, false
		}
		value := strings.Join(parts[partIdx:partIdx+span], "/")
		partIdx += span

		switch fieldName(name) {
		case "host":
			r.Host = value
		case "owner":
			r.Owner = value
		case "project":
			r.Project = value
		case "repo":
			r.Repo = value
		}
	}

	if partIdx != len(parts) {
		return remote.Remote{}, false
	}
	return r, true
}

// placeholderName reports the placeholder name a whole template segment
// holds, and whether seg is a placeholder at all rather than a literal.
func placeholderName(seg string) (string, bool) {
	m := placeholderPattern.FindStringSubmatch(seg)
	if m == nil || m[0] != seg {
		return "", false
	}
	return m[1], true
}
