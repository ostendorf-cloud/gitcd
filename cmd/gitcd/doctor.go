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
		fixed, outstanding = applyFixes(findings, cfg, home, logger, stdout, stderr, *dryRun)
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
// discoverRepos's walk of cfg.Root, the managed file comparison, and the
// global git configuration checks.
func collectFindings(ctx context.Context, cfg config.Config, home string, logger *slog.Logger, stderr io.Writer) ([]doctor.Finding, error) {
	var findings []doctor.Finding

	paths, err := discoverRepos(cfg.Root)
	if err != nil {
		return nil, fmt.Errorf("search %s: %w", cfg.Root, err)
	}
	logger.Debug("discovered repositories", "root", cfg.Root, "count", len(paths))
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
	globalFindings, err := doctor.CheckGlobal(globalContent, globalPath, defaultMovedProfileName)
	if err != nil {
		return nil, err
	}
	findings = append(findings, globalFindings...)

	return findings, nil
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
			fmt.Fprintf(stdout, "would rename %s to %s\n", f.Path, f.WantPath)
			outstanding = append(outstanding, f)
		case f.Kind == doctor.KindPathCase:
			if err := renameCaseOnly(f.Path, f.WantPath); err != nil {
				fmt.Fprintf(stderr, "gitcd doctor: %v\n", err)
				outstanding = append(outstanding, f)
				continue
			}
			fmt.Fprintf(stdout, "renamed %s to %s\n", f.Path, f.WantPath)
			fixed = append(fixed, f)
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
// a mis-cased ancestor untouched. The move goes through a sibling
// temporary name: a case insensitive but case preserving filesystem, the
// common case on macOS, treats a direct rename between two spellings of
// the same name as a no-op, unlike a case sensitive one, so a plain
// two-argument rename cannot be relied on to force the change on every
// filesystem alike.
//
// When the topmost differing ancestor no longer exists at its old path
// but already exists at the new one, it was already renamed by an
// earlier call in the same "doctor --fix" run, fixing a sibling
// repository under the same mis-cased ancestor: this call is then a
// no-op rather than an error.
func renameCaseOnly(oldPath, newPath string) error {
	oldAncestor, newAncestor := firstDifferingSegment(oldPath, newPath)
	if oldAncestor == newAncestor {
		return nil
	}
	if _, err := os.Stat(oldAncestor); errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(newAncestor); err == nil {
			return nil
		}
	}

	tmp := oldAncestor + ".gitcd-doctor-rename-tmp"
	if err := os.Rename(oldAncestor, tmp); err != nil {
		return fmt.Errorf("rename %s to a temporary name: %w", oldAncestor, err)
	}
	if err := os.Rename(tmp, newAncestor); err != nil {
		// Best effort restore, so a failed second hop does not leave the
		// repository sitting at an unexpected temporary name.
		_ = os.Rename(tmp, oldAncestor)
		return fmt.Errorf("rename %s to %s: %w", oldAncestor, newAncestor, err)
	}
	return nil
}

// firstDifferingSegment walks oldPath and newPath component by
// component and returns the path of the first (topmost) component at
// which they differ, in each of the two inputs, so a caller can rename
// exactly that directory and carry everything below it along in one
// move. Both return values equal oldPath and newPath themselves when
// the two share no common prefix at all.
func firstDifferingSegment(oldPath, newPath string) (string, string) {
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
