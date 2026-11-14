// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/doctor"
	"github.com/ostendorf-cloud/gitcd/internal/git"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
	"github.com/ostendorf-cloud/gitcd/internal/layout"
	"github.com/ostendorf-cloud/gitcd/internal/remote"
)

// doctorOutput is the shape "gitcd doctor --json" encodes: both
// configuration homes AC7 of issue #4 asks doctor to always print, plus
// what "--fix" repaired and what is still outstanding.
type doctorOutput struct {
	ConfigPath string           `json:"configPath"`
	ManagedDir string           `json:"managedDir"`
	Fixed      []doctor.Finding `json:"fixed,omitempty"`
	Findings   []doctor.Finding `json:"findings,omitempty"`
}

// runDoctor implements "gitcd doctor": every per repository check under
// the configured root, plus the managed file and global git
// configuration checks, reported as one finding per problem. The walk
// itself is discoverRepos, the same one "status" and "sync" already use:
// issue #3's internal/scan, built for "list" and "adopt" with a richer
// repository value, lands separately and the two walks are reconciled
// once both exist, per AGENTS.md. A repository reachable only through a
// "hasconfig" rule, outside root, is not found by this walk and so is
// not checked; internal/doctor's own doc comment and docs/concepts/gitcd.md's
// open question about "list" cover the same gap for the same reason.
//
// "--fix" repairs only what is mechanical: a stale or missing managed
// file (re-rendered, exactly as "gitcd apply" would), and a path that
// differs from the layout only in character case (renamed in place).
// Every other finding, identity drift, a stale pin, a structural path
// mismatch, a rule that almost matches but for a case difference, and
// every global identity key, is always reported and never touched: a
// value doctor did not write is never one it overwrites, and a drift
// that is not mechanical is skipped and reported rather than guessed at.
func runDoctor(args []string, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("gitcd doctor", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fix := fset.Bool("fix", false, "repair every mechanical finding: a stale or missing managed file, and a case only path mismatch")
	dryRun := fset.Bool("dry-run", false, "with --fix, print what would change without writing or moving anything")
	jsonOutput := fset.Bool("json", false, "print structured output instead of a text report")
	verbose := fset.Bool("verbose", false, "enable debug logging on stderr")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() != 0 {
		fmt.Fprintln(stderr, "gitcd doctor: no arguments are accepted")
		fset.Usage()
		return 2
	}
	logger := newLogger(*verbose, stderr)
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd doctor: %v\n", err)
		return 1
	}
	cfgPath, err := config.Path()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd doctor: %v\n", err)
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd doctor: %v\n", err)
		return 1
	}
	managedDir := gitconfig.Dir(home)

	findings, err := collectFindings(ctx, cfg, home, logger, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd doctor: %v\n", err)
		return 1
	}

	var fixed, outstanding []doctor.Finding
	if *fix {
		// "--fix" prints a progress line per repair it makes. With
		// "--json", stdout must carry nothing but the single encoded
		// object a caller pipes to "jq", so every progress line goes to
		// stderr instead, exactly where every other log line already
		// goes.
		progress := stdout
		if *jsonOutput {
			progress = stderr
		}
		fixed, outstanding = applyFixes(findings, cfg, home, logger, progress, stderr, *dryRun)
	} else {
		outstanding = findings
	}

	if *jsonOutput {
		out := doctorOutput{ConfigPath: cfgPath, ManagedDir: managedDir, Fixed: fixed, Findings: outstanding}
		if err := json.NewEncoder(stdout).Encode(out); err != nil {
			fmt.Fprintf(stderr, "gitcd doctor: encode output: %v\n", err)
			return 1
		}
	} else {
		writeDoctorReport(stdout, cfgPath, managedDir, fixed, outstanding)
	}

	if len(outstanding) > 0 {
		return 1
	}
	return 0
}

// collectFindings runs every check doctor performs: one pass over
// discoverRepos's walk of cfg.Root, the ancestor case conflict check that
// pass also makes possible, the managed file comparison, and the global
// git configuration checks.
func collectFindings(ctx context.Context, cfg config.Config, home string, logger *slog.Logger, stderr io.Writer) ([]doctor.Finding, error) {
	var findings []doctor.Finding

	paths, err := discoverRepos(cfg.Root)
	if err != nil {
		return nil, fmt.Errorf("search %s: %w", cfg.Root, err)
	}
	logger.Debug("discovered repositories", "root", cfg.Root, "count", len(paths))

	var repoPaths []doctor.RepoPath
	for _, path := range paths {
		name := repoName(cfg.Root, path)
		repoFindings, err := doctor.CheckRepo(ctx, cfg, home, name, path)
		if err != nil {
			// One repository's own failure, a remote gitcd cannot parse
			// for instance, is reported and skipped rather than aborting
			// the whole run: a fleet wide command must still report on
			// every other repository.
			fmt.Fprintf(stderr, "gitcd doctor: %s: %v\n", name, err)
			continue
		}
		findings = append(findings, repoFindings...)

		// want is computed again here, rather than threaded out of
		// CheckRepo, so a repository already sitting at the path its own
		// remote implies, which carries no finding of its own, still
		// feeds CheckAncestorCaseConflicts below: it is exactly the one
		// a careless ancestor rename would un-place, so it still has to
		// be in this list. A repository CheckRepo itself failed on, no
		// origin remote being the common case, is already reported
		// loudly on stderr above and excluded from this roster too by
		// the "continue" above: a known limitation, since it then casts
		// no vote in the ancestor check either, and an ancestor rename
		// could in principle carry it along silently.
		want, err := wantPath(ctx, cfg, path)
		if err != nil {
			continue
		}
		repoPaths = append(repoPaths, doctor.RepoPath{Repo: name, Path: path, Want: want})
	}

	if conflicts := doctor.CheckAncestorCaseConflicts(repoPaths); len(conflicts) > 0 {
		contested := make([]string, 0, len(conflicts))
		for _, c := range conflicts {
			contested = append(contested, c.Path)
		}
		findings = filterContestedPathCase(findings, contested)
		findings = append(findings, conflicts...)
	}

	managedFindings, err := doctor.CheckManagedFiles(cfg, home)
	if err != nil {
		return nil, err
	}
	findings = append(findings, managedFindings...)

	globalPath, err := git.GlobalConfigPath()
	if err != nil {
		return nil, err
	}
	globalContent, err := os.ReadFile(globalPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", globalPath, err)
	}
	movedProfileTarget := movedProfileTargetName(cfg)
	_, movedProfileExists := cfg.Profiles[movedProfileTarget]
	globalFindings, err := doctor.CheckGlobal(globalContent, globalPath, movedProfileTarget, movedProfileExists)
	if err != nil {
		return nil, err
	}
	findings = append(findings, globalFindings...)

	return findings, nil
}

// movedProfileTargetName names the profile a global identity key finding
// points at as where that key belongs: cfg.DefaultProfile, the profile
// the configuration's own rules already apply to a remote no rule
// matches, exactly the scope a key set directly in the global git
// configuration also has, when one is configured; defaultMovedProfileName,
// "gitcd init --move-identity"'s own default, otherwise. Naming
// defaultMovedProfileName unconditionally would claim a user who moved
// their identity with "gitcd init --move-identity --profile work" still
// has it sitting in a profile called "global".
func movedProfileTargetName(cfg config.Config) string {
	if cfg.DefaultProfile != "" {
		return cfg.DefaultProfile
	}
	return defaultMovedProfileName
}

// applyFixes repairs every mechanical finding in all, splitting the
// input into what was fixed and what is still outstanding. Every
// managed file finding is repaired by one re-render, since
// internal/gitconfig.Render always reproduces every managed file's
// current, correct content in one pass; a case only path mismatch is
// repaired by renaming the repository in place.
func applyFixes(
	all []doctor.Finding, cfg config.Config, home string, logger *slog.Logger, stdout, stderr io.Writer, dryRun bool,
) (fixed, outstanding []doctor.Finding) {
	hasManaged := false
	for _, f := range all {
		if f.Kind == doctor.KindManagedFileMissing || f.Kind == doctor.KindManagedFileStale {
			hasManaged = true
			break
		}
	}

	managedWritten := false
	if hasManaged {
		if dryRun {
			fmt.Fprintf(stdout, "would rerender every managed file under %s\n", gitconfig.Dir(home))
		} else if err := rerenderManagedFiles(cfg, home, logger, stdout); err != nil {
			fmt.Fprintf(stderr, "gitcd doctor: %v\n", err)
		} else {
			managedWritten = true
		}
	}

	for _, f := range all {
		switch {
		case (f.Kind == doctor.KindManagedFileMissing || f.Kind == doctor.KindManagedFileStale) && managedWritten:
			fixed = append(fixed, f)
		case f.Kind == doctor.KindPathCase && dryRun:
			oldAncestor, newAncestor := doctor.FirstDifferingSegment(f.Path, f.WantPath)
			fmt.Fprintf(stdout, "would rename %s to %s\n", oldAncestor, newAncestor)
			outstanding = append(outstanding, f)
		case f.Kind == doctor.KindPathCase:
			oldAncestor, newAncestor, moved, err := renameCaseOnly(f.Path, f.WantPath)
			if err != nil {
				fmt.Fprintf(stderr, "gitcd doctor: %v\n", err)
				outstanding = append(outstanding, f)
				continue
			}
			if moved {
				fmt.Fprintf(stdout, "renamed %s to %s\n", oldAncestor, newAncestor)
			}
			// renameCaseOnly only ever moves the topmost differing
			// segment, so a repository differing at more than one
			// segment is not fully placed by this one call: the
			// finding stays outstanding, correctly keeping the exit
			// code non-zero, until a later run reaches the deeper
			// segment too.
			actual := newAncestor + strings.TrimPrefix(f.Path, oldAncestor)
			if actual == f.WantPath {
				fixed = append(fixed, f)
			} else {
				outstanding = append(outstanding, f)
			}
		default:
			outstanding = append(outstanding, f)
		}
	}
	return fixed, outstanding
}

// rerenderManagedFiles re-renders and writes every managed file for cfg,
// exactly what "gitcd apply" itself does for a non dry run, reused here
// rather than reimplemented so the two can never drift into two
// different ways of performing the same write.
func rerenderManagedFiles(cfg config.Config, home string, logger *slog.Logger, stdout io.Writer) error {
	rendered, err := gitconfig.Render(cfg, home)
	if err != nil {
		return err
	}
	return writeRenderedFiles(rendered, logger, stdout)
}

// renameCaseOnly moves oldPath to newPath, a path differing from it only
// in character case, possibly in a segment above the repository's own
// leaf directory, a mis-cased owner segment for instance: it renames the
// topmost segment that actually differs, carrying everything below it
// along in the same move, rather than only the leaf, which would leave
// a mis-cased ancestor untouched, and returns that ancestor pair, the
// directories actually renamed, so a caller reports what really moved
// rather than the repository's own path. The move goes through a
// sibling temporary name: a case insensitive but case preserving
// filesystem, the common case on macOS, treats a direct rename between
// two spellings of the same name as a no-op, unlike a case sensitive
// one, so a plain two-argument rename cannot be relied on to force the
// change on every filesystem alike.
//
// moved reports whether this call itself performed that rename. It is
// false for the no-op case described next, and whenever err is not nil:
// only a call that actually moved something on disk reports true.
//
// When newAncestor's own, exact spelling already exists as an entry of
// its parent directory, it was already renamed by an earlier call in
// the same "doctor --fix" run, fixing a sibling repository under the
// same mis-cased ancestor: this call is then a no-op rather than an
// error. That exact spelling is read back from the parent directory's
// own entries, through entryExistsExactly, rather than asked of
// "os.Stat" directly: a case insensitive but case preserving filesystem,
// the common case on macOS (APFS), resolves "GitHub.com" and
// "github.com" to the very same entry through "os.Stat" alone, so an
// earlier version of this check that asked only whether oldAncestor had
// stopped existing never saw that happen there, fell through to the
// real rename below regardless, and printed a second, spurious "moved"
// result for a rename that had, in effect, already happened.
//
// Renaming only the topmost differing segment means a repository
// differing from its wanted path at more than one segment is not fully
// placed by one call: the caller compares the resulting path against
// the finding's own WantPath and must not report it fixed until they
// match, which can take more than one "doctor --fix" run to reach.
func renameCaseOnly(oldPath, newPath string) (oldAncestor, newAncestor string, moved bool, err error) {
	oldAncestor, newAncestor = doctor.FirstDifferingSegment(oldPath, newPath)
	if oldAncestor == newAncestor {
		return oldAncestor, newAncestor, false, nil
	}
	if entryExistsExactly(newAncestor) {
		return oldAncestor, newAncestor, false, nil
	}

	tmp := oldAncestor + ".gitcd-doctor-rename-tmp"
	if err := os.Rename(oldAncestor, tmp); err != nil {
		return oldAncestor, newAncestor, false, fmt.Errorf("rename %s to a temporary name: %w", oldAncestor, err)
	}
	if err := os.Rename(tmp, newAncestor); err != nil {
		// Best effort restore, so a failed second hop does not leave the
		// repository sitting at an unexpected temporary name.
		_ = os.Rename(tmp, oldAncestor)
		return oldAncestor, newAncestor, false, fmt.Errorf("rename %s to %s: %w", oldAncestor, newAncestor, err)
	}
	return oldAncestor, newAncestor, true, nil
}

// entryExistsExactly reports whether path's own final segment already
// exists, spelled exactly as path itself spells it, as an entry of its
// parent directory. "os.Stat" cannot answer this on a case insensitive
// but case preserving filesystem, the common case on macOS (APFS): it
// resolves two different spellings of the same name to the very same
// entry, so it cannot tell a directory already spelled correctly apart
// from one that merely happens to fold to the same name. Reading the
// parent directory back and comparing each entry's own, exact name can,
// on either kind of filesystem, the same technique
// TestRunDoctor_FixRenamesACaseOnlyPathMismatch's own test helper
// already relies on for the very same reason.
//
// A parent directory that cannot be read at all, because it does not
// exist yet for instance, is read as "not found" here rather than as an
// error: the real rename attempt that follows surfaces that failure on
// its own, correctly, rather than this check silently doing nothing in
// its place.
func entryExistsExactly(path string) bool {
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return false
	}
	want := filepath.Base(path)
	for _, e := range entries {
		if e.Name() == want {
			return true
		}
	}
	return false
}

// wantPath renders the path the configured layout would place path's own
// repository at, read from its remote directly rather than through
// doctor.CheckRepo's own profile.Inspect, which this package's
// CheckAncestorCaseConflicts call does not need the fuller identity
// state Inspect also resolves for.
func wantPath(ctx context.Context, cfg config.Config, path string) (string, error) {
	remoteURL, err := git.RemoteURL(ctx, path)
	if err != nil {
		return "", fmt.Errorf("read the origin remote of %s: %w", path, err)
	}
	r, err := remote.Parse(remoteURL, cfg.AliasMap())
	if err != nil {
		return "", err
	}
	rel, err := layout.Render(cfg.LayoutFor(r.Host), r)
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg.Root, rel), nil
}

// filterContestedPathCase drops a KindPathCase finding only when
// repairing it would actually rename one of contested, the ancestor
// directories CheckAncestorCaseConflicts found two or more repositories
// disagreeing about: a KindAncestorCaseConflict finding replaces it
// instead, so "doctor --fix" never attempts the rename a KindPathCase
// finding would otherwise have it try.
//
// A finding's own repair, renameCaseOnly, renames
// doctor.FirstDifferingSegment(f.Path, f.WantPath), the topmost segment
// at which the two actually differ, not necessarily f.Path itself: a
// repository that merely sits somewhere under a contested ancestor, but
// whose own mismatch is a deeper, unrelated segment, is repaired by
// renaming only that deeper segment, never the contested one, so it must
// stay reported and fixable. Only a finding whose own differing segment
// equals, or is itself an ancestor of, one of contested is suppressed,
// since only then does the repair reach far enough up the tree to carry
// the contested ancestor along.
func filterContestedPathCase(findings []doctor.Finding, contested []string) []doctor.Finding {
	kept := make([]doctor.Finding, 0, len(findings))
	for _, f := range findings {
		if f.Kind == doctor.KindPathCase {
			ownAncestor, _ := doctor.FirstDifferingSegment(f.Path, f.WantPath)
			if coversAny(ownAncestor, contested) {
				continue
			}
		}
		kept = append(kept, f)
	}
	return kept
}

// coversAny reports whether ancestor equals, or is itself an ancestor
// of, any of paths: whether a rename of ancestor would carry one of
// paths along with it.
func coversAny(ancestor string, paths []string) bool {
	for _, p := range paths {
		if ancestor == p || strings.HasPrefix(p, ancestor+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// writeDoctorReport renders doctor's text report: both configuration
// homes AC7 asks doctor to always print, then one line per fixed and per
// still outstanding finding.
func writeDoctorReport(stdout io.Writer, cfgPath, managedDir string, fixed, outstanding []doctor.Finding) {
	fmt.Fprintf(stdout, "config file: %s\n", cfgPath)
	fmt.Fprintf(stdout, "managed directory: %s\n", managedDir)
	if len(fixed) == 0 && len(outstanding) == 0 {
		fmt.Fprintln(stdout, "no findings")
		return
	}
	for _, f := range fixed {
		fmt.Fprintf(stdout, "fixed [%s] %s: %s\n", f.Kind, findingSubject(f), f.Message)
	}
	for _, f := range outstanding {
		fmt.Fprintf(stdout, "[%s] %s: %s\n", f.Kind, findingSubject(f), f.Message)
	}
}

// findingSubject names what f is about, for the text report: the
// repository when there is one, the path when there is no repository but
// a path, "global" otherwise.
func findingSubject(f doctor.Finding) string {
	switch {
	case f.Repo != "":
		return f.Repo
	case f.Path != "":
		return f.Path
	default:
		return "global"
	}
}
