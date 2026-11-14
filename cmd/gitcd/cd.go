// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
)

// cdCandidate is one repository "gitcd cd" can jump to: its absolute path
// together with the two strings a query is matched against. A linked
// worktree is a candidate exactly like any other managed repository: "cd"
// never hides one, unlike "list", because jumping to one is the whole
// point of having it.
type cdCandidate struct {
	path string
	// rel is path relative to the configured root, "/" separated
	// regardless of host OS, so a query like "acme/widget" disambiguates
	// by owner.
	rel string
	// name is rel's final segment, the repository directory's own name.
	name string
}

// runCd implements "gitcd cd <query>": it resolves query against every
// managed repository and prints the match on stdout. stdin and isTerminal
// are threaded through explicitly, rather than read from os.Stdin inside,
// so a test can exercise the interactive picker without a real terminal.
func runCd(args []string, stdin io.Reader, isTerminal bool, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("gitcd cd", flag.ContinueOnError)
	fset.SetOutput(stderr)
	verbose := fset.Bool("verbose", false, "enable debug logging on stderr")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() != 1 {
		fmt.Fprintln(stderr, "gitcd cd: exactly one query is required")
		fset.Usage()
		return 2
	}
	query := fset.Arg(0)
	logger := newLogger(*verbose, stderr)
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd cd: %v\n", err)
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd cd: %v\n", err)
		return 1
	}

	candidates, err := cdCandidates(ctx, cfg, gitconfig.Dir(home), logger)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd cd: %v\n", err)
		return 1
	}

	match, err := resolveQuery(query, candidates, stdin, isTerminal, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd cd: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, match.path)
	return 0
}

// cdCandidates scans cfg.Root into every cdCandidate "cd" may resolve a
// query to: a linked worktree is always a candidate, exactly like any
// other managed repository, since jumping to one is the whole point of
// having it. See scanRepositories's own doc comment for the rule this
// follows for every other command.
func cdCandidates(ctx context.Context, cfg config.Config, managedDir string, logger *slog.Logger) ([]cdCandidate, error) {
	results, err := scanRepositories(ctx, cfg, managedDir, true)
	if err != nil {
		return nil, fmt.Errorf("cd: %w", err)
	}
	results = keepInspectable(results, logger)

	candidates := make([]cdCandidate, 0, len(results))
	for _, res := range results {
		rel, err := filepath.Rel(cfg.Root, res.Repo.Path)
		if err != nil {
			rel = res.Repo.Path
		}
		rel = filepath.ToSlash(rel)
		candidates = append(candidates, cdCandidate{
			path: res.Repo.Path,
			rel:  rel,
			name: filepath.Base(rel),
		})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].rel < candidates[j].rel })
	return candidates, nil
}

// resolveQuery resolves query against candidates in two tiers.
//
// An exact match, against a candidate's absolute path, its path relative
// to root, or its final name segment, case insensitively, always wins
// outright, even when query also substring-matches other candidates: a
// query that spells out a full name should never fall into a picker just
// because a longer, unrelated path happens to contain it. When more than
// one candidate matches exactly, a name shared by two different
// repositories under different hosts or owners, that exact set becomes the
// ambiguous candidates rather than falling back further.
//
// Lacking any exact match, every candidate whose relative path contains
// query as a case insensitive substring, anywhere, is a fuzzy match: a
// plain, predictable rule a shell function's caller can reason about
// without seeing the matching code, deliberately simpler than a scored
// subsequence match.
//
// Zero matches is an error naming query. More than one is resolved by
// pick.
func resolveQuery(query string, candidates []cdCandidate, stdin io.Reader, isTerminal bool, stderr io.Writer) (cdCandidate, error) {
	var exact []cdCandidate
	for _, c := range candidates {
		if c.path == query || strings.EqualFold(c.rel, query) || strings.EqualFold(c.name, query) {
			exact = append(exact, c)
		}
	}

	pool := exact
	if len(pool) == 0 {
		lower := strings.ToLower(query)
		for _, c := range candidates {
			if strings.Contains(strings.ToLower(c.rel), lower) {
				pool = append(pool, c)
			}
		}
	}

	switch len(pool) {
	case 0:
		return cdCandidate{}, fmt.Errorf("no repository matches %q", query)
	case 1:
		return pool[0], nil
	default:
		return pick(query, pool, stdin, isTerminal, stderr)
	}
}

// pick prints candidates as a numbered list on stderr. With a terminal on
// stdin, it prompts for and reads a selection from stdin, returning an
// error on an empty read or an out of range choice rather than
// re-prompting, predictable over clever. Without a terminal, it returns an
// error naming query without reading anything: the candidates are already
// listed on stderr by the time that error is returned.
func pick(query string, candidates []cdCandidate, stdin io.Reader, isTerminal bool, stderr io.Writer) (cdCandidate, error) {
	fmt.Fprintf(stderr, "%q matches more than one repository:\n", query)
	for i, c := range candidates {
		fmt.Fprintf(stderr, "  %d) %s\n", i+1, c.path)
	}

	if !isTerminal {
		return cdCandidate{}, fmt.Errorf("more than one repository matches %q, and stdin is not a terminal to pick one", query)
	}

	fmt.Fprintf(stderr, "pick one [1-%d]: ", len(candidates))
	scanner := bufio.NewScanner(stdin)
	if !scanner.Scan() {
		// isTerminal came from isTerminalFile, which cannot tell a real
		// terminal that produced no input from a character device such
		// as /dev/null or /dev/zero that was never a terminal at all: an
		// empty read here honestly could be either, so the message
		// names both rather than asserting the one isTerminal claimed.
		return cdCandidate{}, fmt.Errorf("no selection was read for %q: stdin produced no input, which also happens when it is not actually an interactive terminal", query)
	}
	choice := strings.TrimSpace(scanner.Text())
	idx, err := strconv.Atoi(choice)
	if err != nil || idx < 1 || idx > len(candidates) {
		return cdCandidate{}, fmt.Errorf("%q is not a valid choice between 1 and %d", choice, len(candidates))
	}
	return candidates[idx-1], nil
}

// isTerminalFile reports whether f is likely connected to an interactive
// terminal, tested as a character device rather than a pipe or a
// redirected regular file. It is the standard library only way to detect
// this, avoiding a dependency on golang.org/x/term for the one bit "cd"
// needs from it. The test is not exact: /dev/null and /dev/zero are
// themselves character devices, so a script that redirects stdin from
// either is misread as a terminal. pick's own failure message stays
// honest about that rather than claiming stdin definitely is not one.
func isTerminalFile(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
