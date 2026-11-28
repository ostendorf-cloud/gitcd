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
		{name: "pwsh returns the powershell snippet", shell: "pwsh", want: shell.PowerShell()},
		{name: "powershell returns the powershell snippet", shell: "powershell", want: shell.PowerShell()},
		{name: "an unsupported shell is rejected", shell: "cmd", wantErr: true},
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

	for _, name := range []string{"bash", "zsh", "fish", "pwsh"} {
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

// TestWrapper_BareCallRunsTheBinaryUnderStrictShellOptions sources the
// bash and zsh snippets in a non interactive shell with and without
// "set -u" or "setopt nounset", then calls the wrapper with no argument,
// "version" and "--version". A fake "gitcd" on PATH prints a usage for the
// bare call, so the test sees the wrapper hand it through unchanged.
// Without the guard, an unguarded "$1" aborts the shell under nounset.
func TestWrapper_BareCallRunsTheBinaryUnderStrictShellOptions(t *testing.T) {
	t.Parallel()

	fakeBin := t.TempDir()
	fakeScript := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"  '') printf 'Usage:\\n  gitcd <command>\\n' ;;\n" +
		"  version|--version) printf 'gitcd dev\\n' ;;\n" +
		"  *) exit 2 ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "gitcd"), []byte(fakeScript), 0o755); err != nil {
		t.Fatalf("write the fake gitcd: %v", err)
	}

	shells := []struct {
		name    string
		snippet string
		strict  string
	}{
		{name: "bash", snippet: shell.Bash(), strict: "set -u\n"},
		{name: "zsh", snippet: shell.Zsh(), strict: "setopt nounset\n"},
	}
	calls := []struct {
		name string
		call string
		want string
	}{
		{name: "a bare call", call: "gitcd", want: "Usage:\n  gitcd <command>\n"},
		{name: "version", call: "gitcd version", want: "gitcd dev\n"},
		{name: "the version flag", call: "gitcd --version", want: "gitcd dev\n"},
	}
	for _, sh := range shells {
		for _, strict := range []bool{false, true} {
			for _, tc := range calls {
				name := sh.name + "/" + tc.name
				if strict {
					name += "/nounset"
				}
				t.Run(name, func(t *testing.T) {
					t.Parallel()

					path, err := exec.LookPath(sh.name)
					if err != nil {
						t.Skipf("%s is not installed", sh.name)
					}
					dir := t.TempDir()
					file := filepath.Join(dir, "init.sh")
					if err := os.WriteFile(file, []byte(sh.snippet), 0o644); err != nil {
						t.Fatalf("write %s: %v", file, err)
					}
					script := ""
					if strict {
						script = sh.strict
					}
					// compdef only exists in a completion session.
					script += "compdef() { :; }\n. \"$1\"\n" + tc.call + "\n"

					cmd := exec.CommandContext(context.Background(), path, "-c", script, "--", file)
					cmd.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
					var stdout, stderr strings.Builder
					cmd.Stdout = &stdout
					cmd.Stderr = &stderr
					if err := cmd.Run(); err != nil {
						t.Fatalf("%s: %v, stderr %q", tc.call, err, stderr.String())
					}
					if stdout.String() != tc.want {
						t.Fatalf("%s stdout = %q, want %q", tc.call, stdout.String(), tc.want)
					}
					if stderr.Len() != 0 {
						t.Fatalf("%s wrote to stderr: %q", tc.call, stderr.String())
					}
				})
			}
		}
	}
}

// TestWrapper_WorktreeCommandChangesDirectory sources the bash and zsh
// snippets and calls "worktree" and its spelling "wt" against a fake
// "gitcd" that prints an existing directory. The wrapper must change into
// it. "worktree add --dry-run" prints a path it never created, so the
// wrapper must print that path instead of changing into it.
func TestWrapper_WorktreeCommandChangesDirectory(t *testing.T) {
	t.Parallel()

	target := t.TempDir()
	missing := filepath.Join(target, "never-created")
	fakeBin := t.TempDir()
	fakeScript := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  *--dry-run*) printf '%s\\n' \"$GITCD_MISSING\" ;;\n" +
		"  *) printf '%s\\n' \"$GITCD_TARGET\" ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "gitcd"), []byte(fakeScript), 0o755); err != nil {
		t.Fatalf("write the fake gitcd: %v", err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatalf("resolve %s: %v", target, err)
	}

	shells := []struct {
		name    string
		snippet string
	}{
		{name: "bash", snippet: shell.Bash()},
		{name: "zsh", snippet: shell.Zsh()},
	}
	calls := []struct {
		name string
		call string
		want string
	}{
		{name: "worktree", call: "gitcd worktree menu", want: resolvedTarget + "\n"},
		{name: "wt", call: "gitcd wt menu", want: resolvedTarget + "\n"},
		{name: "wt add", call: "gitcd wt add feat/x", want: resolvedTarget + "\n"},
		{name: "a dry run of a path never created", call: "gitcd wt add --dry-run feat/x", want: missing + "\n"},
	}
	for _, sh := range shells {
		for _, tc := range calls {
			t.Run(sh.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				path, err := exec.LookPath(sh.name)
				if err != nil {
					t.Skipf("%s is not installed", sh.name)
				}
				file := filepath.Join(t.TempDir(), "init.sh")
				if err := os.WriteFile(file, []byte(sh.snippet), 0o644); err != nil {
					t.Fatalf("write %s: %v", file, err)
				}
				// pwd -P prints the directory the shell ended up in.
				script := "compdef() { :; }\n. \"$1\"\ncd /\n" + tc.call + " >/dev/null\nwd=\"$(pwd -P)\"\n" +
					"if [ \"$wd\" = \"/\" ]; then printf '%s\\n' \"$(" + tc.call + ")\"; else printf '%s\\n' \"$wd\"; fi\n"

				cmd := exec.CommandContext(context.Background(), path, "-c", script, "--", file)
				cmd.Env = append(os.Environ(),
					"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
					"GITCD_TARGET="+target,
					"GITCD_MISSING="+missing,
				)
				var stdout, stderr strings.Builder
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				if err := cmd.Run(); err != nil {
					t.Fatalf("%s: %v, stderr %q", tc.call, err, stderr.String())
				}
				if stdout.String() != tc.want {
					t.Fatalf("%s ended in %q, want %q", tc.call, stdout.String(), tc.want)
				}
			})
		}
	}
}

// TestFish_GuardsABareCall checks the fish snippet by string match only,
// since fish is not installed here: an empty "$argv" expands to no word at
// all, so "switch $argv[1]" has to stay behind a guard that runs the
// binary for a bare call.
func TestFish_GuardsABareCall(t *testing.T) {
	t.Parallel()

	snippet := shell.Fish()
	guard := "if test (count $argv) -eq 0\n        command gitcd\n        return $status\n    end\n"
	guardAt := strings.Index(snippet, guard)
	if guardAt < 0 {
		t.Fatalf("fish snippet has no bare call guard: %s", snippet)
	}
	if switchAt := strings.Index(snippet, "switch $argv[1]"); switchAt < guardAt {
		t.Fatalf("fish snippet reads $argv[1] before the bare call guard: %s", snippet)
	}
}

// TestZsh_CdCompletionToleratesAShortCommandLineUnderNounset runs the
// completion function with only the command name typed, under
// "setopt nounset": "${words[2]}" must not abort the shell.
func TestZsh_CdCompletionToleratesAShortCommandLineUnderNounset(t *testing.T) {
	t.Parallel()

	zshPath, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed")
	}
	file := filepath.Join(t.TempDir(), "init.zsh")
	if err := os.WriteFile(file, []byte(shell.Zsh()), 0o644); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}
	script := `setopt nounset
compdef() { :; }
compadd() { :; }
source "$1"
words=(gitcd)
_gitcd
`
	cmd := exec.CommandContext(context.Background(), zshPath, "-c", script, "--", file)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run the zsh completion function: %v: %s", err, out)
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

// TestPowerShell_GuardsCdOnTheTargetActuallyExisting checks the
// PowerShell snippet's guard against a path "clone --dry-run" printed
// without ever creating by string match only: PowerShell is not
// installed in this environment and is not in the mise tool registry,
// so no real PowerShell process can execute it. "Test-Path ... -PathType
// Container" is the PowerShell equivalent of bash's "[ -d ... ]" and
// fish's "test -d ...".
func TestPowerShell_GuardsCdOnTheTargetActuallyExisting(t *testing.T) {
	t.Parallel()

	snippet := shell.PowerShell()
	for _, want := range []string{"Test-Path", "-PathType Container"} {
		if !strings.Contains(snippet, want) {
			t.Fatalf("powershell snippet does not guard cd on the target existing: %s", snippet)
		}
	}
}

// TestPowerShell_PropagatesNonZeroExit checks, by string match only, that
// the PowerShell snippet returns immediately on a non zero exit rather
// than printing the real binary's stdout or running anything else
// afterward that could change $LASTEXITCODE before the caller reads it.
// See the doc comment on the powerShell constant for why that ordering
// matters.
func TestPowerShell_PropagatesNonZeroExit(t *testing.T) {
	t.Parallel()

	snippet := shell.PowerShell()
	if !strings.Contains(snippet, "$LASTEXITCODE -ne 0") {
		t.Fatalf("powershell snippet does not check $LASTEXITCODE: %s", snippet)
	}
}

// TestPowerShell_RegistersCdCompletion checks the PowerShell completion
// block by string match only, for the same reason TestFish_RegistersCdCompletion
// does: no PowerShell process is available to run it against.
func TestPowerShell_RegistersCdCompletion(t *testing.T) {
	t.Parallel()

	snippet := shell.PowerShell()
	for _, want := range []string{
		"Register-ArgumentCompleter",
		"-Native",
		"-CommandName gitcd",
		"list --worktrees",
	} {
		if !strings.Contains(snippet, want) {
			t.Fatalf("powershell snippet does not contain %q: %s", want, snippet)
		}
	}
}

// TestInit_UnsupportedShellNamesEverySupportedShell checks that an
// unknown shell name fails with every shell gitcd actually supports
// named in the error, pwsh and powershell included, rather than only the
// three that existed before this one.
func TestInit_UnsupportedShellNamesEverySupportedShell(t *testing.T) {
	t.Parallel()

	_, err := shell.Init("cmd")
	if err == nil {
		t.Fatal("Init(\"cmd\") = nil, want an error")
	}
	for _, want := range []string{"bash", "zsh", "fish", "pwsh", "powershell"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Init(\"cmd\") error %q does not name %q", err, want)
		}
	}
}
