// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"strings"
	"testing"
)

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
