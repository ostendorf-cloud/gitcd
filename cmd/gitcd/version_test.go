// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun_VersionSpellings(t *testing.T) {
	t.Parallel()

	var want, wantErr bytes.Buffer
	if code := run([]string{"version"}, &want, &wantErr); code != 0 || wantErr.Len() != 0 || want.Len() == 0 {
		t.Fatalf("run(version) = %d, stdout %q, stderr %q, want 0, a line, nothing", code, want.String(), wantErr.String())
	}

	tests := []struct {
		name     string
		args     []string
		wantCode int
		// wantInStderr is empty for a spelling that succeeds.
		wantInStderr string
	}{
		{name: "the version command", args: []string{"version"}, wantCode: 0},
		{name: "the double dash flag", args: []string{"--version"}, wantCode: 0},
		{name: "the single dash flag", args: []string{"-version"}, wantCode: 0},
		{name: "the flag with an extra argument", args: []string{"--version", "extra"}, wantCode: 2, wantInStderr: "gitcd version: no arguments are accepted"},
		{name: "the command with an extra argument", args: []string{"version", "extra"}, wantCode: 2, wantInStderr: "gitcd version: no arguments are accepted"},
		{name: "the flag followed by another flag", args: []string{"--version", "--verbose"}, wantCode: 2, wantInStderr: "-verbose"},
		{name: "the flag after a command", args: []string{"clone", "--version"}, wantCode: 2, wantInStderr: "-version"},
		{name: "the flag after a global style flag", args: []string{"--verbose", "--version"}, wantCode: 2, wantInStderr: "unknown command"},
		{name: "an upper case short flag", args: []string{"-V"}, wantCode: 2, wantInStderr: "unknown command"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)

			if code != tt.wantCode {
				t.Fatalf("run(%q) = %d, want %d", tt.args, code, tt.wantCode)
			}
			if tt.wantCode == 0 {
				if stdout.String() != want.String() {
					t.Fatalf("run(%q) stdout = %q, want %q", tt.args, stdout.String(), want.String())
				}
				if stderr.Len() != 0 {
					t.Fatalf("run(%q) wrote to stderr: %q", tt.args, stderr.String())
				}
				return
			}
			if stdout.Len() != 0 {
				t.Fatalf("run(%q) wrote to stdout: %q", tt.args, stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.wantInStderr) {
				t.Fatalf("run(%q) stderr = %q, want it to contain %q", tt.args, stderr.String(), tt.wantInStderr)
			}
		})
	}
}

func TestRun_UsageNamesVersionFlag(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if code := run([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run(--help) = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "--version") {
		t.Fatalf("usage = %q, want it to name --version", stdout.String())
	}
}

func TestRunVersion_RejectsArgumentsAndUnknownFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		args         []string
		wantInStderr string
	}{
		{name: "an extra argument", args: []string{"version", "extra"}, wantInStderr: "Usage of gitcd version:"},
		{name: "several extra arguments", args: []string{"version", "one", "two"}, wantInStderr: "Usage of gitcd version:"},
		{name: "an unknown flag", args: []string{"version", "--bogus"}, wantInStderr: "-bogus"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)

			if code != 2 {
				t.Fatalf("run(%q) = %d, want 2", tt.args, code)
			}
			if stdout.Len() != 0 {
				t.Fatalf("run(%q) wrote to stdout: %q", tt.args, stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.wantInStderr) {
				t.Fatalf("run(%q) stderr = %q, want it to contain %q", tt.args, stderr.String(), tt.wantInStderr)
			}
		})
	}
}
