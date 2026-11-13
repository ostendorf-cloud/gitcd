// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun_Version(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := run([]string{"version"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run([version]) = %d, want 0", code)
	}
	if got := strings.TrimSpace(stdout.String()); got != Version {
		t.Fatalf("run([version]) wrote %q on stdout, want %q", got, Version)
	}
	if stderr.Len() != 0 {
		t.Fatalf("run([version]) wrote to stderr: %q", stderr.String())
	}
}

func TestRun_NoArguments(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("run(nil) = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("run(nil) wrote to stdout, want the usage on stderr only: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("run(nil) stderr = %q, want it to contain the usage", stderr.String())
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := run([]string{"frobnicate"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("run([frobnicate]) = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "frobnicate") {
		t.Fatalf("run([frobnicate]) stderr = %q, want it to name the unknown command", stderr.String())
	}
}

func TestRun_Help(t *testing.T) {
	t.Parallel()

	for _, flag := range []string{"help", "-h", "--help"} {
		var stdout, stderr bytes.Buffer
		code := run([]string{flag}, &stdout, &stderr)

		if code != 0 {
			t.Fatalf("run([%s]) = %d, want 0", flag, code)
		}
		if !strings.Contains(stdout.String(), "Usage:") {
			t.Fatalf("run([%s]) stdout = %q, want it to contain the usage", flag, stdout.String())
		}
	}
}
