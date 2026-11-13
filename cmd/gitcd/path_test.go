// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPath_ResolvesAnExactRepositoryName(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	dest := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, dest)

	var stdout, stderr bytes.Buffer
	code := run([]string{"path", "widget"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("path widget = %d, want 0, stderr: %s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != dest {
		t.Fatalf("path widget stdout = %q, want %q", got, dest)
	}
}

func TestRunPath_NoMatchFails(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"path", "nowhere"}, &stdout, &stderr)

	if code == 0 {
		t.Fatal("path nowhere = 0, want a failure for a name that matches nothing")
	}
	if stdout.Len() != 0 {
		t.Fatalf("path nowhere wrote to stdout: %q", stdout.String())
	}
}

func TestRunPath_AmbiguousNameFailsListingEveryCandidate(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	first := filepath.Join(root, "github.com", "acme", "widget")
	second := filepath.Join(root, "gitlab.com", "acme", "widget")
	initRepository(t, first)
	initRepository(t, second)

	var stdout, stderr bytes.Buffer
	code := run([]string{"path", "widget"}, &stdout, &stderr)

	if code == 0 {
		t.Fatal("path widget = 0, want a failure for an ambiguous name")
	}
	if !strings.Contains(stderr.String(), first) || !strings.Contains(stderr.String(), second) {
		t.Fatalf("path widget stderr = %q, want it to list both candidates", stderr.String())
	}
}

func TestRunPath_ResolvesAHostOwnerRepoShorthandAlreadyCloned(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	dest := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, dest)

	var stdout, stderr bytes.Buffer
	code := run([]string{"path", "github.com/acme/widget"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("path github.com/acme/widget = %d, want 0, stderr: %s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != dest {
		t.Fatalf("path github.com/acme/widget stdout = %q, want %q", got, dest)
	}
}

func TestRunPath_HostOwnerRepoShorthandNotClonedYetFails(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"path", "github.com/acme/widget"}, &stdout, &stderr)

	if code == 0 {
		t.Fatal("path github.com/acme/widget = 0, want a failure for a repository never cloned")
	}
}

func TestRunPath_RequiresExactlyOneArgument(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"path"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("path with no argument = %d, want 2", code)
	}
}
