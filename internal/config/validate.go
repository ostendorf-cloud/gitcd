// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
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

// The restricted set below is expressed as three kinds of family, and
// never as a list of whole keys. A list cannot describe what has to be
// described here.
//
// Every rule ignores the subsection entirely, and that is what makes a
// family a family. A git config key that names a command is as often
// subsection keyed as not: "diff.<driver>.command", "filter.<f>.clean",
// "pager.<command>", "alias.<name>", "credential.<url>.helper". The
// subsection in each is a name the user invents, so no enumeration of
// whole keys reaches them, while a rule on the section and the final
// name reaches both spellings of the same setting at once.
//
// Section and final name are compared case insensitively, as git
// compares them itself: without that, the same key spelled
// "CORE.sshCommand" would be refused while git still read it back as
// "core.sshcommand". Since no rule reads the subsection, the one part of
// a key git does compare case sensitively never decides a match.

// unsafeNameSuffixes are the final name endings that denote a command
// git runs, or a path or destination it resolves on its own, in whatever
// section they appear.
//
// Matching a suffix rather than a whole name is deliberate. git's own
// naming is compositional: the thing that runs a command is spelled
// "sshCommand", "alternateRefsCommand", "defaultKeyCommand",
// "packObjectsHook", "hooksPath". An exact name list has to be extended
// every time git adds another one, and silently admits the ones nobody
// thought of, which is how a list of four "core" names came to admit
// "core.askPass" and "core.hooksPath" sitting beside them. A suffix set
// covers the spellings git has not invented yet.
var unsafeNameSuffixes = []string{
	"askpass",
	"clean",
	"cmd",
	"command",
	"driver",
	"editor",
	"external",
	"hook",
	"hookspath",
	"pager",
	"process",
	"program",
	"proxy",
	"smudge",
	"templatedir",
	"textconv",
	"variant",
}

// unsafeExactNames are final names that denote the same thing but are
// not composed of a recognisable ending.
var unsafeExactNames = []string{
	"allow",
	"destination",
	"directory",
	"fsmonitor",
	"insteadof",
	"pushinsteadof",
}

// unsafeSections are sections in which no key is an identity setting,
// so the whole section is restricted. "pager" and "alias" hold nothing
// but command definitions, keyed by a subsection the user invents;
// "credential" decides where a credential comes from; "url" holds only
// the two rewriting keys; "safe", "protocol" and "trace2" decide what
// git trusts, which transports it will use, and where it writes its own
// trace output.
var unsafeSections = []string{
	"alias",
	"credential",
	"difftool",
	"mergetool",
	"pager",
	"protocol",
	"safe",
	"trace2",
	"url",
}

// invertedSections are sections restricted as a whole except for the
// keys safeCoreNames lists. "core" is one: its keys run from
// "core.autocrlf", which any profile may set, to "core.sshCommand" and
// "core.hooksPath", which no profile may set unannounced, and the two
// groups are not told apart by their spelling. Listing what an ordinary
// profile legitimately sets, and restricting the rest, is the only way
// round that puts a key git adds later on the restricted side by
// default.
var invertedSections = []string{"core"}

// safeCoreNames are the "core" keys an ordinary profile may carry: the
// settings that describe how a working tree is stored and compared, none
// of which names a command, a path git resolves, or a credential.
var safeCoreNames = []string{
	"abbrev",
	"autocrlf",
	"bare",
	"bigfilethreshold",
	"commentchar",
	"compression",
	"eol",
	"excludesfile",
	"filemode",
	"ignorecase",
	"logallrefupdates",
	"loosecompression",
	"precomposeunicode",
	"quotepath",
	"safecrlf",
	"sharedrepository",
	"symlinks",
	"trustctime",
	"untrackedcache",
	"whitespace",
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

	switch {
	case contains(unsafeSections, section):
		return "sets a command or a trust decision rather than an identity", true
	case contains(invertedSections, section) && !contains(safeCoreNames, name):
		return "is a \"" + section + "\" key outside the settings a profile describes a working tree with", true
	case contains(unsafeExactNames, name):
		return "decides what git trusts or where it resolves something from", true
	case hasUnsafeSuffix(name):
		return "names a command git runs on its own", true
	}
	return "", false
}

// hasUnsafeSuffix reports whether name, already lowercased, ends in one
// of unsafeNameSuffixes.
func hasUnsafeSuffix(name string) bool {
	for _, suffix := range unsafeNameSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// contains reports whether values, already lowercased, holds value.
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
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
		// SplitKey tells an empty subsection apart from a key that is
		// not a "section.key" pair at all, so its own message is the
		// more useful of the two and is kept.
		return fmt.Errorf("%w: %w", ErrInvalidProfile, err)
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

// isControl reports whether r is a control character: the C0 range, DEL,
// and the C1 range. git parses DEL and C1 without complaint, so refusing
// them is not about git refusing the file; it is that a value gitcd
// writes on a person's behalf, which "doctor" later prints and compares,
// should not carry a character that renders as nothing and compares as
// something.
func isControl(r rune) bool {
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}

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
		if isControl(r) && !escapableValueRunes[r] {
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

// ValidateHostAlias reports whether alias can be rendered into the
// include file's conditions. An alias is interpolated into the
// "hasconfig:remote.*.url:" glob of every rule naming the host it
// resolves to, in exactly the position the rule's own host occupies, so
// it is held to the same class as a rule host: a glob metacharacter here
// widens a rule to every host, which is the widening a literal glob in
// an owner is refused to prevent.
func ValidateHostAlias(host, alias string) error {
	if alias == "" {
		return fmt.Errorf("%w: host %q has an empty alias", ErrInvalidProfile, host)
	}
	if !ruleHost.MatchString(alias) {
		return fmt.Errorf("%w: alias %q of host %q is not a host name", ErrInvalidProfile, alias, host)
	}
	return nil
}

// layoutPlaceholders are the placeholder names a layout template may
// carry, the same set internal/layout renders and the same set it
// already refuses a template outside of.
var layoutPlaceholders = []string{"host", "owner", "org", "project", "repo"}

// layoutSegment matches one "/" separated segment of a layout template:
// a run of placeholders and literal characters, where a placeholder is
// "{name}" and a literal character is a letter, a digit, ".", "-" or
// "_".
var layoutSegment = regexp.MustCompile(`^(\{[A-Za-z]+\}|[A-Za-z0-9._-])+$`)

// layoutPlaceholder matches one "{name}" placeholder inside a layout
// template segment, so each can be checked against layoutPlaceholders.
var layoutPlaceholder = regexp.MustCompile(`\{([A-Za-z]+)\}`)

// ValidateLayout reports whether a layout template can be rendered into
// the include file's "gitdir/i:" condition. An empty template is valid
// and means the generic default applies, the way a host entry clears its
// own layout.
//
// The template's class is not a rule field's. A layout is a path
// template, so it has to carry "/" between its segments and "{host}"
// style placeholders inside them, both of which a rule field refuses.
// What it must not carry is the rest: a quote or a bracket would close
// the condition the template is rendered into, a glob metacharacter
// would widen the condition past the rule that named it, and an upward
// segment would root it outside the configured root, where the rule's
// profile would then apply to repositories gitcd never placed.
//
// Validation is per segment, which is what makes the upward case fall
// out rather than needing its own scan: ".." is a segment, and so is
// any placeholder, so each is judged as the whole thing it is.
func ValidateLayout(where, template string) error {
	if template == "" {
		return nil
	}
	for _, segment := range strings.Split(template, "/") {
		switch {
		case segment == "":
			return fmt.Errorf("%w: %s layout %q holds an empty path segment", ErrInvalidProfile, where, template)
		case segment == "." || segment == "..":
			return fmt.Errorf("%w: %s layout %q holds a relative path segment", ErrInvalidProfile, where, template)
		case !layoutSegment.MatchString(segment):
			return fmt.Errorf(
				"%w: %s layout %q may hold only placeholders, letters, digits, \".\", \"-\", \"_\" and \"/\"",
				ErrInvalidProfile, where, template,
			)
		}
		for _, name := range layoutPlaceholder.FindAllStringSubmatch(segment, -1) {
			if !contains(layoutPlaceholders, name[1]) {
				return fmt.Errorf(
					"%w: %s layout %q names the unknown placeholder %q", ErrInvalidProfile, where, template, name[0],
				)
			}
		}
	}
	return nil
}

// Validate reports whether every part of c that reaches a generated
// file can be written into one as written: each profile's name, keys and
// values, each rule's fields, each host's aliases and layout, and the
// references rules and defaultProfile make to a profile by name.
//
// This is the whole gate in one place, so a Config that came from Load
// and one a caller built by hand are held to the same terms. Load calls
// it on the configuration it has just parsed, and
// internal/gitconfig.Render calls it on whatever it is handed.
func (c Config) Validate() error {
	if err := c.validateProfiles(); err != nil {
		return err
	}
	for host, h := range c.Hosts {
		for _, alias := range h.Aliases {
			if err := ValidateHostAlias(host, alias); err != nil {
				return err
			}
		}
		if err := ValidateLayout("host "+host+"'s", h.Layout); err != nil {
			return err
		}
	}
	if err := ValidateLayout("the default", c.Default); err != nil {
		return err
	}
	for _, rule := range c.Rules {
		if err := ValidateRule(rule); err != nil {
			return err
		}
		if rule.Repo != "" && rule.Owner == "" {
			return fmt.Errorf(
				"%w: rule for host %q names a repo without an owner", ErrInvalidProfile, rule.Host,
			)
		}
		if _, ok := c.Profiles[rule.Profile]; !ok {
			return fmt.Errorf(
				"%w: rule for host %q names the undefined profile %q", ErrInvalidProfile, rule.Host, rule.Profile,
			)
		}
	}
	if c.DefaultProfile != "" {
		if _, ok := c.Profiles[c.DefaultProfile]; !ok {
			return fmt.Errorf(
				"%w: defaultProfile names the undefined profile %q", ErrInvalidProfile, c.DefaultProfile,
			)
		}
	}
	return nil
}

// validateProfiles validates every profile name, key and value in c,
// and refuses two names that differ only in case.
//
// The case check is the same reasoning the trailing dot and the trailing
// space already get. A profile name becomes a file name, and on a case
// insensitive filesystem, which is macOS's default, "work" and "Work"
// are one file: whichever profile is rendered second overwrites the
// first, both rules point their "path" at the surviving file, and the
// rule naming one profile then applies the other profile's identity with
// nothing printed to say so.
func (c Config) validateProfiles() error {
	names := make([]string, 0, len(c.Profiles))
	for name := range c.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)

	allowed, err := allowedUnsafeKeys(c.AllowUnsafeKeys)
	if err != nil {
		return err
	}

	seen := make(map[string]string, len(names))
	for _, name := range names {
		if err := ValidateProfileName(name); err != nil {
			return err
		}
		folded := strings.ToLower(name)
		if first, ok := seen[folded]; ok {
			return fmt.Errorf(
				"%w: the profiles %q and %q differ only in case, and would render to one file",
				ErrInvalidProfile, first, name,
			)
		}
		seen[folded] = name

		for _, key := range c.Profiles[name].Keys() {
			if err := validateProfileEntry(key, c.Profiles[name][key], allowed); err != nil {
				return fmt.Errorf("profile %q: %w", name, err)
			}
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
