// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/shell"
)

func TestRunShellInit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		shellID string
		want    string
	}{
		{name: "zsh", shellID: "zsh", want: shell.Zsh()},
		{name: "bash", shellID: "bash", want: shell.Bash()},
		{name: "fish", shellID: "fish", want: shell.Fish()},
		{name: "pwsh", shellID: "pwsh", want: shell.PowerShell()},
		{name: "powershell", shellID: "powershell", want: shell.PowerShell()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			code := run([]string{"shell-init", tt.shellID}, &stdout, &stderr)

			if code != 0 {
				t.Fatalf("shell-init %s = %d, want 0, stderr: %s", tt.shellID, code, stderr.String())
			}
			if stdout.String() != tt.want {
				t.Fatalf("shell-init %s stdout = %q, want %q", tt.shellID, stdout.String(), tt.want)
			}
		})
	}
}

func TestRunShellInit_UnsupportedShellFails(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := run([]string{"shell-init", "cmd"}, &stdout, &stderr)

	if code == 0 {
		t.Fatal("shell-init cmd = 0, want a failure")
	}
	if stdout.Len() != 0 {
		t.Fatalf("shell-init cmd wrote to stdout: %q", stdout.String())
	}
}

func TestRunShellInit_RequiresExactlyOneArgument(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := run([]string{"shell-init"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("shell-init with no argument = %d, want 2", code)
	}
}
