// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/shell"
)

// pwshBinary is the real PowerShell 7 executable mise installed under
// this repository's pinned mise.toml, resolved once here, before any
// test's setupEnv can point HOME at a fresh temporary directory. mise's
// own shims, and "mise which" itself, both derive mise's data directory
// from HOME; invoking either one after HOME has already moved makes mise
// decide the pinned tool was never installed and try to fetch it again
// over a network this test has none of. Resolving the installed binary's
// own, real path up front, and calling that path directly from then on,
// never goes through mise, or its idea of HOME, again. Empty when "mise"
// itself is unavailable, or PowerShell is not installed.
var pwshBinary = resolvePwsh()

func resolvePwsh() string {
	cmd := exec.CommandContext(context.Background(), "mise", "which", "pwsh")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// TestShellWrapper_ExecutesInPowerShell runs the real "gitcd shell-init
// pwsh" snippet under a real PowerShell process, a real gitcd binary and
// a real, locally cloned repository. The string assertions in
// internal/shell/shell_test.go check the rendered text; this test checks
// what that text actually does once PowerShell runs it: the function
// lands in the session's scope, "clone" and "cd" actually "cd", a
// "--dry-run" clone actually does not, a failing command actually
// propagates its exit code without printing anything, a pass through
// command's output actually survives, and the "cd" completer actually
// fires for "cd" and not for an unrelated subcommand.
//
// It skips, rather than fails, when PowerShell is not available, since a
// contributor's machine may not have it even though it is pinned in
// mise.toml for CI.
func TestShellWrapper_ExecutesInPowerShell(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the real binary and runs it under a real PowerShell process")
	}
	if pwshBinary == "" {
		t.Skip(`pwsh is not installed; install it with "mise install powershell" (pinned in mise.toml), or from https://github.com/PowerShell/PowerShell`)
	}

	home := setupEnv(t)
	bin := buildGitcd(t)

	source := filepath.Join(home, "upstream")
	initRepository(t, source)
	const cloneURL = "https://example.test/acme/widget.git"
	// The same offline URL rewrite TestRunClone_ClonesAFreshTargetThroughARewrittenOfflineURL
	// uses: git itself rewrites it to the local source before ever
	// touching the network, so the clone this test drives through the
	// PowerShell wrapper never leaves the machine either.
	runGit(t, home, "config", "--global", "url.file://"+source+".insteadOf", cloneURL)

	scriptPath := filepath.Join(t.TempDir(), "init.ps1")
	writeFile(t, scriptPath, shell.PowerShell()+pwshHarness(cloneURL))

	cmd := exec.CommandContext(context.Background(), pwshBinary, "-NoLogo", "-NoProfile", "-NonInteractive", "-File", scriptPath)
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"PATH="+filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run the powershell wrapper: %v: %s", err, out)
	}
	got := parseMarkers(string(out))

	root := filepath.Join(home, "Code")
	wantClonePath := filepath.Join(root, "example.test", "acme", "widget")

	for _, want := range []struct {
		key   string
		value string
	}{
		{"FUNCTION_DEFINED", "True"},
		{"DRYRUN_CHANGED", "False"},
		{"DRYRUN_OUT", wantClonePath},
		{"CLONE_CWD", wantClonePath},
		{"CLONE_CHANGED", "True"},
		{"FAIL_EXIT", "2"},
		{"FAIL_OUT_EMPTY", "True"},
		{"FAIL_CHANGED", "False"},
		{"PASS_OUT", "dev"},
		{"CD_COMPLETIONS", "widget"},
		{"STATUS_COMPLETION_COUNT", "0"},
	} {
		if got[want.key] != want.value {
			t.Errorf("%s = %q, want %q (full powershell output:\n%s)", want.key, got[want.key], want.value, out)
		}
	}
}

// pwshHarness is PowerShell test glue run alongside the rendered
// snippet, not shipped code: unlike the snippet constants in
// internal/shell, it interpolates cloneURL, a constant this test itself
// controls rather than anything user supplied, to drive the function
// through every behaviour the acceptance criteria name. Each step prints
// a "KEY=value" marker parseMarkers reads back.
func pwshHarness(cloneURL string) string {
	return fmt.Sprintf(`
$cloneUrl = '%s'
$initial = (Get-Location).Path
"FUNCTION_DEFINED=$((Get-Command gitcd -CommandType Function -ErrorAction SilentlyContinue) -ne $null)"

# A dry run, while the target has never been created: must print the
# path and must not "cd".
$dryRunOut = gitcd clone --dry-run $cloneUrl 2>$null
"DRYRUN_CHANGED=$((Get-Location).Path -ne $initial)"
"DRYRUN_OUT=$dryRunOut"

# A real clone, creating the target for the first time: must "cd" into
# it.
gitcd clone $cloneUrl 2>$null | Out-Null
"CLONE_CWD=$((Get-Location).Path)"
"CLONE_CHANGED=$((Get-Location).Path -ne $initial)"
Set-Location -LiteralPath $initial

# "clone" with no argument is gitcd's own, deterministic, no network
# failure: must propagate the exit code and print nothing.
$failOut = gitcd clone 2>$null
"FAIL_EXIT=$LASTEXITCODE"
"FAIL_OUT_EMPTY=$([string]::IsNullOrEmpty($failOut))"
"FAIL_CHANGED=$((Get-Location).Path -ne $initial)"

# A pass through command: its output must still reach the caller.
$passOut = gitcd version
"PASS_OUT=$passOut"

# The "cd" completer must fire for "cd" and offer the repository just
# cloned by its short name, and must not fire for an unrelated
# subcommand.
$cdInput = 'gitcd cd wid'
$cdCompletion = [System.Management.Automation.CommandCompletion]::CompleteInput($cdInput, $cdInput.Length, $null)
"CD_COMPLETIONS=$($cdCompletion.CompletionMatches.CompletionText -join ',')"

$statusInput = 'gitcd status wid'
$statusCompletion = [System.Management.Automation.CommandCompletion]::CompleteInput($statusInput, $statusInput.Length, $null)
"STATUS_COMPLETION_COUNT=$($statusCompletion.CompletionMatches.Count)"
`, cloneURL)
}

// parseMarkers splits "KEY=value" lines in out into a map, ignoring every
// other line: gitcd's own stderr logging and PowerShell's own output
// share the same captured stream.
func parseMarkers(out string) map[string]string {
	got := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		got[key] = value
	}
	return got
}
