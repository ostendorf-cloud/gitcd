// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package profile resolves a remote to the identity profile the
// configured rules expect for it, and inspects a repository on disk for
// the profile it actually carries. It never writes: a write goes through
// internal/git, from the command layer.
package profile

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/git"
	"github.com/ostendorf-cloud/gitcd/internal/remote"
)

// Resolution is the outcome of resolving a remote against a Config's
// rules: which profile applies, and, when a specific rule rather than only
// the default matched, that rule, so a caller can explain why.
type Resolution struct {
	// Profile is the resolved profile name, or "" when no rule matched
	// and no default profile is configured.
	Profile string
	// Rule is the specific rule that matched, or nil when only the
	// default profile applied, or nothing applied at all.
	Rule *config.Rule
}

// Resolve reports which profile r resolves to under cfg's rules: the most
// specific matching rule wins, specificity ranked host, then host and
// owner, then host, owner and repo, with cfg.DefaultProfile applied when
// no rule matches at all. Rule order in cfg.Rules does not decide this,
// SortRules does.
func Resolve(r remote.Remote, cfg config.Config) Resolution {
	res := Resolution{Profile: cfg.DefaultProfile}

	bestTier := -1
	for _, rule := range SortRules(cfg.Rules) {
		if !ruleMatches(rule, r) {
			continue
		}
		tier := ruleTier(rule)
		if tier < bestTier {
			continue
		}
		bestTier = tier
		matched := rule
		res.Profile = rule.Profile
		res.Rule = &matched
	}
	return res
}

// SortRules returns rules sorted in the order gitcd applies them in,
// ascending from least to most specific, so that, rendered as one includeIf
// block per rule in this order, a later, more specific block always wins
// over an earlier, less specific one, regardless of the order the
// configuration file lists them in. Rules tied on specificity are ordered
// by host, then owner, then repo, then profile name, so the order is
// deterministic rather than depending on map or file order.
func SortRules(rules []config.Rule) []config.Rule {
	sorted := make([]config.Rule, len(rules))
	copy(sorted, rules)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if ta, tb := ruleTier(a), ruleTier(b); ta != tb {
			return ta < tb
		}
		if a.Host != b.Host {
			return a.Host < b.Host
		}
		if a.Owner != b.Owner {
			return a.Owner < b.Owner
		}
		if a.Repo != b.Repo {
			return a.Repo < b.Repo
		}
		return a.Profile < b.Profile
	})
	return sorted
}

// ruleTier ranks a rule's specificity by how many of its fields narrow the
// match: host alone is 1, host and owner is 2, host, owner and repo is 3.
func ruleTier(r config.Rule) int {
	tier := 0
	if r.Host != "" {
		tier++
	}
	if r.Owner != "" {
		tier++
	}
	if r.Repo != "" {
		tier++
	}
	return tier
}

// ruleMatches reports whether r's non-empty fields all equal the matching
// field of remote: an empty field matches anything.
func ruleMatches(r config.Rule, rm remote.Remote) bool {
	if r.Host != "" && !strings.EqualFold(r.Host, rm.Host) {
		return false
	}
	if r.Owner != "" && r.Owner != rm.Owner {
		return false
	}
	if r.Repo != "" && r.Repo != rm.Repo {
		return false
	}
	return true
}

// Origin names where an effective git config value came from.
const (
	// OriginNone means the key carries no value at all.
	OriginNone = ""
	// OriginLocal means the repository's own local configuration, set
	// through "gitcd profile set" or by hand.
	OriginLocal = "local"
	// OriginManaged means one of the profile files "gitcd apply" renders,
	// read through the generated includeIf chain.
	OriginManaged = "managed"
	// OriginOther means a file outside both of those, most commonly the
	// user's own global configuration, set directly rather than through
	// an includeIf block.
	OriginOther = "other"
)

// Identity is one effective git config value together with where it came
// from.
type Identity struct {
	Value  string
	Origin string
}

// Repo is one repository's identity relevant state: where it lives on
// disk, the remote and profile the configured rules expect for it, and the
// identity git actually resolves for it right now. Later milestones build
// "list", "doctor" and "status" on this value.
type Repo struct {
	// Path is the repository's working directory, as given.
	Path string
	// GitDir is the absolute path of this checkout's own ".git"
	// directory: for a linked worktree, its private metadata directory,
	// distinct from CommonDir.
	GitDir string
	// CommonDir is the absolute path of the repository's common
	// directory, the one holding the local configuration every worktree
	// of this repository shares.
	CommonDir string
	// IsWorktree reports whether Path is a linked worktree rather than
	// the repository's main checkout.
	IsWorktree bool
	// RemoteURL is the literal "remote.origin.url" value, unresolved
	// through any "url.<base>.insteadOf" rewrite.
	RemoteURL string
	// Remote is RemoteURL, parsed.
	Remote remote.Remote
	// ExpectedProfile is the profile the configured rules resolve Remote
	// to, "" when none applies.
	ExpectedProfile string
	// MatchedRule is the specific rule that produced ExpectedProfile, nil
	// when only the default profile applied or nothing applied at all.
	MatchedRule *config.Rule
	// Email is the effective "user.email", with its origin.
	Email Identity
	// SigningKey is the effective "user.signingkey", with its origin.
	SigningKey Identity
}

// Inspect reads path's own remote and git metadata, resolves the profile
// its rules expect, and reads back the identity git actually resolves for
// it, classifying each value's origin against managedDir, the directory
// "gitcd apply" renders its profile files into.
func Inspect(ctx context.Context, path string, cfg config.Config, managedDir string) (Repo, error) {
	r := Repo{Path: path}

	gitDir, err := git.Dir(ctx, path)
	if err != nil {
		return Repo{}, fmt.Errorf("profile: read the git directory of %s: %w", path, err)
	}
	r.GitDir = gitDir

	commonDir, err := git.CommonDir(ctx, path)
	if err != nil {
		return Repo{}, fmt.Errorf("profile: read the common directory of %s: %w", path, err)
	}
	r.CommonDir = commonDir
	r.IsWorktree = gitDir != commonDir

	remoteURL, err := git.RemoteURL(ctx, path)
	if err != nil {
		return Repo{}, fmt.Errorf("profile: read the origin remote of %s: %w", path, err)
	}
	r.RemoteURL = remoteURL

	parsed, err := remote.Parse(remoteURL, cfg.AliasMap())
	if err != nil {
		return Repo{}, fmt.Errorf("profile: parse the origin remote of %s: %w", path, err)
	}
	r.Remote = parsed

	res := Resolve(parsed, cfg)
	r.ExpectedProfile = res.Profile
	r.MatchedRule = res.Rule

	email, err := readIdentity(ctx, path, "user.email", commonDir, managedDir)
	if err != nil {
		return Repo{}, err
	}
	r.Email = email

	signingKey, err := readIdentity(ctx, path, "user.signingkey", commonDir, managedDir)
	if err != nil {
		return Repo{}, err
	}
	r.SigningKey = signingKey

	return r, nil
}

func readIdentity(ctx context.Context, path, key, commonDir, managedDir string) (Identity, error) {
	value, ok, err := git.ShowOrigin(ctx, path, key)
	if err != nil {
		return Identity{}, fmt.Errorf("profile: read %s of %s: %w", key, path, err)
	}
	if !ok {
		return Identity{}, nil
	}
	return Identity{Value: value.Value, Origin: classifyOrigin(value.Origin, commonDir, managedDir)}, nil
}

// classifyOrigin reports which of OriginLocal, OriginManaged or
// OriginOther the file at originPath is, for a repository whose common
// directory is commonDir and whose managed profile directory is
// managedDir. Paths are resolved through any symlink before comparing,
// since a system temporary directory, among others, commonly is one, and
// git itself resolves symlinks when it reports an absolute path.
func classifyOrigin(originPath, commonDir, managedDir string) string {
	if originPath == "" {
		return OriginOther
	}
	origin := resolvePath(originPath)
	if origin == resolvePath(filepath.Join(commonDir, "config")) {
		return OriginLocal
	}
	if isWithin(origin, resolvePath(managedDir)) {
		return OriginManaged
	}
	return OriginOther
}

// resolvePath resolves every symlink in path, falling back to path itself
// when it cannot, empty or not yet created being the common case.
func resolvePath(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

func isWithin(path, dir string) bool {
	if dir == "" {
		return false
	}
	if path == dir {
		return true
	}
	return len(path) > len(dir) && path[:len(dir)] == dir && path[len(dir)] == '/'
}
