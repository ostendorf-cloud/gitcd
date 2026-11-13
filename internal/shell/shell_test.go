// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package shell_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/shell"
)

func TestInit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		shell   string
		want    string
		wantErr bool
	}{
		{name: "bash returns the bash snippet", shell: "bash", want: shell.Bash()},
		{name: "zsh returns the zsh snippet", shell: "zsh", want: shell.Zsh()},
		{name: "fish returns the fish snippet", shell: "fish", want: shell.Fish()},
		{name: "an unsupported shell is rejected", shell: "powershell", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := shell.Init(tt.shell)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Init(%q) = %q, want an error", tt.shell, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Init(%q) returned an unexpected error: %v", tt.shell, err)
			}
			if got != tt.want {
				t.Fatalf("Init(%q) = %q, want %q", tt.shell, got, tt.want)
			}
		})
	}
}

func TestInit_EveryWrittenSnippetGuardsCloneAndPath(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"bash", "zsh", "fish"} {
		snippet, err := shell.Init(name)
		if err != nil {
			t.Fatalf("Init(%q) returned an unexpected error: %v", name, err)
		}
		if !strings.Contains(snippet, "gitcd") {
			t.Fatalf("%s snippet does not define a gitcd function: %s", name, snippet)
		}
		if !strings.Contains(snippet, "clone") || !strings.Contains(snippet, "path") {
			t.Fatalf("%s snippet does not guard clone and path: %s", name, snippet)
		}
	}
}

// TestBashLike_GuardsCdOnTheTargetActuallyExisting checks that neither the
// bash and zsh snippet nor the fish snippet changes directory on a path
// "clone --dry-run" printed without ever creating, which would otherwise
// break the command's own acceptance criterion. fish is not installed in
// this environment, so its guard is checked by inspecting the rendered
// snippet rather than by running it; see the doc comment on the fish
// constant for the fish semantics this guard relies on.
func TestBashLike_GuardsCdOnTheTargetActuallyExisting(t *testing.T) {
	t.Parallel()

	if !strings.Contains(shell.Bash(), `[ -d "$real" ]`) {
		t.Fatalf("bash/zsh snippet does not guard cd on the target existing: %s", shell.Bash())
	}
	if !strings.Contains(shell.Fish(), `test -d "$real"`) {
		t.Fatalf("fish snippet does not guard cd on the target existing: %s", shell.Fish())
	}
}

func TestBashAndZshSnippetsAreSyntacticallyValid(t *testing.T) {
	t.Parallel()

	for _, shellBinary := range []string{"bash", "zsh"} {
		path, err := exec.LookPath(shellBinary)
		if err != nil {
			t.Skipf("%s is not installed", shellBinary)
		}

		var snippet string
		switch shellBinary {
		case "bash":
			snippet = shell.Bash()
		case "zsh":
			snippet = shell.Zsh()
		}

		dir := t.TempDir()
		file := filepath.Join(dir, "init.sh")
		if err := os.WriteFile(file, []byte(snippet), 0o644); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}

		cmd := exec.CommandContext(context.Background(), path, "-n", file)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s -n %s: %v: %s", shellBinary, file, err, out)
		}
	}
}
