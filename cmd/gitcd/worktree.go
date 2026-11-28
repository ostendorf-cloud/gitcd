// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/git"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
	"github.com/ostendorf-cloud/gitcd/internal/status"
)

// errNoLinkedWorktree is wrapped into the error "gitcd worktree" returns
// for a repository that has only its main checkout, so there is nothing to
// pick between.
var errNoLinkedWorktree = errors.New("the repository has no linked worktree")

// errNoWorktreeMatch is wrapped into the error "gitcd worktree" returns
// when its filter matches no worktree of the repository.
var errNoWorktreeMatch = errors.New("no worktree matches")

// errWorktreeExists is wrapped into the error "gitcd worktree add" returns
// when the path it would create already exists.
var errWorktreeExists = errors.New("the worktree path already exists")

// worktreeRepo is the repository "gitcd worktree" works on: where its main
// checkout is and every worktree git has registered for it.
type worktreeRepo struct {
	// main is the main checkout's path. Found through the configured root,
	// it is spelled against the root as configured; found from the working
	// directory, it is the path git reports.
	main string
	// worktrees is git's own list, the main checkout first.
	worktrees []git.Worktree
}

// runWorktree implements "gitcd worktree" and its alias "gitcd wt": it
// lists the worktrees of one repository with branch and dirty state, picks
// one, and prints its path on stdout. "gitcd worktree add" creates one.
//
// The repository is the one the working directory is in, from any of its
// worktrees, or the one "--repo" names. The optional argument filters that
// repository's worktrees by branch or directory name, and with one match it
// prints the path without asking. "add" is reserved as the first argument
// and names the subcommand, so a worktree named "add" is reached by a
// longer part of its branch or its path.
func runWorktree(args []string, stdin io.Reader, isTerminal bool, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "add" {
		return runWorktreeAdd(args[1:], stdin, isTerminal, stdout, stderr)
	}

	fset := flag.NewFlagSet("gitcd worktree", flag.ContinueOnError)
	fset.SetOutput(stderr)
	repoQuery := fset.String("repo", "", "pick from the worktrees of this repository instead of the current one")
	verbose := fset.Bool("verbose", false, "enable debug logging on stderr")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() > 1 {
		fmt.Fprintln(stderr, "gitcd worktree: at most one filter is accepted")
		fset.Usage()
		return 2
	}
	filter := fset.Arg(0)
	logger := newLogger(*verbose, stderr)
	ctx := context.Background()

	cfg, home, ok := loadForWorktree("worktree", stderr)
	if !ok {
		return 1
	}
	repo, err := locateWorktreeRepo(ctx, cfg, gitconfig.Dir(home), *repoQuery, stdin, isTerminal, stderr, logger)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd worktree: %v\n", err)
		return 1
	}

	chosen, err := chooseWorktree(ctx, repo, filter, stdin, isTerminal, stderr, logger)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd worktree: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, chosen.Path)
	return 0
}

// loadForWorktree loads the configuration and the home directory, naming
// command in the message it prints on stderr when either fails.
func loadForWorktree(command string, stderr io.Writer) (config.Config, string, bool) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd %s: %v\n", command, err)
		return config.Config{}, "", false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd %s: %v\n", command, err)
		return config.Config{}, "", false
	}
	return cfg, home, true
}

// locateWorktreeRepo finds the repository "gitcd worktree" works on. With an
// empty repoQuery it is the repository the working directory belongs to:
// git answers the same worktree list from the main checkout and from every
// linked worktree, so running from inside one still lists them all. With a
// repoQuery it resolves the query against the repositories under the
// configured root the way "cd" does, except that a linked worktree is never
// a candidate, so the query names a repository and not one of its
// worktrees.
func locateWorktreeRepo(ctx context.Context, cfg config.Config, managedDir, repoQuery string, stdin io.Reader, isTerminal bool, stderr io.Writer, logger *slog.Logger) (worktreeRepo, error) {
	if repoQuery == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return worktreeRepo{}, fmt.Errorf("find the working directory: %w", err)
		}
		list, err := git.ListWorktrees(ctx, cwd)
		if err != nil {
			return worktreeRepo{}, fmt.Errorf("the working directory is not inside a git repository, name one with --repo: %w", err)
		}
		if len(list) == 0 {
			return worktreeRepo{}, fmt.Errorf("git lists no worktree for %s", cwd)
		}
		return worktreeRepo{main: list[0].Path, worktrees: list}, nil
	}

	candidates, err := repoCandidates(ctx, cfg, managedDir, false, logger)
	if err != nil {
		return worktreeRepo{}, err
	}
	match, err := resolveQuery(repoQuery, candidates, stdin, isTerminal, stderr)
	if err != nil {
		return worktreeRepo{}, err
	}
	list, err := git.ListWorktrees(ctx, match.path)
	if err != nil {
		return worktreeRepo{}, err
	}
	return worktreeRepo{main: match.path, worktrees: list}, nil
}

// enterableWorktrees returns the worktrees of list a shell can change into:
// not the bare repository entry, and not one git would prune, whose
// directory is gone. The main checkout stays first, so a user inside a
// linked worktree can go back to it, and the linked ones follow ordered by
// branch and then path, whatever order git registered them in. linked
// reports how many of the result are linked worktrees.
func enterableWorktrees(list []git.Worktree) (enterable []git.Worktree, linked int) {
	var others []git.Worktree
	for i, w := range list {
		switch {
		case w.Bare || w.Prunable:
		case i == 0:
			enterable = append(enterable, w)
		default:
			others = append(others, w)
		}
	}
	sort.SliceStable(others, func(i, j int) bool {
		if others[i].Branch != others[j].Branch {
			return others[i].Branch < others[j].Branch
		}
		return others[i].Path < others[j].Path
	})
	return append(enterable, others...), len(others)
}

// matchWorktrees returns the worktrees of pool a filter selects, in two
// tiers like resolveQuery's. A worktree whose branch or directory name is
// exactly filter, or whose path is, case insensitively, always wins
// outright. Lacking one, every worktree whose branch or directory name
// contains filter is a match. An empty filter selects every worktree.
func matchWorktrees(pool []git.Worktree, filter string) []git.Worktree {
	if filter == "" {
		return pool
	}
	lower := strings.ToLower(filter)
	var exact, fuzzy []git.Worktree
	for _, w := range pool {
		branch := strings.ToLower(w.Branch)
		dir := strings.ToLower(worktreeDirName(w.Path))
		switch {
		case branch == lower || dir == lower || strings.EqualFold(w.Path, filter):
			exact = append(exact, w)
		case strings.Contains(branch, lower) || strings.Contains(dir, lower):
			fuzzy = append(fuzzy, w)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return fuzzy
}

// worktreeDirName is the final segment of a worktree's path.
func worktreeDirName(path string) string {
	path = strings.TrimRight(strings.ReplaceAll(path, `\`, "/"), "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// chooseWorktree picks one worktree of repo: the filter narrows them, one
// left is returned without a question, several go to the picker, and none
// is an error. A repository with no linked worktree is an error whatever
// the filter says, since there is nothing to move between.
func chooseWorktree(ctx context.Context, repo worktreeRepo, filter string, stdin io.Reader, isTerminal bool, stderr io.Writer, logger *slog.Logger) (git.Worktree, error) {
	pool, linked := enterableWorktrees(repo.worktrees)
	for _, w := range repo.worktrees {
		if w.Prunable {
			logger.Debug("a worktree git would prune is not offered", "path", w.Path)
		}
	}
	if linked == 0 {
		return git.Worktree{}, fmt.Errorf("%w: %s", errNoLinkedWorktree, repo.main)
	}

	label := func(ws []git.Worktree) []string { return worktreeLabels(ctx, repo, ws) }
	return selectWorktree(pool, filter, repo.main, label, stdin, isTerminal, stderr)
}

// selectWorktree narrows pool by filter and resolves what is left. label
// renders a list of worktrees into the lines the picker shows, one each,
// and runs only when a question is actually asked.
func selectWorktree(pool []git.Worktree, filter, repoPath string, label func([]git.Worktree) []string, stdin io.Reader, isTerminal bool, stderr io.Writer) (git.Worktree, error) {
	matches := matchWorktrees(pool, filter)
	switch len(matches) {
	case 0:
		return git.Worktree{}, fmt.Errorf("%w %q in %s", errNoWorktreeMatch, filter, repoPath)
	case 1:
		return matches[0], nil
	}

	var header, notTerminal, subject string
	if filter == "" {
		header = fmt.Sprintf("worktrees of %s:", repoPath)
		notTerminal = fmt.Sprintf("%s has more than one worktree, and stdin is not a terminal to pick one", repoPath)
		subject = "a worktree of " + repoPath
	} else {
		header = fmt.Sprintf("%q matches more than one worktree of %s:", filter, repoPath)
		notTerminal = fmt.Sprintf("more than one worktree of %s matches %q, and stdin is not a terminal to pick one", repoPath, filter)
		subject = fmt.Sprintf("%q", filter)
	}
	idx, err := pickIndex(header, notTerminal, subject, label(matches), stdin, isTerminal, stderr)
	if err != nil {
		return git.Worktree{}, err
	}
	return matches[idx], nil
}

// worktreeLabels renders each worktree as one picker line: its branch, its
// dirty state and its path, the main checkout marked. The state comes from
// the same reading "gitcd status" uses, one call per worktree, so a
// worktree git cannot read shows "error" rather than hiding.
func worktreeLabels(ctx context.Context, repo worktreeRepo, worktrees []git.Worktree) []string {
	branches := make([]string, len(worktrees))
	states := make([]string, len(worktrees))
	width := 0
	for i, w := range worktrees {
		branches[i] = w.Branch
		if w.Detached {
			branches[i] = "(detached)"
		}
		if len(repo.worktrees) > 0 && w.Path == repo.worktrees[0].Path {
			branches[i] += " (main)"
		}
		if len(branches[i]) > width {
			width = len(branches[i])
		}

		rs := repoStatus{Error: "unreadable"}
		if st, err := status.Inspect(ctx, w.Path); err == nil {
			rs = toRepoStatus(w.Path, st)
		}
		states[i] = stateColumn(rs)
		if w.Locked {
			states[i] += ", locked"
		}
	}

	labels := make([]string, len(worktrees))
	for i, w := range worktrees {
		labels[i] = fmt.Sprintf("%-*s  %-7s  %s", width, branches[i], states[i], w.Path)
	}
	return labels
}

// runWorktreeAdd implements "gitcd worktree add <branch>": it creates the
// worktree of branch at <worktreeDir>/<branch> through "git worktree add"
// and prints its path. A branch that does not exist yet is created, from
// "--base" or the repository's HEAD, and one that does is checked out. The
// shell wrapper changes into the printed directory, like it does for every
// command that prints one. Without worktreeDir in the configuration there
// is no directory to create in, and the command refuses.
func runWorktreeAdd(args []string, stdin io.Reader, isTerminal bool, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("gitcd worktree add", flag.ContinueOnError)
	fset.SetOutput(stderr)
	repoQuery := fset.String("repo", "", "create the worktree in this repository instead of the current one")
	base := fset.String("base", "", "start a new branch at this commit or branch instead of the repository's HEAD")
	dryRun := fset.Bool("dry-run", false, "print the path without creating the worktree")
	verbose := fset.Bool("verbose", false, "enable debug logging on stderr")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() != 1 {
		fmt.Fprintln(stderr, "gitcd worktree add: exactly one branch name is required")
		fset.Usage()
		return 2
	}
	branch := fset.Arg(0)
	logger := newLogger(*verbose, stderr)
	ctx := context.Background()

	cfg, home, ok := loadForWorktree("worktree add", stderr)
	if !ok {
		return 1
	}
	if cfg.WorktreeDir == "" {
		fmt.Fprintln(stderr, "gitcd worktree add: worktreeDir is not set in the configuration, so there is no directory to create the worktree in")
		return 1
	}
	repo, err := locateWorktreeRepo(ctx, cfg, gitconfig.Dir(home), *repoQuery, stdin, isTerminal, stderr, logger)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd worktree add: %v\n", err)
		return 1
	}

	target, err := planWorktreeAdd(ctx, cfg, repo.main, branch)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd worktree add: %v\n", err)
		return 1
	}
	exists := git.BranchExists(ctx, repo.main, branch)
	if exists && *base != "" {
		fmt.Fprintf(stderr, "gitcd worktree add: --base only applies to a new branch, and %s already exists\n", branch)
		return 1
	}

	if *dryRun {
		fmt.Fprintf(stderr, "gitcd worktree add: would create %s for branch %s in %s\n", target, branch, repo.main)
		fmt.Fprintln(stdout, target)
		return 0
	}
	if err := git.AddWorktree(ctx, repo.main, target, branch, !exists, *base, stderr); err != nil {
		fmt.Fprintf(stderr, "gitcd worktree add: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, target)
	return 0
}

// planWorktreeAdd works out where "worktree add" creates the worktree of
// branch for the repository whose main checkout is mainPath, and refuses
// everything that must not reach "git worktree add": a branch name git
// would not accept, one that places the path outside the directory, and a
// path that already exists. It creates nothing.
func planWorktreeAdd(ctx context.Context, cfg config.Config, mainPath, branch string) (string, error) {
	dir, err := config.ResolveWorktreeDir(cfg.WorktreeDir, mainPath)
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", fmt.Errorf("%w: worktreeDir is not set", config.ErrInvalidWorktreeDir)
	}
	target, err := config.WorktreeTarget(dir, branch)
	if err != nil {
		return "", err
	}
	if err := git.CheckBranchName(ctx, branch); err != nil {
		return "", err
	}
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("%w: %s", errWorktreeExists, target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("check %s: %w", target, err)
	}
	return target, nil
}
