// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	skippedSummary = "repositories the scan could not inspect were skipped"
	skippedDetail  = "skipped a repository scan could not inspect"
)

// skippedTree builds a root with one inspectable repository and three a
// scan cannot inspect: a checkout with no origin remote, a directory whose
// .git file points nowhere, and a linked worktree whose directory is gone.
// It returns the root and the three skipped paths.
func skippedTree(t *testing.T, home string) (root string, skipped []string) {
	t.Helper()
	root = filepath.Join(home, "Code")

	widget := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, widget)
	addRemote(t, widget, "https://github.com/acme/widget.git")

	stale := filepath.Join(root, "github.com", "acme", "widget-feature")
	runGit(t, widget, "worktree", "add", stale, "-b", "feature")
	if err := os.RemoveAll(stale); err != nil {
		t.Fatalf("remove %s: %v", stale, err)
	}

	noOrigin := filepath.Join(root, "local", "scratch")
	initRepository(t, noOrigin)

	broken := filepath.Join(root, "store", "tmp-1a2b")
	writeFile(t, filepath.Join(broken, ".git"), "gitdir: "+filepath.Join(home, "missing", "gitdir")+"\n")

	return root, []string{stale, noOrigin, broken}
}

func logLines(stderr string) []string {
	var lines []string
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, "time=") {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestRunScan_ReportsSkippedRepositoriesOnce(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantSummary bool
	}{
		{name: "list", args: []string{"list"}, wantSummary: true},
		{name: "doctor", args: []string{"doctor"}, wantSummary: true},
		{name: "status does not skip", args: []string{"status"}, wantSummary: false},
	}
	for _, tt := range tests {
		t.Run(tt.name+" prints one summary line", func(t *testing.T) {
			home := setupEnv(t)
			skippedTree(t, home)

			var stdout, stderr bytes.Buffer
			run(tt.args, &stdout, &stderr)

			lines := logLines(stderr.String())
			wantLines := 0
			if tt.wantSummary {
				wantLines = 1
			}
			if len(lines) != wantLines {
				t.Fatalf("%v logged %d lines, want %d, stderr: %s", tt.args, len(lines), wantLines, stderr.String())
			}
			if tt.wantSummary {
				if !strings.Contains(lines[0], "level=WARN") || !strings.Contains(lines[0], skippedSummary) ||
					!strings.Contains(lines[0], "count=3") || !strings.Contains(lines[0], "--verbose") {
					t.Errorf("%v summary = %q, want a warning with the count and a pointer to --verbose", tt.args, lines[0])
				}
				if strings.Contains(lines[0], "error=") {
					t.Errorf("%v summary carries a reason: %q", tt.args, lines[0])
				}
			}
		})

		t.Run(tt.name+" with --verbose names every skipped repository", func(t *testing.T) {
			home := setupEnv(t)
			_, skipped := skippedTree(t, home)

			var stdout, stderr bytes.Buffer
			run(append(append([]string{}, tt.args...), "--verbose"), &stdout, &stderr)

			details := 0
			summaries := 0
			for _, line := range logLines(stderr.String()) {
				switch {
				case strings.Contains(line, skippedDetail):
					details++
					if !strings.Contains(line, "level=DEBUG") || !strings.Contains(line, "error=") {
						t.Errorf("detail line = %q, want a debug line with a reason", line)
					}
				case strings.Contains(line, skippedSummary):
					summaries++
				}
			}
			if !tt.wantSummary {
				if details != 0 || summaries != 0 {
					t.Fatalf("%v logged %d detail and %d summary lines, want none", tt.args, details, summaries)
				}
				return
			}
			if details != len(skipped) || summaries != 1 {
				t.Fatalf("%v logged %d detail and %d summary lines, want %d and 1, stderr: %s",
					tt.args, details, summaries, len(skipped), stderr.String())
			}
			for _, path := range skipped {
				if got := strings.Count(stderr.String(), "path="+path+" "); got != 1 {
					t.Errorf("%v named %s %d times, want once, stderr: %s", tt.args, path, got, stderr.String())
				}
			}
		})
	}
}

func TestRunScan_PrintsNothingWhenNothingIsSkipped(t *testing.T) {
	for _, args := range [][]string{{"list"}, {"doctor"}, {"status"}} {
		t.Run(args[0], func(t *testing.T) {
			home := setupEnv(t)
			widget := filepath.Join(home, "Code", "github.com", "acme", "widget")
			initRepository(t, widget)
			addRemote(t, widget, "https://github.com/acme/widget.git")

			var stdout, stderr bytes.Buffer
			run(args, &stdout, &stderr)

			if got := logLines(stderr.String()); len(got) != 0 {
				t.Fatalf("%v logged %v, want no log line", args, got)
			}
		})
	}
}

func TestRunDoctor_FixReportsSkippedRepositoriesOnce(t *testing.T) {
	for _, verbose := range []bool{false, true} {
		name := "without --verbose"
		if verbose {
			name = "with --verbose"
		}
		t.Run(name, func(t *testing.T) {
			home := setupEnv(t)
			root, _ := skippedTree(t, home)
			writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+`
profiles:
  work:
    user.email: jane@acme.example
rules:
  - host: github.com
    profile: work
`)

			args := []string{"doctor", "--fix"}
			if verbose {
				args = append(args, "--verbose")
			}
			var stdout, stderr bytes.Buffer
			run(args, &stdout, &stderr)
			if !strings.Contains(stdout.String(), "fixed [managed-file-missing]") {
				t.Fatalf("doctor --fix stdout = %q, want a repaired managed file so a second pass runs", stdout.String())
			}

			if got := strings.Count(stderr.String(), skippedSummary); got != 1 {
				t.Errorf("doctor --fix logged the summary %d times, want once, stderr: %s", got, stderr.String())
			}
			wantDetails := 0
			if verbose {
				wantDetails = 3
			}
			if got := strings.Count(stderr.String(), skippedDetail); got != wantDetails {
				t.Errorf("doctor --fix logged %d detail lines, want %d, stderr: %s", got, wantDetails, stderr.String())
			}
		})
	}
}

// A query that names a skipped repository still resolves it, and a query
// that names nothing still fails with the existing miss message.
func TestRunPath_StillResolvesASkippedRepository(t *testing.T) {
	home := setupEnv(t)
	_, skipped := skippedTree(t, home)

	var stdout, stderr bytes.Buffer
	code := run([]string{"path", "scratch"}, &stdout, &stderr)
	if code != 0 || strings.TrimSpace(stdout.String()) != skipped[1] {
		t.Fatalf("path scratch = %d, stdout %q, want %s, stderr: %s", code, stdout.String(), skipped[1], stderr.String())
	}
	if got := logLines(stderr.String()); len(got) != 0 {
		t.Errorf("path scratch logged %v, want no log line", got)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"path", "nothing-like-it"}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), `no repository named "nothing-like-it"`) {
		t.Fatalf("path nothing-like-it = %d, stderr: %s, want the miss message", code, stderr.String())
	}
}
