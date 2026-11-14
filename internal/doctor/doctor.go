// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package doctor finds drift between what gitcd's configuration implies
// and what actually sits on disk and in git's own configuration. It is
// the fleet wide form of internal/profile's single repository Inspect:
// the command layer walks the configured root and calls CheckRepo once
// per repository found, then CheckManagedFiles and CheckGlobal once for
// the fleet as a whole, and reports every Finding returned. This package
// never writes anything: "gitcd doctor --fix" and "gitcd adopt", in
// cmd/gitcd, perform every repair it only detects the need for.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/git"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
	"github.com/ostendorf-cloud/gitcd/internal/layout"
	"github.com/ostendorf-cloud/gitcd/internal/profile"
)

// Kind names one class of finding, each tracing to one check issue #4
// asks for.
type Kind string

const (
	// KindIdentityDrift is a repository whose effective identity differs
	// from the profile its own rules imply, with no "gitcd.profile" pin
	// recorded that would explain the difference as deliberate.
	KindIdentityDrift Kind = "identity-drift"
	// KindStalePin is a "gitcd.profile" pin naming a profile the current
	// configuration no longer carries: a deliberate pin at the time it
	// was written, now stale rather than deliberate.
	KindStalePin Kind = "stale-pin"
	// KindPathMismatch is a repository under root sitting at a path the
	// configured layout would not render for its own remote, for a
	// reason other than character case.
	KindPathMismatch Kind = "path-mismatch"
	// KindPathCase is KindPathMismatch narrowed to a difference of
	// character case only: the layout renders the same path, spelled
	// differently, which "doctor --fix" repairs with a rename.
	KindPathCase Kind = "path-case-mismatch"
	// KindRuleCaseMismatch is a configured rule whose host, owner and
	// repo would all match a repository case-insensitively, but do not
	// match exactly: profile.Resolve, and the "hasconfig" glob
	// internal/gitconfig renders from the same literal fields, are both
	// case sensitive on owner and repo, so the rule silently never
	// applies as written.
	KindRuleCaseMismatch Kind = "rule-case-mismatch"
	// KindManagedFileMissing is a file "gitcd apply" renders that does
	// not exist on disk at all.
	KindManagedFileMissing Kind = "managed-file-missing"
	// KindManagedFileStale is a file "gitcd apply" renders that exists
	// but no longer matches what the current configuration renders,
	// covering both an out of date include block and any ordering drift
	// inside it, since re-rendering always reproduces the current,
	// correctly ordered content.
	KindManagedFileStale Kind = "managed-file-stale"
	// KindGlobalIdentityKey is an identity key the global git
	// configuration sets directly: "user.useConfigOnly" cannot suppress
	// it, so it applies to every repository gitcd manages, one some rule
	// matches or not.
	KindGlobalIdentityKey Kind = "global-identity-key"
	// KindGlobalIdentityOrder is a global identity key sitting after the
	// gitcd include: "git config --global --add" inserts a new key into
	// that section, so a key already after it keeps beating every
	// includeIf value silently.
	KindGlobalIdentityOrder Kind = "global-identity-order"
	// KindOptedInKey is a profile key from a family profiles do not carry
	// by default, which the configuration's "allowUnsafeKeys" list opts
	// back in. It is not a defect: the opt in is a deliberate choice, and
	// doctor reports it rather than repairing it.
	//
	// It is reported because the choice outlives the moment it was made.
	// A key that makes git run a command of its own, answer a credential
	// prompt or rewrite a remote URL applies to every repository its
	// profile's rules match, from then on, through a file nobody opens
	// again. doctor is the command that says what gitcd's configuration
	// actually does to a repository, so a key of that weight belongs in
	// its report, named, next to the profile carrying it.
	KindOptedInKey Kind = "opted-in-key"
	// KindAncestorCaseConflict is an ancestor directory, shared by two or
	// more repositories under root, whose repositories disagree on its
	// spelling: each repository's own remote implies a different case
	// for the same, physically shared directory. Renaming it to fix one
	// repository would immediately un-place the other, and the reverse
	// rename on a later run would un-place the first again, so this is
	// never Fixable: "doctor --fix" reports it and renames nothing under
	// that ancestor, rather than flipping the two forever.
	KindAncestorCaseConflict Kind = "ancestor-case-conflict"
)

// Finding is one problem doctor reports.
type Finding struct {
	Kind Kind `json:"kind"`
	// Repo is the repository name, "/" separated relative to root, this
	// finding is about, or "" for a finding not tied to one repository.
	Repo string `json:"repo,omitempty"`
	// Path is the absolute path this finding is about, a repository's
	// own path or a managed file's path, or "" when neither applies.
	Path string `json:"path,omitempty"`
	// Message describes the finding in a form ready to print.
	Message string `json:"message"`
	// Fixable reports whether "doctor --fix" repairs this finding
	// mechanically. An identity key, local or global, is never one of
	// these: a value doctor did not write is never one it overwrites.
	Fixable bool `json:"fixable"`
	// WantPath is set alongside KindPathCase: the path "doctor --fix"
	// renames Path to.
	WantPath string `json:"wantPath,omitempty"`
}

// CheckRepo inspects the repository at path, named name relative to
// root, for every per repository finding: identity drift or a stale
// pin, a path the layout would not render, and a rule that almost
// matches this repository but for a case difference.
func CheckRepo(ctx context.Context, cfg config.Config, home, name, path string) ([]Finding, error) {
	repo, err := profile.Inspect(ctx, path, cfg, gitconfig.Dir(home))
	if err != nil {
		return nil, fmt.Errorf("doctor: inspect %s: %w", path, err)
	}

	var findings []Finding

	identityFindings, err := checkIdentity(ctx, path, name, cfg, repo)
	if err != nil {
		return nil, err
	}
	findings = append(findings, identityFindings...)

	findings = append(findings, checkPath(cfg, name, path, repo)...)
	findings = append(findings, checkRuleCase(cfg, name, repo)...)

	return findings, nil
}

// checkIdentity reports KindStalePin when path carries a "gitcd.profile"
// pin naming a profile cfg no longer has, and KindIdentityDrift when,
// with no pin recorded at all, the identity git actually resolves for
// path differs from what the profile its rules imply would set. A valid
// pin, naming a profile cfg still carries, is a deliberate choice,
// reported as neither: AC6 of issue #4 asks doctor to tell the two
// apart, not to flag a deliberate pin as if it were drift.
func checkIdentity(ctx context.Context, path, name string, cfg config.Config, repo profile.Repo) ([]Finding, error) {
	pinned, err := git.ConfigGetLocal(ctx, path, "gitcd.profile")
	switch {
	case err == nil:
		if _, ok := cfg.Profiles[pinned]; !ok {
			return []Finding{{
				Kind: KindStalePin,
				Repo: name,
				Path: path,
				Message: fmt.Sprintf(
					"pinned to profile %q through gitcd.profile, which the configuration no longer carries", pinned,
				),
			}}, nil
		}
		return nil, nil
	case errors.Is(err, git.ErrConfigKeyNotSet):
		// No pin: fall through to compare the effective identity
		// against what the rules currently imply.
	default:
		return nil, fmt.Errorf("doctor: read gitcd.profile of %s: %w", path, err)
	}

	if repo.ExpectedProfile == "" {
		return nil, nil
	}
	want := cfg.Profiles[repo.ExpectedProfile]
	var mismatches []string
	if v, ok := want["user.email"]; ok && v != repo.Email.Value {
		mismatches = append(mismatches, fmt.Sprintf("user.email: have %q, profile %q expects %q", repo.Email.Value, repo.ExpectedProfile, v))
	}
	if v, ok := want["user.signingkey"]; ok && v != repo.SigningKey.Value {
		mismatches = append(
			mismatches, fmt.Sprintf("user.signingkey: have %q, profile %q expects %q", repo.SigningKey.Value, repo.ExpectedProfile, v),
		)
	}
	if len(mismatches) == 0 {
		return nil, nil
	}
	return []Finding{{
		Kind: KindIdentityDrift,
		Repo: name,
		Path: path,
		Message: fmt.Sprintf(
			"effective identity differs from the profile its rules imply, %q: %s", repo.ExpectedProfile, strings.Join(mismatches, "; "),
		),
	}}, nil
}

// checkPath reports a repository sitting at a path the configured layout
// would not render for its own remote: KindPathCase when the layout
// would render the same path spelled differently, KindPathMismatch for
// every other mismatch, including a remote the layout cannot render a
// path for at all.
func checkPath(cfg config.Config, name, path string, repo profile.Repo) []Finding {
	rel, err := layout.Render(cfg.LayoutFor(repo.Remote.Host), repo.Remote)
	if err != nil {
		return []Finding{{
			Kind: KindPathMismatch,
			Repo: name,
			Path: path,
			Message: fmt.Sprintf(
				"the configured layout cannot render a path for this repository's own remote: %v", err,
			),
		}}
	}
	want := filepath.Join(cfg.Root, rel)
	if want == path {
		return nil
	}
	if strings.EqualFold(want, path) {
		return []Finding{{
			Kind:     KindPathCase,
			Repo:     name,
			Path:     path,
			Fixable:  true,
			WantPath: want,
			Message: fmt.Sprintf(
				"on disk at %s, the layout renders %s for this repository's remote: a case only difference", path, want,
			),
		}}
	}
	return []Finding{{
		Kind: KindPathMismatch,
		Repo: name,
		Path: path,
		Message: fmt.Sprintf(
			"on disk at %s, the layout renders %s for this repository's own remote", path, want,
		),
	}}
}

// RepoPath pairs one repository's own on-disk path with the path the
// configured layout renders for its remote, so CheckAncestorCaseConflicts
// can compare every repository under root against its siblings, including
// one that is not itself mismatched: a repository already sitting at the
// path its own remote implies still needs to be told apart from the one
// renaming a shared ancestor would place it at by accident.
type RepoPath struct {
	// Repo is the repository name, "/" separated relative to root.
	Repo string
	// Path is the repository's actual, on-disk path.
	Path string
	// Want is the path the configured layout renders for this
	// repository's own remote, "" when it could not be rendered at all.
	Want string
}

// FirstDifferingSegment walks oldPath and newPath component by component
// and returns the path of the first (topmost) component at which they
// differ, in each of the two inputs: the ancestor directory a caller
// renaming oldPath to newPath actually needs to rename, carrying
// everything below it along in one move, rather than only the leaf. Both
// return values equal oldPath and newPath themselves when the two share
// no common prefix at all.
func FirstDifferingSegment(oldPath, newPath string) (string, string) {
	oldSegs := strings.Split(filepath.Clean(oldPath), string(filepath.Separator))
	newSegs := strings.Split(filepath.Clean(newPath), string(filepath.Separator))

	i := 0
	for i < len(oldSegs) && i < len(newSegs) && oldSegs[i] == newSegs[i] {
		i++
	}
	if i >= len(oldSegs) || i >= len(newSegs) {
		return oldPath, newPath
	}
	return strings.Join(oldSegs[:i+1], string(filepath.Separator)), strings.Join(newSegs[:i+1], string(filepath.Separator))
}

// CheckAncestorCaseConflicts reports KindAncestorCaseConflict for every
// ancestor directory that two or more of repos would rename to a
// different spelling: for each repository whose own path is a case-only
// mismatch of its own Want, it finds every sibling repository physically
// under the same ancestor directory and asks what that sibling's own Want
// implies for the very same ancestor. When siblings disagree, case
// insensitively equal but not identical, on what that one shared,
// physical directory should be called, renaming it to fix one would
// un-place the other, so neither is fixed and this is reported instead.
// A repository whose Want already equals Path is not itself mismatched,
// but is still consulted as a sibling: it is the repository a careless
// rename would newly un-place, exactly the bug this check exists to
// catch before "doctor --fix" ever makes it.
func CheckAncestorCaseConflicts(repos []RepoPath) []Finding {
	var findings []Finding
	seenAncestor := map[string]bool{}

	for _, rp := range repos {
		if rp.Want == "" || rp.Path == rp.Want || !strings.EqualFold(rp.Path, rp.Want) {
			continue
		}
		oldAncestor, _ := FirstDifferingSegment(rp.Path, rp.Want)
		if seenAncestor[oldAncestor] {
			continue
		}
		seenAncestor[oldAncestor] = true

		depth := len(strings.Split(filepath.Clean(oldAncestor), string(filepath.Separator)))
		wanted := map[string][]string{}
		for _, sib := range repos {
			if sib.Want == "" {
				continue
			}
			if sib.Path != oldAncestor && !strings.HasPrefix(sib.Path, oldAncestor+string(filepath.Separator)) {
				continue
			}
			wantSegs := strings.Split(filepath.Clean(sib.Want), string(filepath.Separator))
			if depth > len(wantSegs) {
				continue
			}
			wantAncestor := strings.Join(wantSegs[:depth], string(filepath.Separator))
			wanted[wantAncestor] = append(wanted[wantAncestor], sib.Repo)
		}
		if len(wanted) < 2 {
			continue
		}

		castings := make([]string, 0, len(wanted))
		for casing := range wanted {
			castings = append(castings, casing)
		}
		sort.Strings(castings)
		parts := make([]string, 0, len(castings))
		for _, casing := range castings {
			names := wanted[casing]
			sort.Strings(names)
			parts = append(parts, fmt.Sprintf("%q (%s)", casing, strings.Join(names, ", ")))
		}
		findings = append(findings, Finding{
			Kind: KindAncestorCaseConflict,
			Path: oldAncestor,
			Message: fmt.Sprintf(
				"repositories under %s disagree on how it should be spelled: %s; doctor --fix refuses to rename it, "+
					"since fixing one of them would un-place the other",
				oldAncestor, strings.Join(parts, ", "),
			),
		})
	}
	return findings
}

// checkRuleCase reports KindRuleCaseMismatch for every rule whose host,
// owner and repo all match repo's remote case-insensitively but not
// exactly: profile.Resolve compares owner and repo with a case sensitive
// "!=", so such a rule never actually applies, silently, and the
// "hasconfig" glob internal/gitconfig renders from the same literal
// fields inherits the same gap, since git's own wildmatch there is case
// sensitive too.
func checkRuleCase(cfg config.Config, name string, repo profile.Repo) []Finding {
	var findings []Finding
	for _, rule := range cfg.Rules {
		if rule.Host != "" && !strings.EqualFold(rule.Host, repo.Remote.Host) {
			continue
		}
		ownerFold := rule.Owner == "" || strings.EqualFold(rule.Owner, repo.Remote.Owner)
		repoFold := rule.Repo == "" || strings.EqualFold(rule.Repo, repo.Remote.Repo)
		if !ownerFold || !repoFold {
			continue
		}
		exactOwner := rule.Owner == "" || rule.Owner == repo.Remote.Owner
		exactRepo := rule.Repo == "" || rule.Repo == repo.Remote.Repo
		if exactOwner && exactRepo {
			continue
		}
		findings = append(findings, Finding{
			Kind: KindRuleCaseMismatch,
			Repo: name,
			Message: fmt.Sprintf(
				"rule host=%q owner=%q repo=%q profile=%q would match this repository but for a case difference (actual owner=%q repo=%q); the rule never applies as written",
				rule.Host, rule.Owner, rule.Repo, rule.Profile, repo.Remote.Owner, repo.Remote.Repo,
			),
		})
	}
	return findings
}

// CheckManagedFiles compares every file "gitcd apply" renders for cfg
// against what actually sits on disk under home, reporting
// KindManagedFileMissing for one that does not exist and
// KindManagedFileStale for one that exists with different content: an
// out of date include block, AC3 of issue #4 asks for by name, is one
// instance of this, and so is any ordering drift inside it, since
// internal/gitconfig.Render always reproduces the current, correctly
// ordered content.
func CheckManagedFiles(cfg config.Config, home string) ([]Finding, error) {
	rendered, err := gitconfig.Render(cfg, home)
	if err != nil {
		return nil, fmt.Errorf("doctor: render the managed configuration: %w", err)
	}

	paths := make([]string, 0, len(rendered))
	for path := range rendered {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	includePath := gitconfig.IncludePath(home)

	var findings []Finding
	for _, path := range paths {
		want := rendered[path]
		got, readErr := os.ReadFile(path)
		switch {
		case errors.Is(readErr, os.ErrNotExist):
			findings = append(findings, Finding{
				Kind:    KindManagedFileMissing,
				Path:    path,
				Fixable: true,
				Message: fmt.Sprintf("%s is missing%s", path, managedFileNote(path, includePath)),
			})
		case readErr != nil:
			return nil, fmt.Errorf("doctor: read %s: %w", path, readErr)
		case string(got) != want:
			findings = append(findings, Finding{
				Kind:    KindManagedFileStale,
				Path:    path,
				Fixable: true,
				Message: fmt.Sprintf("%s no longer matches the rendered configuration%s", path, managedFileNote(path, includePath)),
			})
		}
	}
	return findings, nil
}

// CheckOptedInKeys reports one KindOptedInKey finding per profile key
// cfg opts back in through its "allowUnsafeKeys" list, in a stable order
// by profile name and then key. A configuration with no such key, the
// ordinary case, yields none.
//
// Nothing here is fixable: the opt in is the user's own decision, and
// doctor never undoes one of those. The finding states what the key
// does and which profile carries it.
func CheckOptedInKeys(cfg config.Config) []Finding {
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)

	var findings []Finding
	for _, name := range names {
		for _, key := range cfg.Profiles[name].Keys() {
			why, optedIn := config.UnsafeKeyReason(key)
			if !optedIn {
				continue
			}
			findings = append(findings, Finding{
				Kind: KindOptedInKey,
				Message: fmt.Sprintf(
					"profile %q carries %q, which %s; it applies because %q lists it",
					name, key, why, config.AllowUnsafeKeysKey,
				),
			})
		}
	}
	return findings
}

// managedFileNote names path as the gitcd include file specifically,
// when it is one, so a reported finding reads as the AC3 "missing or
// stale include block" it is, rather than only naming an unfamiliar
// path.
func managedFileNote(path, includePath string) string {
	if path == includePath {
		return "; it is the gitcd include file every profile rule depends on"
	}
	return "; it is a managed profile file \"gitcd apply\" renders"
}

// CheckGlobal reports every global identity finding: KindGlobalIdentityKey
// for each identity key content, the global git configuration's own raw
// text, sets directly, naming movedProfileTarget as the profile it would
// belong in, and KindGlobalIdentityOrder when one or more of them sit
// after the gitcd include, which would keep winning over every includeIf
// value silently.
//
// movedProfileTargetExists tells the finding whether movedProfileTarget
// already names a profile the configuration actually carries: a caller
// resolves this against its own configuration, since this package never
// loads one itself. When false, the message says so rather than naming a
// profile as if the configuration already carried it, which it need not:
// "gitcd init --move-identity" accepts a "--profile" naming any profile
// at all, and nothing here can recover which one was chosen in the past
// for a key moved before this run.
func CheckGlobal(content []byte, globalPath, movedProfileTarget string, movedProfileTargetExists bool) ([]Finding, error) {
	var findings []Finding

	belongsIn := fmt.Sprintf("profile %q", movedProfileTarget)
	if !movedProfileTargetExists {
		belongsIn = fmt.Sprintf("a profile such as %q, which the configuration does not yet define", movedProfileTarget)
	}
	for _, v := range git.FindGlobalIdentityValues(content, git.IdentityKeys) {
		findings = append(findings, Finding{
			Kind: KindGlobalIdentityKey,
			Message: fmt.Sprintf(
				"%s sets %s = %s directly; \"user.useConfigOnly\" cannot suppress it, so every repository gitcd manages, including one no rule matches, gets this identity; it belongs instead in %s",
				globalPath, v.Key, v.Value, belongsIn,
			),
		})
	}

	keys, err := git.IdentityKeysAfterInclude(content, git.IdentityKeys)
	if err != nil {
		return nil, fmt.Errorf("doctor: check %s for an identity key after the include: %w", globalPath, err)
	}
	if len(keys) > 0 {
		findings = append(findings, Finding{
			Kind: KindGlobalIdentityOrder,
			Message: fmt.Sprintf(
				"%s sets %s after the gitcd include; a key there keeps beating every includeIf value silently",
				globalPath, strings.Join(keys, ", "),
			),
		})
	}

	return findings, nil
}
