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

func TestRunCd_ResolvesAUniqueFuzzyMatch(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	dest := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, dest)
	addRemote(t, dest, "https://github.com/acme/widget.git")

	var stdout, stderr bytes.Buffer
	code := run([]string{"cd", "widg"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("cd widg = %d, want 0, stderr: %s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != dest {
		t.Fatalf("cd widg stdout = %q, want %q", got, dest)
	}
}

func TestRunCd_ExactNameWinsOverABroaderFuzzyMatch(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	// "widget" is an exact name match; "widget-extra"'s relative path
	// also contains the substring "widget" and would otherwise tie.
	exact := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, exact)
	addRemote(t, exact, "https://github.com/acme/widget.git")

	longer := filepath.Join(root, "github.com", "acme", "widget-extra")
	initRepository(t, longer)
	addRemote(t, longer, "https://github.com/acme/widget-extra.git")

	var stdout, stderr bytes.Buffer
	code := run([]string{"cd", "widget"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("cd widget = %d, want 0, stderr: %s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != exact {
		t.Fatalf("cd widget stdout = %q, want the exact name match %q", got, exact)
	}
}

func TestRunCd_IncludesALinkedWorktreeAsACandidate(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	main := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, main)
	addRemote(t, main, "https://github.com/acme/widget.git")
	worktree := filepath.Join(root, "github.com", "acme", "widget-feature-x")
	runGit(t, main, "worktree", "add", worktree)

	var stdout, stderr bytes.Buffer
	code := run([]string{"cd", "feature-x"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("cd feature-x = %d, want 0, stderr: %s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != worktree {
		t.Fatalf("cd feature-x stdout = %q, want %q: cd must reach a linked worktree even though list hides it by default", got, worktree)
	}
}

func TestRunCd_NoMatchFailsNamingTheQuery(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"cd", "nowhere"}, &stdout, &stderr)

	if code == 0 {
		t.Fatal("cd nowhere = 0, want a failure for a query that matches nothing")
	}
	if stdout.Len() != 0 {
		t.Fatalf("cd nowhere wrote to stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "nowhere") {
		t.Fatalf("cd nowhere stderr = %q, want it to name the query", stderr.String())
	}
}

func TestRunCd_AmbiguousQueryWithoutATerminalFailsListingEveryCandidate(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	first := filepath.Join(root, "github.com", "acme", "widget")
	second := filepath.Join(root, "gitlab.com", "acme", "widget")
	initRepository(t, first)
	addRemote(t, first, "https://github.com/acme/widget.git")
	initRepository(t, second)
	addRemote(t, second, "https://gitlab.com/acme/widget.git")

	var stdout, stderr bytes.Buffer
	// stdin carries a perfectly valid choice, "1", deliberately: with
	// isTerminal false, that choice must never be read at all, since a
	// script piping a plain file or command output into "gitcd cd" has no
	// business being treated as an interactive pick. If the terminal
	// guard were ever removed, this would read "1" and succeed instead
	// of failing, which is exactly what this test must catch.
	code := runCd([]string{"widget"}, strings.NewReader("1\n"), false, &stdout, &stderr)

	if code == 0 {
		t.Fatal("cd widget = 0, want a failure for an ambiguous query with no terminal")
	}
	if stdout.Len() != 0 {
		t.Fatalf("cd widget wrote to stdout: %q, want nothing: a non-terminal stdin must never be read for a pick", stdout.String())
	}
	if !strings.Contains(stderr.String(), first) || !strings.Contains(stderr.String(), second) {
		t.Fatalf("cd widget stderr = %q, want it to list both candidates", stderr.String())
	}
}

func TestRunCd_AmbiguousQueryWithATerminalReadsAPickFromStdin(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	first := filepath.Join(root, "github.com", "acme", "widget")
	second := filepath.Join(root, "gitlab.com", "acme", "widget")
	initRepository(t, first)
	addRemote(t, first, "https://github.com/acme/widget.git")
	initRepository(t, second)
	addRemote(t, second, "https://gitlab.com/acme/widget.git")

	var stdout, stderr bytes.Buffer
	// isTerminal is forced true here: the point under test is the picker
	// reading a numbered choice from whatever reader it is given, not
	// real terminal detection, which isTerminalFile already covers on its
	// own.
	code := runCd([]string{"widget"}, strings.NewReader("2\n"), true, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("cd widget = %d, want 0 once a valid pick is read, stderr: %s", code, stderr.String())
	}
	got := strings.TrimSpace(stdout.String())
	// The two candidates sort by their path relative to root ("github.com/..."
	// before "gitlab.com/..."), so choice "2" is the second, gitlab.com one.
	if got != second {
		t.Fatalf("cd widget stdout = %q, want the second listed candidate %q", got, second)
	}
	if !strings.Contains(stderr.String(), "1) "+first) || !strings.Contains(stderr.String(), "2) "+second) {
		t.Fatalf("cd widget stderr = %q, want a numbered picker listing both candidates", stderr.String())
	}
}

// TestRunCd_AmbiguousQueryWithATerminalButNoInputFailsWithAnHonestMessage
// covers the case isTerminalFile cannot tell apart: stdin redirected from
// /dev/null or /dev/zero is a character device, so isTerminalFile = true
// is correct by its own test, but it is not an interactive terminal, so no
// selection is ever read. Before the fix, the failure claimed "no
// selection was read" as if a real terminal had simply been given nothing,
// which is not what happened here: stdin never was a terminal to begin
// with. The message must say so is possible instead of asserting the
// wrong certainty.
func TestRunCd_AmbiguousQueryWithATerminalButNoInputFailsWithAnHonestMessage(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	first := filepath.Join(root, "github.com", "acme", "widget")
	second := filepath.Join(root, "gitlab.com", "acme", "widget")
	initRepository(t, first)
	addRemote(t, first, "https://github.com/acme/widget.git")
	initRepository(t, second)
	addRemote(t, second, "https://gitlab.com/acme/widget.git")

	var stdout, stderr bytes.Buffer
	// isTerminal is forced true, as isTerminalFile would wrongly report
	// for /dev/null or /dev/zero, while stdin itself carries no input at
	// all, exactly what reading from either of those produces.
	code := runCd([]string{"widget"}, strings.NewReader(""), true, &stdout, &stderr)

	if code == 0 {
		t.Fatal("cd widget = 0, want a failure when no selection can be read")
	}
	if stdout.Len() != 0 {
		t.Fatalf("cd widget wrote to stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "not actually an interactive terminal") {
		t.Fatalf("cd widget stderr = %q, want it to admit stdin may not actually be a terminal rather than assuming it is one", stderr.String())
	}
}

func TestRunCd_InvalidPickFails(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	first := filepath.Join(root, "github.com", "acme", "widget")
	second := filepath.Join(root, "gitlab.com", "acme", "widget")
	initRepository(t, first)
	addRemote(t, first, "https://github.com/acme/widget.git")
	initRepository(t, second)
	addRemote(t, second, "https://gitlab.com/acme/widget.git")

	var stdout, stderr bytes.Buffer
	code := runCd([]string{"widget"}, strings.NewReader("9\n"), true, &stdout, &stderr)

	if code == 0 {
		t.Fatal("cd widget = 0, want a failure for a choice out of range")
	}
	if stdout.Len() != 0 {
		t.Fatalf("cd widget wrote to stdout: %q", stdout.String())
	}
}

func TestRunCd_RequiresExactlyOneArgument(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"cd"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("cd with no argument = %d, want 2", code)
	}
}

// TestIsTerminalFile_APipeIsNotATerminal exercises the real terminal
// detection runCd wires to os.Stdin, rather than the isTerminal bool the
// other tests in this file inject directly. A pipe is the realistic shape
// of "gitcd cd" being piped into from a script or another command, the
// case the acceptance criterion means by "without a terminal". Detecting
// an actual terminal needs a real pty, which the standard library alone
// cannot allocate, so that side is covered indirectly: isTerminalFile
// returning false for the one kind of file descriptor this test can
// construct is the primary path this function has.
func TestIsTerminalFile_APipeIsNotATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create a pipe: %v", err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Fatalf("close the pipe's read end: %v", err)
		}
	}()
	defer func() {
		if err := w.Close(); err != nil {
			t.Fatalf("close the pipe's write end: %v", err)
		}
	}()

	if isTerminalFile(r) {
		t.Fatal("isTerminalFile(pipe) = true, want false")
	}
}
