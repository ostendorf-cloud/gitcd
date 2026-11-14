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
	"unicode"

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
// trailing slash. It fails on an unknown placeholder, on one whose field is
// empty on r since an empty segment would otherwise collapse two slashes
// together, on the reverse: a field r carries that tmpl has no placeholder
// for, since that field would otherwise be silently dropped from the
// rendered path, which is how two different remotes can end up sharing one
// path, and on a field whose value cannot name a directory inside the
// root, checked by validateField.
func Render(tmpl string, r remote.Remote) (string, error) {
	fields := map[string]string{
		"host":    r.Host,
		"owner":   r.Owner,
		"project": r.Project,
		"repo":    r.Repo,
	}

	used := map[string]bool{}
	var missing, unknown string
	var invalid error
	result := placeholderPattern.ReplaceAllStringFunc(tmpl, func(match string) string {
		name := fieldName(placeholderPattern.FindStringSubmatch(match)[1])
		used[name] = true
		value, known := fields[name]
		if !known {
			unknown = name
			return match
		}
		if value == "" {
			missing = name
			return value
		}
		if err := validateField(name, value); err != nil && invalid == nil {
			invalid = err
		}
		return value
	})
	if unknown != "" {
		return "", fmt.Errorf("layout: template %q uses the unknown placeholder %q", tmpl, unknown)
	}
	if missing != "" {
		return "", fmt.Errorf("layout: template %q needs %q, which this remote has none of", tmpl, missing)
	}
	if invalid != nil {
		return "", invalid
	}
	if r.Project != "" && !used["project"] {
		return "", fmt.Errorf("layout: template %q has no %q placeholder, which this remote carries", tmpl, "project")
	}

	return strings.Trim(result, "/"), nil
}

// validateField rejects value when it cannot name one or more directories
// inside the root. owner alone may carry several components, split on
// "/", for a GitLab subgroup; every other field must be the single
// component it names on its own, so a "/" anywhere inside it is rejected
// the same as it would be inside one of owner's own components.
//
// A "\" is rejected everywhere too, on every host operating system, not
// only on Windows: this package and internal/remote always build and
// compare a path as a literal "/" separated string, never through
// path/filepath, so the same rendered value is read back on whichever
// platform a later caller runs this binary on. A component that carries
// a "\" would only turn into a second, separate directory once that
// string reaches a Windows path API, not here and not on a POSIX host,
// so rejecting it has to happen before that difference can matter, for
// every build of this binary alike.
func validateField(field, value string) error {
	segments := []string{value}
	if field == "owner" {
		segments = strings.Split(value, "/")
	}
	for _, seg := range segments {
		if err := validateSegment(field, seg); err != nil {
			return err
		}
	}
	return nil
}

// validateSegment rejects seg, one component of field, when it cannot name
// exactly one directory inside the root the way an ordinary component
// does, or when it would read back, on some platform this binary also
// targets, as the same directory a differently spelled seg already names.
//
// Rejected: an empty component; one with no visible content once leading
// and trailing whitespace is removed, since that renders the same kind of
// collapsed, ambiguous segment an empty component would; one naming the
// current or a parent directory; one that is itself absolute; one
// carrying a "/" or a "\", on every host operating system, not only on
// Windows, since this package and internal/remote always build and
// compare a path as a literal "/" separated string, never through
// path/filepath, so the same rendered value is read back on whichever
// platform a later caller runs this binary on; one carrying a ":", a
// character reserved inside a single Windows path component, for the
// same cross-platform reason; one ending in a dot or a space, which
// Windows strips from a final path component, so a component ending in
// "widget" and one ending in "widget." would resolve to the same
// directory there (stated from documented Windows path resolution rules;
// there is no Windows host available here to confirm it live); and one
// carrying a Unicode control or formatting character, rejected because
// the rendered path reaches further than this one component's own
// display — clone, path and cd print it on stdout for a shell function to
// consume, and doctor and adopt log and print it too, so a character that
// changes how the surrounding text reads affects every one of those
// readers, not only how this component's own name looks in isolation.
//
// validateSegment enforces no length ceiling of its own. The destination
// filesystem already enforces one, surfaced as a plain error from the
// syscall gitcd already makes when it creates or opens the rendered
// destination; a second ceiling here would have to track every target
// filesystem's own limit instead of relying on the one that actually
// applies at write time.
func validateSegment(field, seg string) error {
	switch {
	case seg == "":
		return fmt.Errorf("layout: %s has an empty path component", field)
	case strings.TrimSpace(seg) == "":
		return fmt.Errorf("layout: %s has a path component with no visible content: %q", field, seg)
	case seg == ".":
		return fmt.Errorf("layout: %s has a path component naming the current directory: %q", field, seg)
	case seg == "..":
		return fmt.Errorf("layout: %s has a path component naming a parent directory: %q", field, seg)
	case strings.HasPrefix(seg, "/") || strings.HasPrefix(seg, "\\"):
		return fmt.Errorf("layout: %s has an absolute path component: %q", field, seg)
	case strings.ContainsAny(seg, "/\\"):
		return fmt.Errorf("layout: %s has a path component carrying a path separator: %q", field, seg)
	case strings.Contains(seg, ":"):
		return fmt.Errorf("layout: %s has a path component carrying a reserved character: %q", field, seg)
	case seg != strings.TrimRight(seg, ". "):
		return fmt.Errorf("layout: %s has a path component ending in a dot or a space: %q", field, seg)
	case containsFormattingOrControl(seg):
		return fmt.Errorf("layout: %s has a path component carrying a formatting or control character: %q", field, seg)
	default:
		return nil
	}
}

// containsFormattingOrControl reports whether seg carries a Unicode
// control character (category Cc, such as a tab or a literal newline) or
// formatting character (category Cf, such as a bidirectional override):
// neither changes which directory the component names, but either
// changes how the surrounding text reads once the rendered path reaches
// a terminal, a log line or a shell function consuming it.
func containsFormattingOrControl(seg string) bool {
	for _, r := range seg {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return true
		}
	}
	return false
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
//
// No caller exists yet: the doctor and adopt milestone (issue #4) consumes
// it, to detect a repository sitting at a path the layout would not render.
//
// ParsePath does not run the same component check Render does: path is
// meant to come from a walk of an existing directory tree under the
// configured root, where a real directory entry named "." or ".." cannot
// occur, and the Remote this returns never becomes a write target on its
// own, only a value compared back against Render's own output, which
// still rejects it there if the comparison ever is attempted on a path a
// walk could not have produced.
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
