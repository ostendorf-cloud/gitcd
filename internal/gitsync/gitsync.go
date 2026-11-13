// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package gitsync fetches a set of repositories through a bounded pool
// of workers. It never pulls, rebases or merges: internal/git.Fetch is
// the only git operation it runs, so a repository's working tree is
// never touched, only its remote tracking refs.
//
// Named "gitsync" rather than "sync" so a caller importing it never
// shadows the standard library package of that name.
package gitsync

import "context"

// Repo is one repository to fetch: Path is where it lives on disk, Name
// is how a caller should report it, typically its path relative to the
// configured root.
type Repo struct {
	Path string
	Name string
}

// Result is the outcome of fetching one Repo. Err is nil on success.
type Result struct {
	Repo Repo
	Err  error
}

// Fetcher fetches one repository. internal/git.Fetch implements it; a
// test supplies a fake instead, so this package's own tests never shell
// out to git.
type Fetcher func(ctx context.Context, path string) error

// Run fetches every repo in repos through fetch, running at most pool
// fetches at once, and calls onResult once per repo as its fetch
// completes. onResult is always called from the single goroutine Run
// itself runs on, one call fully returning before the next begins, so a
// caller that writes a progress line from onResult never has it
// interleaved with another repository's line, however many fetches are
// running concurrently underneath. A pool of 1 or less runs every fetch
// sequentially.
func Run(ctx context.Context, repos []Repo, pool int, fetch Fetcher, onResult func(Result)) {
	if pool < 1 {
		pool = 1
	}

	jobs := make(chan Repo)
	results := make(chan Result)

	workers := pool
	if workers > len(repos) {
		workers = len(repos)
	}
	for i := 0; i < workers; i++ {
		go worker(ctx, jobs, results, fetch)
	}

	go func() {
		defer close(jobs)
		for _, r := range repos {
			jobs <- r
		}
	}()

	for range repos {
		onResult(<-results)
	}
}

// worker fetches every Repo jobs delivers until it is closed, sending
// one Result per job to results.
func worker(ctx context.Context, jobs <-chan Repo, results chan<- Result, fetch Fetcher) {
	for r := range jobs {
		err := fetch(ctx, r.Path)
		results <- Result{Repo: r, Err: err}
	}
}
