// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
	"github.com/ostendorf-cloud/gitcd/internal/status"
)

// repoStatus is one repository's status rendering, the shape both the
// table and the "--json" output are built from, so the two never drift
// apart.
type repoStatus struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Branch    string `json:"branch,omitempty"`
	Detached  bool   `json:"detached"`
	Unborn    bool   `json:"unborn"`
	Upstream  string `json:"upstream,omitempty"`
	HasSync   bool   `json:"hasSyncComparison"`
	InSync    bool   `json:"inSync"`
	Ahead     int    `json:"ahead"`
	Behind    int    `json:"behind"`
	Staged    bool   `json:"staged"`
	Unstaged  bool   `json:"unstaged"`
	Untracked bool   `json:"untracked"`
	Dirty     bool   `json:"dirty"`
	Error     string `json:"error,omitempty"`
}

// runStatus implements "gitcd status": every repository under the
// configured root, its branch, its dirty state and how far it is ahead
// or behind its upstream, one line each. "--strict" turns a dirty
// repository, by any of a staged change, an unstaged change or an
// untracked file, into a non zero exit, so a script can gate on a clean
// fleet without parsing the table itself.
func runStatus(args []string, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("gitcd status", flag.ContinueOnError)
	fset.SetOutput(stderr)
	jsonOutput := fset.Bool("json", false, "print structured output instead of a table")
	strict := fset.Bool("strict", false, "exit non-zero when any repository is dirty")
	verbose := fset.Bool("verbose", false, "enable debug logging on stderr")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() != 0 {
		fmt.Fprintln(stderr, "gitcd status: no arguments are accepted")
		fset.Usage()
		return 2
	}
	logger := newLogger(*verbose, stderr)
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd status: %v\n", err)
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd status: %v\n", err)
		return 1
	}

	// A linked worktree carries its own branch and its own dirty state,
	// distinct from its main checkout's, so it always gets its own row:
	// see scanRepositories's own doc comment for the rule this follows
	// for every other command. Unlike "list", "cd" and "doctor", status
	// needs nothing profile.Inspect resolves, only a repository's own
	// path, so a repository scanRepositories could not fully inspect is
	// still given a row rather than dropped.
	results, err := scanRepositories(ctx, cfg, gitconfig.Dir(home), true)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd status: search %s: %v\n", cfg.Root, err)
		return 1
	}
	logger.Debug("discovered repositories", "root", cfg.Root, "count", len(results))

	var rows []repoStatus
	failed := false
	for _, res := range results {
		path := res.Repo.Path
		name := repoName(cfg.Root, path)
		st, err := status.Inspect(ctx, path)
		if err != nil {
			fmt.Fprintf(stderr, "gitcd status: %s: %v\n", name, err)
			rows = append(rows, repoStatus{Name: name, Path: path, Error: err.Error()})
			failed = true
			continue
		}
		rows = append(rows, toRepoStatus(name, st))
	}

	if *jsonOutput {
		if err := json.NewEncoder(stdout).Encode(rows); err != nil {
			fmt.Fprintf(stderr, "gitcd status: encode output: %v\n", err)
			return 1
		}
	} else {
		writeStatusTable(stdout, rows)
	}

	if failed {
		return 1
	}
	if *strict {
		for _, r := range rows {
			if r.Dirty {
				return 1
			}
		}
	}
	return 0
}

// toRepoStatus renders one status.State into the shape status prints.
func toRepoStatus(name string, st status.State) repoStatus {
	return repoStatus{
		Name:      name,
		Path:      st.Path,
		Branch:    st.Branch,
		Detached:  st.Detached,
		Unborn:    st.Unborn,
		Upstream:  st.Upstream,
		HasSync:   st.HasUpstreamComparison,
		InSync:    st.HasUpstreamComparison && st.Ahead == 0 && st.Behind == 0,
		Ahead:     st.Ahead,
		Behind:    st.Behind,
		Staged:    st.Staged,
		Unstaged:  st.Unstaged,
		Untracked: st.Untracked,
		Dirty:     st.Dirty(),
	}
}

// writeStatusTable renders results as one line per repository: NAME,
// BRANCH, STATE and SYNC, aligned with a tabwriter rather than a fixed
// column width, so a long repository name or a narrow terminal shifts
// later columns instead of breaking the layout or truncating anything.
func writeStatusTable(w io.Writer, results []repoStatus) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tBRANCH\tSTATE\tSYNC")
	for _, r := range results {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Name, branchColumn(r), stateColumn(r), syncColumn(r))
	}
	// tabwriter.Flush can only fail when an earlier Write to w already
	// did, and every Write here is a Fprintf whose own error this
	// function already ignores by the same convention clone.go and
	// profile.go use for stdout and stderr writes throughout this
	// package.
	_ = tw.Flush()
}

// branchColumn renders the branch a repository is on, naming a detached
// HEAD explicitly rather than leaving the column blank.
func branchColumn(r repoStatus) string {
	if r.Detached {
		return "(detached)"
	}
	return r.Branch
}

// stateColumn renders a repository's dirty and unborn state as one short
// word, favouring "unborn", since nothing else about the tree is settled
// yet, over whatever dirty flags an empty working tree happens to carry.
func stateColumn(r repoStatus) string {
	if r.Error != "" {
		return "error"
	}
	if r.Unborn {
		return "unborn"
	}
	if r.Dirty {
		return "dirty"
	}
	return "clean"
}

// syncColumn renders how a repository compares to its upstream: "-" when
// that comparison cannot be made at all, an unborn HEAD or no configured
// upstream, never a blank cell that would silently read as "up to date".
func syncColumn(r repoStatus) string {
	if r.Error != "" {
		return "-"
	}
	if r.Unborn {
		return "-"
	}
	if r.Upstream == "" {
		return "no upstream"
	}
	if !r.HasSync {
		return "-"
	}
	if r.Ahead == 0 && r.Behind == 0 {
		return "up to date"
	}
	return fmt.Sprintf("+%d -%d", r.Ahead, r.Behind)
}
