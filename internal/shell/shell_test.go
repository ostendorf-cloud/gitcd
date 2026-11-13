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

// TestZsh_CdCompletionOffersEveryRepositoryByItsShortName runs the real
// zsh completion function the Zsh snippet defines, with "compadd" and
// "compdef" stubbed out since neither is available outside a terminal
// completion session, and a fake "gitcd" on PATH standing in for the real
// binary "gitcd list --worktrees" shells out to. It is a behavioural test,
// not a string match: it would still pass if the function's body were
// replaced by anything else that called compadd with the right names, and
// would fail if "_gitcd" stopped reducing each path to its final segment,
// or stopped going through "gitcd list --worktrees" at all.
func TestZsh_CdCompletionOffersEveryRepositoryByItsShortName(t *testing.T) {
	t.Parallel()

	zshPath, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed")
	}

	dir := t.TempDir()
	snippetFile := filepath.Join(dir, "init.zsh")
	if err := os.WriteFile(snippetFile, []byte(shell.Zsh()), 0o644); err != nil {
		t.Fatalf("write %s: %v", snippetFile, err)
	}

	fakeBin := t.TempDir()
	fakeGitcd := filepath.Join(fakeBin, "gitcd")
	fakeScript := "#!/bin/sh\n" +
		"if [ \"$1\" = list ] && [ \"$2\" = --worktrees ]; then\n" +
		"  printf '%s\\n' /code/github.com/acme/widget /code/gitlab.com/acme/other\n" +
		"fi\n"
	if err := os.WriteFile(fakeGitcd, []byte(fakeScript), 0o755); err != nil {
		t.Fatalf("write %s: %v", fakeGitcd, err)
	}

	script := `compdef() { :; }
compadd() { for a in "$@"; do [ "$a" = "--" ] && continue; print -r -- "$a"; done }
source "$1"
words=(gitcd cd)
_gitcd
`
	cmd := exec.CommandContext(context.Background(), zshPath, "-c", script, "--", snippetFile)
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run the zsh completion function: %v: %s", err, out)
	}

	got := strings.Fields(strings.TrimSpace(string(out)))
	want := []string{"widget", "other"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("_gitcd compadd arguments = %v, want %v", got, want)
	}
}

// TestFish_RegistersCdCompletion checks the fish completion block by
// string match only: fish is not installed in this environment and is not
// in the mise tool registry, so no real fish process can execute it.
func TestFish_RegistersCdCompletion(t *testing.T) {
	t.Parallel()

	snippet := shell.Fish()
	for _, want := range []string{
		"complete -c gitcd",
		"__fish_seen_subcommand_from cd",
		"gitcd list --worktrees",
	} {
		if !strings.Contains(snippet, want) {
			t.Fatalf("fish snippet does not contain %q: %s", want, snippet)
		}
	}
}
