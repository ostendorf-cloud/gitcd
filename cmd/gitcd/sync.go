// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/git"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
	"github.com/ostendorf-cloud/gitcd/internal/gitsync"
)

// syncPoolSize is how many fetches "gitcd sync" runs at once. The issue
// considered serialising the first SSH connection per host, to avoid an
// agent prompt storm, and rejected it: the SSH agent and ControlMaster
// already coalesce that, so a fixed pool of four is all this needs.
const syncPoolSize = 4

// runSync implements "gitcd sync": every repository under the
// configured root is fetched, never pulled, rebased or merged, through a
// bounded pool of syncPoolSize workers. One repository's fetch failure
// is reported and does not stop the others; the command exits non-zero
// when any repository failed. "--dry-run" prints the repositories that
// would be fetched on stdout, the dry run's own result, instead of
// fetching anything.
func runSync(args []string, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("gitcd sync", flag.ContinueOnError)
	fset.SetOutput(stderr)
	dryRun := fset.Bool("dry-run", false, "print the repositories that would be fetched without fetching them")
	verbose := fset.Bool("verbose", false, "enable debug logging on stderr")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() != 0 {
		fmt.Fprintln(stderr, "gitcd sync: no arguments are accepted")
		fset.Usage()
		return 2
	}
	logger := newLogger(*verbose, stderr)
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd sync: %v\n", err)
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd sync: %v\n", err)
		return 1
	}

	// Every worktree of one repository shares that repository's remote,
	// so fetching one again would only repeat the exact same fetch: see
	// scanRepositories's own doc comment for the rule this follows for
	// every other command. Unlike "list", "cd" and "doctor", sync needs
	// nothing profile.Inspect resolves to fetch a repository, only its
	// own path, so one scanRepositories could not fully inspect is still
	// fetched rather than dropped.
	results, err := scanRepositories(ctx, cfg, gitconfig.Dir(home), false)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd sync: search %s: %v\n", cfg.Root, err)
		return 1
	}
	logger.Debug("discovered repositories", "root", cfg.Root, "count", len(results))

	repos := make([]gitsync.Repo, len(results))
	for i, res := range results {
		repos[i] = gitsync.Repo{Path: res.Repo.Path, Name: repoName(cfg.Root, res.Repo.Path)}
	}

	if *dryRun {
		for _, r := range repos {
			fmt.Fprintln(stdout, r.Name)
		}
		return 0
	}

	if len(repos) == 0 {
		fmt.Fprintln(stderr, "gitcd sync: no repositories found")
		return 0
	}

	failed := 0
	gitsync.Run(ctx, repos, syncPoolSize, git.Fetch, func(r gitsync.Result) {
		if r.Err != nil {
			failed++
			fmt.Fprintf(stderr, "gitcd sync: %s: failed: %v\n", r.Repo.Name, r.Err)
			return
		}
		fmt.Fprintf(stderr, "gitcd sync: %s: fetched\n", r.Repo.Name)
	})

	fmt.Fprintf(stderr, "gitcd sync: %d of %d repositories failed\n", failed, len(repos))
	if failed > 0 {
		return 1
	}
	return 0
}
