// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"fmt"
	"strings"
)

// ErrUnsafeKey is wrapped into the error Load returns when a profile
// carries a git config key that hands control of a subprocess or of
// credential handling to the configuration file, and "allowUnsafeKeys"
// does not list that key. Such a key is refused rather than applied: a
// profile is picked automatically, per remote, by a rule the person who
// clones a repository does not re-read every time, so a key that makes
// git run a command, answer a credential prompt or rewrite a remote URL
// only applies when it is named once, deliberately, somewhere else in
// the file.
var ErrUnsafeKey = errors.New("config: profile key needs an explicit opt in")

// AllowUnsafeKeysKey is the top level configuration key that opts one
// unsafe key back in, named in every refusal so the message carries its
// own remedy.
const AllowUnsafeKeysKey = "allowUnsafeKeys"

// unsafeKeyRule is one family of git config key a profile may not carry
// unless AllowUnsafeKeysKey names it.
//
// Matching is per family rather than per literal key, because the keys
// in question are not a flat list. "credential" is dangerous as a whole
// section, including the URL keyed "credential.<url>.helper" form, so no
// list of full keys could cover it. "process" and "hook" are dangerous
// wherever they appear, under "filter" today and under whatever section
// a future git version adds, so they match on the final name and ignore
// the section. The four "core" entries, by contrast, are genuinely four
// specific keys, and matching the whole "core" section instead would
// refuse "core.autocrlf" and the rest of an ordinary profile.
//
// Section and name are compared case insensitively, as git compares them
// itself: without that, the same key spelled "CORE.sshCommand" would be
// refused while git still read it back as "core.sshcommand". No rule
// looks at the subsection, so the one part of a key git does compare
// case sensitively never decides a match.
type unsafeKeyRule struct {
	// section is the section the rule applies to, lowercased, or "" for
	// a rule that applies under every section.
	section string
	// names are the final key names the rule applies to, lowercased, or
	// empty for a rule covering every key in its section.
	names []string
	// why says, functionally, what the key does, so the refusal explains
	// itself without the reader having to look the key up.
	why string
}

// unsafeKeyRules is the set of key families refused by default.
var unsafeKeyRules = []unsafeKeyRule{
	{
		section: "core",
		names:   []string{"sshcommand"},
		why:     "replaces the command git runs to reach a remote",
	},
	{
		section: "core",
		names:   []string{"pager", "editor", "fsmonitor"},
		why:     "names a command git runs on its own",
	},
	{
		section: "credential",
		why:     "takes over how git answers a credential prompt",
	},
	{
		section: "url",
		names:   []string{"insteadof", "pushinsteadof"},
		why:     "rewrites the remote URL git actually contacts",
	},
	{
		names: []string{"process", "hook"},
		why:   "names a command git runs on its own",
	},
}

// UnsafeKeyReason reports whether key belongs to a family a profile may
// carry only when AllowUnsafeKeysKey names it, and returns the plain
// reason the family is listed. key need not be valid: an unparseable key
// is simply not unsafe, since ValidateProfileKey refuses it first.
func UnsafeKeyReason(key string) (string, bool) {
	section, _, name, err := SplitKey(key)
	if err != nil {
		return "", false
	}
	section, name = strings.ToLower(section), strings.ToLower(name)
	for _, rule := range unsafeKeyRules {
		if rule.section != "" && rule.section != section {
			continue
		}
		if len(rule.names) > 0 && !containsFold(rule.names, name) {
			continue
		}
		return rule.why, true
	}
	return "", false
}

// containsFold reports whether names, already lowercased, holds name.
func containsFold(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// normalizeKey renders key in the form the allow list is compared on:
// section and final name lowercased, as git compares them, and the
// subsection left exactly as written, as git compares that.
func normalizeKey(key string) (string, error) {
	section, subsection, name, err := SplitKey(key)
	if err != nil {
		return "", err
	}
	return strings.ToLower(section) + "." + subsection + "." + strings.ToLower(name), nil
}

// SplitKey splits a git config key's "section.key" or
// "section.subsection.key" form into its three parts, the subsection
// empty for the two part form. It reports an error for a key that is
// neither form, and performs no further validation: ValidateProfileKey
// decides whether each part is acceptable.
func SplitKey(key string) (section, subsection, name string, err error) {
	first := strings.Index(key, ".")
	last := strings.LastIndex(key, ".")
	if first <= 0 || last == len(key)-1 {
		return "", "", "", fmt.Errorf("config: %q is not a \"section.key\" git config key", key)
	}
	if first == last {
		return key[:first], "", key[first+1:], nil
	}
	if first+1 == last {
		// Two adjacent dots: the three part form with nothing where the
		// subsection belongs. Reading it as the two part form instead
		// would quietly render it as a key it does not spell.
		return "", "", "", fmt.Errorf("config: %q names an empty subsection", key)
	}
	return key[:first], key[first+1 : last], key[last+1:], nil
}

// ValidateProfileKey reports whether key is a git config key a profile
// may carry: a valid "section.key" or "section.subsection.key" form in
// which every part, the subsection included, holds only the characters
// a git config section header and key name accept, and not a key under
// the "remote" section.
//
// The subsection matters as much as the other two parts, and for a
// reason the two part form does not have. A rendered profile file groups
// its keys under a "[section \"subsection\"]" header, so the subsection
// is written into the header itself rather than into a value. A
// subsection carrying a quote, a bracket or a line break would therefore
// close that header early and let the keys after it land in a section
// the profile never named, which is both wrong and silent: the file
// still parses, and git reads back a key nobody wrote.
//
// It is deliberately stricter than git's own subsection rules, which
// accept nearly any character between the quotes. A URL keyed subsection
// such as "http.https://example.test.sslVerify" is refused as a result.
// Every key gitcd's profiles exist to set names its subsection in this
// narrow character class or has no subsection at all, and a profile is
// applied automatically rather than read, so the narrow class is the
// right default and a refusal is visible at load time.
func ValidateProfileKey(key string) error {
	section, subsection, name, err := SplitKey(key)
	if err != nil {
		return fmt.Errorf("%w: %q is not a \"section.key\" git config key", ErrInvalidProfile, key)
	}
	if !configKeySection.MatchString(section) {
		return fmt.Errorf("%w: %q has an invalid section name", ErrInvalidProfile, key)
	}
	if subsection != "" && !configKeyName.MatchString(subsection) {
		return fmt.Errorf(
			"%w: %q has an invalid subsection: only letters, digits and %q are accepted between the section and the key name",
			ErrInvalidProfile, key, "-",
		)
	}
	if !configKeyName.MatchString(name) {
		return fmt.Errorf("%w: %q has an invalid key name", ErrInvalidProfile, key)
	}
	if strings.EqualFold(section, "remote") {
		return fmt.Errorf("%w: %q sets a remote.* key, which a profile may not carry", ErrInvalidProfile, key)
	}
	return nil
}

// windowsDeviceNames are the file name stems Windows reserves for its
// own devices. A file cannot be created under one of these on Windows,
// with or without an extension, so a profile named after one would
// render on Linux and macOS and then fail to apply at all on Windows.
var windowsDeviceNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com0": true, "com1": true, "com2": true, "com3": true, "com4": true,
	"com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
	"lpt0": true, "lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true,
	"lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// ValidateProfileName reports whether name can be used as a profile
// name. A profile name is not only a label: it becomes the name of the
// file "apply" renders that profile to, under the directory gitcd owns,
// and it is written into the include file as part of a path. So the
// name has to be one path segment, and one that every supported
// platform can actually create.
//
// The accepted class is deliberately not the one a key's section and
// final name use. It is wider in one direction and narrower in another,
// and both differences come from the name being a file name.
//
// Wider: "_" and "." are accepted inside the name, because a profile
// name is also what a person types, at "--profile work_eu" or in a
// rule, and reads naturally as "work.eu". A key's section has no reason
// to carry either.
//
// Narrower: the name must begin and end with a letter or a digit, and
// may not be one of the names Windows reserves for a device. A leading
// "." would make the rendered file hidden on Linux and macOS, a
// trailing "." or space is silently stripped by Windows, so two
// profiles differing only there would render to one file, and a
// reserved device name cannot be created as a file on Windows at all.
//
// The class also settles, by construction, the three cases that matter
// most: an empty name, a name carrying a path separator, and a name
// that is the current or a parent directory are all outside it. Each is
// still checked by name first, so the refusal says which one it was
// rather than only naming the class.
func ValidateProfileName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("%w: a profile name may not be empty", ErrInvalidProfile)
	case strings.ContainsAny(name, `/\`):
		return fmt.Errorf(
			"%w: profile name %q holds a path separator, and a profile name is one file name", ErrInvalidProfile, name,
		)
	case name == "." || name == "..":
		return fmt.Errorf(
			"%w: profile name %q names a directory rather than a profile", ErrInvalidProfile, name,
		)
	case !profileName.MatchString(name):
		return fmt.Errorf(
			"%w: profile name %q may hold only letters, digits, \".\", \"-\" and \"_\", and has to begin and end with a letter or a digit",
			ErrInvalidProfile, name,
		)
	}
	stem, _, _ := strings.Cut(name, ".")
	if windowsDeviceNames[strings.ToLower(stem)] {
		return fmt.Errorf(
			"%w: profile name %q is a reserved device name, which cannot be a file name on every supported platform",
			ErrInvalidProfile, name,
		)
	}
	return nil
}

// allowedUnsafeKeys builds the set of normalized keys that
// AllowUnsafeKeysKey opts back in, out of the entries as the file wrote
// them.
//
// An entry has to pass the same key validation a profile key does, and
// has to name a key that is actually refused by default. Both refusals
// keep the list honest: an entry that matches nothing would otherwise
// sit in the file looking like it permits something, and the person who
// wrote it would find out only when the key it was meant to name is
// still refused. A wildcard entry is refused by the first check, since
// no glob metacharacter is in the accepted character class.
func allowedUnsafeKeys(entries []string) (map[string]bool, error) {
	allowed := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if err := ValidateProfileKey(entry); err != nil {
			return nil, fmt.Errorf("%s: %w", AllowUnsafeKeysKey, err)
		}
		if _, unsafe := UnsafeKeyReason(entry); !unsafe {
			return nil, fmt.Errorf(
				"%w: %s names %q, which a profile may carry without being opted in", ErrInvalidProfile, AllowUnsafeKeysKey, entry,
			)
		}
		normalized, err := normalizeKey(entry)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", AllowUnsafeKeysKey, err)
		}
		allowed[normalized] = true
	}
	return allowed, nil
}

// validateProfileEntry applies every gate one profile entry passes: the
// key's syntax, the value's characters, and the opt in a key refused by
// default needs. allowed holds the normalized keys
// AllowUnsafeKeysKey listed.
func validateProfileEntry(key, value string, allowed map[string]bool) error {
	if err := ValidateProfileKey(key); err != nil {
		return err
	}
	if err := ValidateProfileValue(value); err != nil {
		return fmt.Errorf("key %q: %w", key, err)
	}
	why, unsafe := UnsafeKeyReason(key)
	if !unsafe {
		return nil
	}
	normalized, err := normalizeKey(key)
	if err != nil {
		return err
	}
	if allowed[normalized] {
		return nil
	}
	return fmt.Errorf(
		"%w: %q %s; list it under %q to apply it anyway", ErrUnsafeKey, key, why, AllowUnsafeKeysKey,
	)
}

// escapableValueRunes are the control characters git's own config parser
// reads back from an escape sequence inside a double quoted value, and
// which internal/gitconfig therefore writes escaped rather than
// literally. Verified against git 2.54.0: "\n", "\t" and "\b" in a
// quoted value read back as the control character, while a literal line
// break inside the quotes, and a "\r" escape, both make git refuse the
// whole file.
var escapableValueRunes = map[rune]bool{'\n': true, '\t': true, '\b': true}

// ValidateProfileValue reports whether value can be written into a git
// config file as a double quoted value. The characters git has an escape
// sequence for are accepted and escaped at render time; every other
// control character is refused, because there is no spelling of it git
// reads back.
//
// Refusing is not cosmetic. A rendered profile file is reached through
// an include in the global git configuration, so a value git cannot
// parse does not fail one command: it makes git refuse its configuration
// in every repository the include matches, until the file is repaired by
// hand.
func ValidateProfileValue(value string) error {
	for _, r := range value {
		if r < 0x20 && !escapableValueRunes[r] {
			return fmt.Errorf(
				"%w: a value may not hold the control character %q, which git config has no escape for",
				ErrInvalidProfile, r,
			)
		}
	}
	return nil
}

// ValidateRule reports whether r's host, owner and repo can be rendered
// into the include file's conditions.
//
// The three are interpolated into an "includeIf" condition, a
// "gitdir/i:" path glob and a "hasconfig:remote.*.url:" URL glob, so
// each one is validated against a character class that holds no glob
// metacharacter, no quote and no path traversal segment at all. That is
// a deliberately narrower gate than escaping each field would be, and it
// is the stronger of the two: escaping keeps a literal "*" working as a
// literal, where this refuses it outright.
//
// Refusing a glob costs nothing, because a rule already has a way to
// widen a match: an empty field matches every value of that field, which
// is what "*" would have been written for. What a glob does add is a
// silent failure mode. A rule is what decides which identity and which
// signing key a repository commits under, and an owner holding "*" or
// "**" quietly matches repositories the rule was never meant to cover,
// applying the wrong identity to them with nothing printed. A rule that
// names an owner is expected to name one owner.
func ValidateRule(r Rule) error {
	if r.Host == "" {
		return fmt.Errorf("%w: a rule needs a host", ErrInvalidProfile)
	}
	if !ruleHost.MatchString(r.Host) {
		return fmt.Errorf("%w: rule host %q is not a host name", ErrInvalidProfile, r.Host)
	}
	if r.Owner != "" {
		if err := validateRuleSegments("owner", r.Owner); err != nil {
			return err
		}
	}
	if r.Repo != "" {
		if !ruleSegment.MatchString(r.Repo) {
			return fmt.Errorf(
				"%w: rule repo %q may hold only letters, digits, \".\", \"-\" and \"_\"", ErrInvalidProfile, r.Repo,
			)
		}
		if r.Repo == "." || r.Repo == ".." {
			return fmt.Errorf("%w: rule repo %q is not a repository name", ErrInvalidProfile, r.Repo)
		}
	}
	return nil
}

// validateRuleSegments validates a rule's owner, which may carry "/"
// separated segments: a GitLab style group and subgroup is one owner as
// far as a rule is concerned. Every segment is validated on its own, and
// "." or ".." is refused, since either would climb out of the configured
// root once the field is rendered into the "gitdir/i:" path glob.
func validateRuleSegments(field, value string) error {
	for _, segment := range strings.Split(value, "/") {
		switch {
		case segment == "":
			return fmt.Errorf("%w: rule %s %q holds an empty segment", ErrInvalidProfile, field, value)
		case segment == "." || segment == "..":
			return fmt.Errorf("%w: rule %s %q holds a relative path segment", ErrInvalidProfile, field, value)
		case !ruleSegment.MatchString(segment):
			return fmt.Errorf(
				"%w: rule %s %q may hold only letters, digits, \".\", \"-\", \"_\" and \"/\"", ErrInvalidProfile, field, value,
			)
		}
	}
	return nil
}
