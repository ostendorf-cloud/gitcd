// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package gitsync_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ostendorf-cloud/gitcd/internal/gitsync"
)

func repos(n int) []gitsync.Repo {
	out := make([]gitsync.Repo, n)
	for i := range out {
		out[i] = gitsync.Repo{Path: fmt.Sprintf("/repo/%d", i), Name: fmt.Sprintf("repo-%d", i)}
	}
	return out
}

func TestRun_FetchesEveryRepositoryExactlyOnce(t *testing.T) {
	t.Parallel()

	want := repos(6)
	var calls atomic.Int32
	fetch := func(_ context.Context, _ string) error {
		calls.Add(1)
		return nil
	}

	var got []gitsync.Result
	gitsync.Run(context.Background(), want, 4, fetch, func(r gitsync.Result) {
		got = append(got, r)
	})

	if int(calls.Load()) != len(want) {
		t.Fatalf("fetch was called %d times, want %d", calls.Load(), len(want))
	}
	if len(got) != len(want) {
		t.Fatalf("onResult was called %d times, want %d", len(got), len(want))
	}

	var names []string
	for _, r := range got {
		if r.Err != nil {
			t.Fatalf("Result for %s carries an unexpected error: %v", r.Repo.Name, r.Err)
		}
		names = append(names, r.Repo.Name)
	}
	sort.Strings(names)
	for i, r := range want {
		if names[i] != r.Name {
			t.Fatalf("fetched repositories = %v, want every one of %v", names, want)
		}
	}
}

// TestRun_ReportsPerRepositoryFailureWithoutAbortingTheRun guards the
// acceptance criterion that one repository's fetch failure neither stops
// the run nor is silently dropped: every repository, including the ones
// after the failing one, must still produce a Result.
func TestRun_ReportsPerRepositoryFailureWithoutAbortingTheRun(t *testing.T) {
	t.Parallel()

	want := repos(5)
	failing := want[2].Path
	boom := errors.New("boom")
	fetch := func(_ context.Context, path string) error {
		if path == failing {
			return boom
		}
		return nil
	}

	results := map[string]error{}
	gitsync.Run(context.Background(), want, 2, fetch, func(r gitsync.Result) {
		results[r.Repo.Path] = r.Err
	})

	if len(results) != len(want) {
		t.Fatalf("got %d results, want one per repository (%d)", len(results), len(want))
	}
	if !errors.Is(results[failing], boom) {
		t.Fatalf("Result for the failing repository carries %v, want %v", results[failing], boom)
	}
	for _, r := range want {
		if r.Path == failing {
			continue
		}
		if results[r.Path] != nil {
			t.Fatalf("Result for %s carries an unexpected error: %v", r.Name, results[r.Path])
		}
	}
}

// TestRun_BoundsConcurrency guards the acceptance criterion that sync
// uses a bounded pool rather than fetching every repository at once: with
// pool 2 and every fetch blocking until released, at most 2 must ever run
// at the same time.
func TestRun_BoundsConcurrency(t *testing.T) {
	t.Parallel()

	const pool = 2
	want := repos(6)

	var inFlight, maxInFlight atomic.Int32
	fetch := func(_ context.Context, _ string) error {
		n := inFlight.Add(1)
		for {
			old := maxInFlight.Load()
			if n <= old || maxInFlight.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)
		return nil
	}

	gitsync.Run(context.Background(), want, pool, fetch, func(gitsync.Result) {})

	if got := maxInFlight.Load(); got > pool {
		t.Fatalf("observed %d fetches in flight at once, want at most %d", got, pool)
	}
	if got := maxInFlight.Load(); got < pool {
		t.Fatalf("observed at most %d fetches in flight at once, want the pool of %d to actually be used", got, pool)
	}
}

// TestRun_OnResultNeverCalledConcurrently guards the "no interleaving"
// requirement sync's progress reporting depends on: even though fetches
// run concurrently, onResult itself must only ever run one call at a
// time. A plain, unsynchronised counter is deliberate here: a concurrent
// call would both race on it, caught by "go test -race", and push it
// above 1.
func TestRun_OnResultNeverCalledConcurrently(t *testing.T) {
	t.Parallel()

	want := repos(8)
	fetch := func(_ context.Context, _ string) error {
		time.Sleep(time.Millisecond)
		return nil
	}

	active := 0
	maxActive := 0
	gitsync.Run(context.Background(), want, 4, fetch, func(gitsync.Result) {
		active++
		if active > maxActive {
			maxActive = active
		}
		time.Sleep(time.Millisecond)
		active--
	})

	if maxActive > 1 {
		t.Fatalf("onResult was active %d times at once, want never more than 1", maxActive)
	}
}

func TestRun_EmptyRepositoryListCallsOnResultForNothing(t *testing.T) {
	t.Parallel()

	calls := 0
	fetch := func(_ context.Context, _ string) error {
		t.Fatal("fetch was called for an empty repository list")
		return nil
	}

	gitsync.Run(context.Background(), nil, 4, fetch, func(gitsync.Result) {
		calls++
	})

	if calls != 0 {
		t.Fatalf("onResult was called %d times, want 0 for an empty repository list", calls)
	}
}
