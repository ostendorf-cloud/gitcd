// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
)

func TestRunDoctor_NoArgumentsAccepted(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "extra"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("doctor with an argument = %d, want 2", code)
	}
}

func TestRunDoctor_ReportsAMissingManagedFileAndExitsNonZero(t *testing.T) {
	home := setupEnv(t)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  work:
    user.email: acme-work-identity
rules:
  - host: github.com
    profile: work
`)

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("doctor = %d, want 1, stdout: %s stderr: %s", code, stdout.String(), stderr.String())
	}

	out := stdout.String()
	includePath := filepath.Join(home, ".config", "gitcd", "includes.gitconfig")
	if !strings.Contains(out, includePath) {
		t.Fatalf("doctor stdout = %q, want it to name the missing include file %s", out, includePath)
	}
	cfgPath := filepath.Join(home, ".gitcd.yaml")
	if !strings.Contains(out, cfgPath) {
		t.Fatalf("doctor stdout = %q, want it to print the config file path %s", out, cfgPath)
	}
	managedDir := filepath.Join(home, ".config", "gitcd")
	if !strings.Contains(out, managedDir) {
		t.Fatalf("doctor stdout = %q, want it to print the managed directory %s", out, managedDir)
	}
}

// TestRunDoctor_ExcludesALinkedWorktreeFromPerRepositoryChecks guards the
// worktree visibility rule doctor settles on: a linked worktree shares
// its main checkout's remote and local git configuration, so checking it
// again would only repeat the same identity checks, and its own path,
// nested at "<repo>/.claude/worktrees/<branch>", is never one the
// configured layout would render, so checking its path at all would
// report a bogus mismatch for a location a worktree is never expected to
// sit at. Probing confirmed the premise directly: a "gitdir" rule
// matches a linked worktree exactly the same as its main checkout, since
// the worktree's own git directory always nests under the main
// checkout's own path, so no identity check could ever tell the two
// apart in the first place.
func TestRunDoctor_ExcludesALinkedWorktreeFromPerRepositoryChecks(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	main := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, main)
	runGit(t, main, "remote", "add", "origin", "https://github.com/acme/widget.git")
	worktree := filepath.Join(main, ".claude", "worktrees", "feat-x")
	runGit(t, main, "worktree", "add", worktree, "-b", "feat-x")

	var stdout, stderr bytes.Buffer
	run([]string{"doctor"}, &stdout, &stderr)
	out := stdout.String()
	if strings.Contains(out, worktree) {
		t.Fatalf(
			"doctor stdout = %q, want no finding naming the linked worktree %s: it must never be checked on its own",
			out, worktree,
		)
	}
}

func TestRunDoctor_NoRepositoriesAndNoManagedFilesNeededIsStillNonZeroOnAGlobalIdentityKey(t *testing.T) {
	// setupEnv itself sets a global user.name and user.email, exactly
	// the finding doctor exists to report: with no profiles configured
	// at all, there is nothing to render, so the only finding left is
	// the global identity key setupEnv's own baseline carries.
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("doctor = %d, want 1 for a global identity key finding, stdout: %s stderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "global-identity-key") {
		t.Fatalf("doctor stdout = %q, want a global-identity-key finding", stdout.String())
	}
}

// TestRunDoctor_GlobalKeyPositionRelativeToTheGitcdInclude runs the whole
// command against a global file whose key sits on one side of the gitcd
// include while a foreign include follows it: the command must pass the
// include file's path on, or it only sees the foreign include as the line
// a key has to precede.
func TestRunDoctor_GlobalKeyPositionRelativeToTheGitcdInclude(t *testing.T) {
	tests := []struct {
		name       string
		global     string
		wantOrder  bool
		wantPhrase string
		notPhrase  string
	}{
		{
			name:       "a key between the gitcd include and a foreign include",
			global:     "{include}[user]\n\temail = jane@global.example.test\n[include]\n\tpath = {foreign}\n",
			wantOrder:  true,
			wantPhrase: "It sits after an include section, so it wins over every profile",
			notPhrase:  "overrides it when the gitcd include comes after it",
		},
		{
			name:       "a key before the gitcd include and a foreign include after it",
			global:     "[user]\n\temail = jane@global.example.test\n{include}[include]\n\tpath = {foreign}\n",
			wantOrder:  false,
			wantPhrase: "overrides it when the gitcd include comes after it",
			notPhrase:  "It sits after an include section",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := setupEnv(t)
			expand := strings.NewReplacer(
				"{include}", "[include]\n\tpath = "+gitConfigEscaped(gitconfig.IncludePath(home))+"\n",
				"{foreign}", filepath.ToSlash(filepath.Join(home, "other.gitconfig")),
			)
			writeFile(t, globalConfigPath(home), expand.Replace(tt.global))

			var stdout, stderr bytes.Buffer
			if code := run([]string{"doctor"}, &stdout, &stderr); code != 1 {
				t.Fatalf("doctor = %d, want 1, stdout: %s stderr: %s", code, stdout.String(), stderr.String())
			}
			out := stdout.String()
			if got := strings.Contains(out, "global-identity-order"); got != tt.wantOrder {
				t.Errorf("doctor stdout = %q, want a global-identity-order finding = %v", out, tt.wantOrder)
			}
			if !strings.Contains(out, tt.wantPhrase) {
				t.Errorf("doctor stdout = %q, want it to contain %q", out, tt.wantPhrase)
			}
			if strings.Contains(out, tt.notPhrase) {
				t.Errorf("doctor stdout = %q, want it not to contain %q", out, tt.notPhrase)
			}
		})
	}
}

// TestRunDoctor_GlobalIdentityHintMatchesWhatInitDoes runs doctor and then
// "init --move-identity=yes" in the same home and compares them: the
// target the doctor hint offers is accepted by init exactly when the hint
// offers it, and a target the hint does not claim is never named as one.
// setupEnv's own baseline sets a global user.name and user.email, the
// finding whose wording is under test.
func TestRunDoctor_GlobalIdentityHintMatchesWhatInitDoes(t *testing.T) {
	const (
		offersDefault  = `moves it into the new profile "global"`
		refusesDefault = `refuses "global"`
	)
	tests := []struct {
		name string
		yaml string
		// initArgs are added to --move-identity=yes. An explicit --profile
		// is a choice doctor cannot know, so its rows only check that the
		// hint does not claim the profile init refuses.
		initArgs    []string
		wantAccept  bool
		wantOffers  bool
		wantRefuses bool
		// wantStderr names the conflict init reports when it refuses.
		wantStderr string
		// notNamed is a profile name the hint must not offer.
		notNamed string
	}{
		{
			name:       "no defaultProfile and no profiles",
			yaml:       "",
			wantAccept: true,
			wantOffers: true,
		},
		{
			name:       "a defaultProfile other than global",
			yaml:       "profiles:\n  private:\n    user.email: jane@private.example\ndefaultProfile: private\n",
			wantAccept: true,
			wantOffers: true,
			notNamed:   "private",
		},
		{
			name:        "global is the defaultProfile",
			yaml:        "profiles:\n  global:\n    user.email: jane@private.example\ndefaultProfile: global\n",
			wantRefuses: true,
			wantStderr:  "defaultProfile",
		},
		{
			name:        "global is an existing profile and no defaultProfile",
			yaml:        "profiles:\n  global:\n    user.email: jane@private.example\n",
			wantRefuses: true,
			wantStderr:  "already exists",
		},
		{
			name:        "a profile that differs from global only in case",
			yaml:        "profiles:\n  Global:\n    user.email: jane@private.example\n",
			wantRefuses: true,
			wantStderr:  "only in case",
		},
		{
			name:       "an explicit unused profile with a defaultProfile",
			yaml:       "profiles:\n  private:\n    user.email: jane@private.example\ndefaultProfile: private\n",
			initArgs:   []string{"--profile=personal"},
			wantAccept: true,
			wantOffers: true,
		},
		{
			name:       "an explicit profile naming the defaultProfile",
			yaml:       "profiles:\n  private:\n    user.email: jane@private.example\ndefaultProfile: private\n",
			initArgs:   []string{"--profile=private"},
			wantOffers: true,
			wantStderr: "defaultProfile",
			notNamed:   "private",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := setupEnv(t)
			cfgPath := filepath.Join(home, ".gitcd.yaml")
			if tt.yaml != "" {
				writeFile(t, cfgPath, tt.yaml)
			}

			var out, errOut bytes.Buffer
			if code := run([]string{"doctor"}, &out, &errOut); code != 1 {
				t.Fatalf("doctor = %d, want 1, stdout: %s stderr: %s", code, out.String(), errOut.String())
			}
			hint := out.String()
			if got := strings.Contains(hint, offersDefault); got != tt.wantOffers {
				t.Errorf("doctor offers the default target = %v, want %v, stdout: %s", got, tt.wantOffers, hint)
			}
			if got := strings.Contains(hint, refusesDefault); got != tt.wantRefuses {
				t.Errorf("doctor says init refuses the default target = %v, want %v, stdout: %s", got, tt.wantRefuses, hint)
			}
			if !strings.Contains(hint, "--profile") {
				t.Errorf("doctor stdout = %q, want it to name --profile", hint)
			}
			if tt.notNamed != "" && strings.Contains(hint, fmt.Sprintf("%q", tt.notNamed)) {
				t.Errorf("doctor stdout = %q, want it not to name profile %q at all", hint, tt.notNamed)
			}
			globalBefore, err := os.ReadFile(globalConfigPath(home))
			if err != nil {
				t.Fatalf("read the global git configuration: %v", err)
			}

			var initOut, initErr bytes.Buffer
			args := append([]string{"--move-identity=yes"}, tt.initArgs...)
			code := runInit(args, strings.NewReader(""), &initOut, &initErr)
			global, err := os.ReadFile(globalConfigPath(home))
			if err != nil {
				t.Fatalf("read the global git configuration: %v", err)
			}
			if tt.wantAccept {
				if code != 0 {
					t.Fatalf("init %v = %d, want it to accept the target the hint names, stderr: %s", args, code, initErr.String())
				}
				for _, line := range strings.Split(string(global), "\n") {
					if strings.HasPrefix(strings.TrimSpace(line), "email") {
						t.Errorf("global git configuration = %q, want the moved key gone", global)
					}
				}
				return
			}
			if code != 1 {
				t.Fatalf("init %v = %d, want a refusal, stdout: %s stderr: %s", args, code, initOut.String(), initErr.String())
			}
			if !strings.Contains(initErr.String(), tt.wantStderr) {
				t.Errorf("init stderr = %q, want it to name %q", initErr.String(), tt.wantStderr)
			}
			if !bytes.Equal(global, globalBefore) {
				t.Errorf("global git configuration = %q, want it unchanged after a refusal, was %q", global, globalBefore)
			}
			backups, err := filepath.Glob(filepath.Join(home, "*.gitcd-backup-*"))
			if err != nil {
				t.Fatalf("glob backups: %v", err)
			}
			if len(backups) != 0 {
				t.Errorf("init wrote backups %v before refusing, want none", backups)
			}
		})
	}
}

// TestRunDoctor_FixDoesNotPrintAResolvedIdentityDriftAsOpen has a
// repository whose profile email only reaches it once the missing include
// file is rendered. "--fix" renders it, so the drift it caused is gone and
// must not be listed after the fix, while a drift the render cannot
// resolve, a global key after the include, still is, with the exit status
// to match.
func TestRunDoctor_FixDoesNotPrintAResolvedIdentityDriftAsOpen(t *testing.T) {
	tests := []struct {
		name string
		// global is the global git configuration, {include} the gitcd
		// include section.
		global    string
		wantDrift bool
		wantCode  int
	}{
		{
			name:      "no global identity key",
			global:    "{include}",
			wantDrift: false,
			wantCode:  0,
		},
		{
			name:      "a global key before the include",
			global:    "[user]\n\temail = jane@global.example\n{include}",
			wantDrift: false,
			wantCode:  1,
		},
		{
			name:      "a global key after the include",
			global:    "{include}[user]\n\temail = jane@global.example\n",
			wantDrift: true,
			wantCode:  1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := setupEnv(t)
			root := filepath.Join(home, "Code")
			writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+`
profiles:
  work:
    user.email: jane@acme.example
rules:
  - host: github.com
    profile: work
`)
			repo := filepath.Join(root, "github.com", "acme", "widget")
			initRepository(t, repo)
			addRemote(t, repo, "https://github.com/acme/widget.git")
			include := "[include]\n\tpath = " + gitConfigEscaped(gitconfig.IncludePath(home)) + "\n"
			writeFile(t, globalConfigPath(home), strings.ReplaceAll(tt.global, "{include}", include))

			var before, beforeErr bytes.Buffer
			if code := run([]string{"doctor"}, &before, &beforeErr); code != 1 {
				t.Fatalf("doctor = %d, want 1, stdout: %s stderr: %s", code, before.String(), beforeErr.String())
			}
			if !strings.Contains(before.String(), "[identity-drift]") {
				t.Fatalf("doctor stdout = %q, want an identity drift before the fix", before.String())
			}

			var stdout, stderr bytes.Buffer
			code := run([]string{"doctor", "--fix"}, &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("doctor --fix = %d, want %d, stdout: %s stderr: %s", code, tt.wantCode, stdout.String(), stderr.String())
			}
			open := false
			for _, line := range strings.Split(stdout.String(), "\n") {
				if strings.HasPrefix(line, "[identity-drift]") {
					open = true
				}
			}
			if open != tt.wantDrift {
				t.Errorf("doctor --fix lists the identity drift as open = %v, want %v, stdout: %s", open, tt.wantDrift, stdout.String())
			}
			if !strings.Contains(stdout.String(), "fixed [managed-file-missing]") {
				t.Errorf("doctor --fix stdout = %q, want the missing include file reported as fixed", stdout.String())
			}

			// The next plain run agrees with what --fix printed.
			var again, againErr bytes.Buffer
			if next := run([]string{"doctor"}, &again, &againErr); next != tt.wantCode {
				t.Errorf("doctor after --fix = %d, want %d, stdout: %s", next, tt.wantCode, again.String())
			}
		})
	}
}

// outstandingLines returns the finding lines of a doctor text report.
func outstandingLines(report string) []string {
	var lines []string
	for _, line := range strings.Split(report, "\n") {
		if strings.HasPrefix(line, "[") {
			lines = append(lines, line)
		}
	}
	return lines
}

// TestRunDoctor_FixThatFailsPartwayReportsWhatIsStillOpen makes the second
// managed file unwritable, so "--fix" writes the include file and then
// fails. What it prints as open, and its exit status, must equal a plain
// doctor run afterwards.
func TestRunDoctor_FixThatFailsPartwayReportsWhatIsStillOpen(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+`
profiles:
  work:
    user.email: jane@acme.example
rules:
  - host: github.com
    profile: work
`)
	repo := filepath.Join(root, "github.com", "acme", "widget")
	initRepository(t, repo)
	addRemote(t, repo, "https://github.com/acme/widget.git")
	writeFile(t, globalConfigPath(home), "[include]\n\tpath = "+gitConfigEscaped(gitconfig.IncludePath(home))+"\n")

	// The include file sorts before the profile file, so it is written
	// first. The profile file is a symlink, which the write refuses.
	profilePath := gitconfig.ProfilePath(home, "work")
	writeFile(t, filepath.Join(home, "elsewhere.gitconfig"), "# placeholder\n")
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(profilePath), err)
	}
	symlinkOrSkip(t, filepath.Join(home, "elsewhere.gitconfig"), profilePath)

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--fix"}, &stdout, &stderr)
	if _, err := os.Stat(gitconfig.IncludePath(home)); err != nil {
		t.Fatalf("doctor --fix did not write the include file before failing: %v, stderr: %s", err, stderr.String())
	}

	var again, againErr bytes.Buffer
	nextCode := run([]string{"doctor"}, &again, &againErr)
	if code != nextCode {
		t.Errorf("doctor --fix = %d, want %d as a plain run afterwards, stdout: %s", code, nextCode, stdout.String())
	}
	got, want := outstandingLines(stdout.String()), outstandingLines(again.String())
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("doctor --fix lists %q as open, want what a plain run lists afterwards: %q", got, want)
	}
	if len(want) == 0 {
		t.Fatalf("plain doctor afterwards lists nothing, want the unwritable profile file reported, stdout: %s", again.String())
	}
}

// TestRunDoctor_FixCarriesARepositoryWithoutARemoteAlongWithACaseRename
// has a mis-cased owner directory that holds a repository with a remote
// and one without. The case rename moves both, and a repository doctor
// cannot inspect is no reason to withhold the report or fail the run
// afterwards: the report is printed, and the exit status equals a plain
// run afterwards.
func TestRunDoctor_FixCarriesARepositoryWithoutARemoteAlongWithACaseRename(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			home := setupEnv(t)
			root := filepath.Join(home, "src")
			writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+`
profiles:
  work:
    user.email: jane@acme.example
rules:
  - host: github.com
    profile: work
`)
			// The repositories are committed before the global file is
			// replaced: setupEnv's identity lives in that file, and a commit
			// without one fails on a machine that cannot guess a name.
			withRemote := filepath.Join(root, "github.com", "Foo", "gadget")
			initRepository(t, withRemote)
			addRemote(t, withRemote, "https://github.com/foo/gadget.git")
			initRepository(t, filepath.Join(root, "github.com", "Foo", "noremote"))
			writeFile(t, globalConfigPath(home), "[include]\n\tpath = "+gitConfigEscaped(gitconfig.IncludePath(home))+"\n")

			args := []string{"doctor", "--fix"}
			if format == "json" {
				args = append(args, "--json")
			}
			var stdout, stderr bytes.Buffer
			code := run(args, &stdout, &stderr)

			var again, againErr bytes.Buffer
			nextCode := run([]string{"doctor"}, &again, &againErr)
			if nextCode != 0 {
				t.Fatalf("doctor after --fix = %d, want 0, stdout: %s", nextCode, again.String())
			}
			if code != nextCode {
				t.Errorf("doctor %v = %d, want %d as a plain run afterwards, stdout: %s stderr: %s", args[1:], code, nextCode, stdout.String(), stderr.String())
			}
			if format == "text" {
				if !strings.Contains(stdout.String(), "config file:") {
					t.Errorf("doctor --fix stdout = %q, want the report", stdout.String())
				}
				return
			}
			dec := json.NewDecoder(&stdout)
			var out doctorOutput
			if err := dec.Decode(&out); err != nil {
				t.Fatalf("doctor --fix --json stdout is not a JSON object: %v", err)
			}
			if dec.More() {
				t.Errorf("doctor --fix --json stdout holds more than one JSON value")
			}
		})
	}
}

// TestRunDoctor_FixPrintsAScanWarningOnce guards the second pass of
// "--fix": a repository without an origin remote is skipped with a warning
// by the first pass, and the re-read after the repair must not print it
// again.
func TestRunDoctor_FixPrintsAScanWarningOnce(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+`
profiles:
  work:
    user.email: jane@acme.example
rules:
  - host: github.com
    profile: work
`)
	initRepository(t, filepath.Join(root, "local", "scratch"))

	var stdout, stderr bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdout, &stderr)
	if !strings.Contains(stdout.String(), "fixed [managed-file-missing]") {
		t.Fatalf("doctor --fix stdout = %q, want a repaired managed file so a second pass runs", stdout.String())
	}
	if got := strings.Count(stderr.String(), "skipped a repository scan could not inspect"); got != 1 {
		t.Errorf("doctor --fix printed the scan warning %d times, want once, stderr: %s", got, stderr.String())
	}
}

func TestRunDoctor_FixRerendersAMissingManagedFile(t *testing.T) {
	home := setupEnv(t)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  work:
    user.email: acme-work-identity
rules:
  - host: github.com
    profile: work
`)
	includePath := filepath.Join(home, ".config", "gitcd", "includes.gitconfig")
	if _, err := os.Stat(includePath); err == nil {
		t.Fatalf("%s already exists before --fix", includePath)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--fix"}, &stdout, &stderr)
	// The global identity key setupEnv's own baseline carries is never
	// fixed, so the exit code stays non-zero even after a successful fix
	// of everything mechanical.
	if code != 1 {
		t.Fatalf("doctor --fix = %d, want 1 (the global identity key finding is never fixed), stderr: %s", code, stderr.String())
	}
	if _, err := os.Stat(includePath); err != nil {
		t.Fatalf("doctor --fix did not render %s: %v", includePath, err)
	}
	if !strings.Contains(stdout.String(), "fixed [managed-file-missing]") {
		t.Fatalf("doctor --fix stdout = %q, want it to report the managed file finding as fixed", stdout.String())
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		if strings.HasPrefix(line, "[managed-file-missing]") || strings.HasPrefix(line, "[managed-file-stale]") {
			t.Fatalf("doctor --fix stdout carries an outstanding managed file finding: %q", line)
		}
	}
}

func TestRunDoctor_FixDryRunChangesNothing(t *testing.T) {
	home := setupEnv(t)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  work:
    user.email: acme-work-identity
rules:
  - host: github.com
    profile: work
`)
	includePath := filepath.Join(home, ".config", "gitcd", "includes.gitconfig")

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--fix", "--dry-run"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("doctor --fix --dry-run = %d, want 1, stderr: %s", code, stderr.String())
	}
	if _, err := os.Stat(includePath); err == nil {
		t.Fatalf("doctor --fix --dry-run wrote %s", includePath)
	}
	if !strings.Contains(stdout.String(), "would rerender") {
		t.Fatalf("doctor --fix --dry-run stdout = %q, want it to describe what would change", stdout.String())
	}
}

func TestRunDoctor_JSONOutput(t *testing.T) {
	home := setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--json"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("doctor --json = %d, want 1, stderr: %s", code, stderr.String())
	}

	var out struct {
		ConfigPath string `json:"configPath"`
		ManagedDir string `json:"managedDir"`
		Findings   []struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("doctor --json produced invalid JSON: %v, output: %s", err, stdout.String())
	}
	if out.ConfigPath != filepath.Join(home, ".gitcd.yaml") {
		t.Fatalf("doctor --json configPath = %q, want %q", out.ConfigPath, filepath.Join(home, ".gitcd.yaml"))
	}
	if len(out.Findings) == 0 {
		t.Fatal("doctor --json findings is empty, want at least the global identity key finding")
	}
}

func TestRunDoctor_JSONFixOutputIsValidJSON(t *testing.T) {
	home := setupEnv(t)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  work:
    user.email: acme-work-identity
rules:
  - host: github.com
    profile: work
`)

	var stdout, stderr bytes.Buffer
	run([]string{"doctor", "--json", "--fix"}, &stdout, &stderr)

	var out struct {
		ConfigPath string           `json:"configPath"`
		Fixed      []map[string]any `json:"fixed"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("doctor --json --fix produced invalid JSON on stdout: %v, output: %s", err, stdout.String())
	}
	if out.ConfigPath != filepath.Join(home, ".gitcd.yaml") {
		t.Fatalf("doctor --json --fix configPath = %q, want %q", out.ConfigPath, filepath.Join(home, ".gitcd.yaml"))
	}
	if len(out.Fixed) == 0 {
		t.Fatal("doctor --json --fix fixed is empty, want the managed file fix reported")
	}
}

func TestRunDoctor_JSONFixDryRunOutputIsValidJSON(t *testing.T) {
	home := setupEnv(t)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  work:
    user.email: acme-work-identity
rules:
  - host: github.com
    profile: work
`)

	var stdout, stderr bytes.Buffer
	run([]string{"doctor", "--json", "--fix", "--dry-run"}, &stdout, &stderr)

	var out struct {
		ConfigPath string `json:"configPath"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("doctor --json --fix --dry-run produced invalid JSON on stdout: %v, output: %s", err, stdout.String())
	}
}

func TestRunDoctor_FixRenamesACaseOnlyPathMismatch(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	// The layout renders lowercase, but this clone sits under an upper
	// cased owner segment: a case only mismatch.
	misCased := filepath.Join(root, "github.com", "ACME", "widget")
	initRepository(t, misCased)
	runGit(t, misCased, "remote", "add", "origin", "https://github.com/acme/widget.git")

	var stdout, stderr bytes.Buffer
	// The global identity key from setupEnv's own baseline is never
	// fixed, so the exit code is not asserted here: TestRunDoctor_FixRerendersAMissingManagedFile
	// already covers that it stays non-zero.
	run([]string{"doctor", "--fix"}, &stdout, &stderr)

	wantPath := filepath.Join(root, "github.com", "acme", "widget")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("doctor --fix did not rename the repository to %s: %v", wantPath, err)
	}
	// os.Stat alone cannot tell the two spellings apart on a case
	// insensitive, case preserving filesystem (APFS, the macOS default):
	// it resolves "ACME" and "acme" to the same entry either way. Reading
	// the parent directory back reports the entry's own, exact spelling.
	entries, err := os.ReadDir(filepath.Join(root, "github.com"))
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(root, "github.com"), err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !containsExact(names, "acme") {
		t.Fatalf("directory entries under github.com = %v, want the exact spelling %q on disk", names, "acme")
	}
	if containsExact(names, "ACME") {
		t.Fatalf("directory entries under github.com = %v, want the old spelling %q gone", names, "ACME")
	}
	if !strings.Contains(stdout.String(), "renamed") {
		t.Fatalf("doctor --fix stdout = %q, want it to report the rename", stdout.String())
	}
}

func containsExact(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestRunDoctor_FixRenameMessageNamesTheAncestorNotTheRepoPath(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	// Two repositories share one mis-cased owner ancestor, and both
	// remotes agree it should read lowercase: a legitimate, uncontested
	// rename of the shared ancestor, not the per-repository leaf path.
	widget := filepath.Join(root, "github.com", "ACME", "widget")
	initRepository(t, widget)
	runGit(t, widget, "remote", "add", "origin", "https://github.com/acme/widget.git")
	gadget := filepath.Join(root, "github.com", "ACME", "gadget")
	initRepository(t, gadget)
	runGit(t, gadget, "remote", "add", "origin", "https://github.com/acme/gadget.git")

	var stdout, stderr bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdout, &stderr)

	oldAncestor := filepath.Join(root, "github.com", "ACME")
	newAncestor := filepath.Join(root, "github.com", "acme")
	wantLine := fmt.Sprintf("renamed %s to %s", oldAncestor, newAncestor)
	if !strings.Contains(stdout.String(), wantLine) {
		t.Fatalf("doctor --fix stdout = %q, want a line naming the ancestor pair %q", stdout.String(), wantLine)
	}
	badLine := fmt.Sprintf("renamed %s to %s", widget, filepath.Join(root, "github.com", "acme", "widget"))
	if strings.Contains(stdout.String(), badLine) {
		t.Fatalf("doctor --fix stdout = %q, want it to never name the repository's own leaf path as what moved", stdout.String())
	}
}

func TestRunDoctor_FixPrintsTheSharedAncestorRenameExactlyOnce(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	// Two repositories share one mis-cased owner ancestor, and both
	// remotes agree it should read lowercase: renameCaseOnly's own call
	// for "widget" renames the shared ancestor; its call for "gadget"
	// must recognise that ancestor is already correctly spelled and
	// print nothing a second time, on every filesystem alike, including
	// a case insensitive but case preserving one (APFS, the macOS
	// default), where "os.Stat" alone cannot tell "ACME" and "acme"
	// apart.
	widget := filepath.Join(root, "github.com", "ACME", "widget")
	initRepository(t, widget)
	runGit(t, widget, "remote", "add", "origin", "https://github.com/acme/widget.git")
	gadget := filepath.Join(root, "github.com", "ACME", "gadget")
	initRepository(t, gadget)
	runGit(t, gadget, "remote", "add", "origin", "https://github.com/acme/gadget.git")

	var stdout, stderr bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdout, &stderr)

	oldAncestor := filepath.Join(root, "github.com", "ACME")
	newAncestor := filepath.Join(root, "github.com", "acme")
	wantLine := fmt.Sprintf("renamed %s to %s", oldAncestor, newAncestor)
	if got := strings.Count(stdout.String(), wantLine); got != 1 {
		t.Fatalf("doctor --fix stdout contains %q %d times, want exactly once: %q", wantLine, got, stdout.String())
	}
}

func TestRunDoctor_FixRefusesAContestedAncestorAndConverges(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	// Both repositories physically share one ancestor directory, spelled
	// "Acme" on disk. "widget"'s own remote wants it lowercase, so it is
	// mismatched; "other"'s remote wants exactly "Acme", so it already
	// matches and carries no finding of its own. Renaming the ancestor to
	// fix "widget" would immediately un-place "other".
	widget := filepath.Join(root, "github.com", "Acme", "widget")
	initRepository(t, widget)
	runGit(t, widget, "remote", "add", "origin", "https://github.com/acme/widget.git")
	other := filepath.Join(root, "github.com", "Acme", "other")
	initRepository(t, other)
	runGit(t, other, "remote", "add", "origin", "https://github.com/Acme/other.git")

	readEntries := func() []string {
		entries, err := os.ReadDir(filepath.Join(root, "github.com"))
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Join(root, "github.com"), err)
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return names
	}
	before := readEntries()

	var stdout1, stderr1 bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdout1, &stderr1)
	if strings.Contains(stdout1.String(), "renamed") {
		t.Fatalf("doctor --fix (run 1) stdout = %q, want no rename of a contested ancestor", stdout1.String())
	}
	if !strings.Contains(stdout1.String(), "ancestor-case-conflict") {
		t.Fatalf("doctor --fix (run 1) stdout = %q, want an ancestor-case-conflict finding", stdout1.String())
	}
	afterRun1 := readEntries()
	if !equalStringSets(before, afterRun1) {
		t.Fatalf("doctor --fix (run 1) changed the directory entries under github.com: before %v, after %v", before, afterRun1)
	}

	var stdout2, stderr2 bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdout2, &stderr2)
	if strings.Contains(stdout2.String(), "renamed") {
		t.Fatalf("doctor --fix (run 2) stdout = %q, want no rename of a contested ancestor", stdout2.String())
	}
	if !strings.Contains(stdout2.String(), "ancestor-case-conflict") {
		t.Fatalf("doctor --fix (run 2) stdout = %q, want the conflict reported again", stdout2.String())
	}
	afterRun2 := readEntries()
	if !equalStringSets(afterRun1, afterRun2) {
		t.Fatalf(
			"doctor --fix (run 2) is not a no-op: directory entries under github.com changed, run 1 %v, run 2 %v",
			afterRun1, afterRun2,
		)
	}
}

func TestRunDoctor_FixReportsAndFixesAnUnrelatedLeafMismatchUnderAContestedAncestor(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	// Three repositories share one ancestor directory, "Acme". "widget"
	// and "other" genuinely contest its spelling, exactly the fixture
	// TestRunDoctor_FixRefusesAContestedAncestorAndConverges already
	// uses. "Mismatched" agrees with "Acme" and carries its own,
	// unrelated leaf mismatch: the directory reads "Mismatched", its own
	// remote implies the lowercase "mismatched", a mismatch one segment
	// below the contested ancestor, not the ancestor itself.
	widget := filepath.Join(root, "github.com", "Acme", "widget")
	initRepository(t, widget)
	runGit(t, widget, "remote", "add", "origin", "https://github.com/acme/widget.git")
	other := filepath.Join(root, "github.com", "Acme", "other")
	initRepository(t, other)
	runGit(t, other, "remote", "add", "origin", "https://github.com/Acme/other.git")
	mismatched := filepath.Join(root, "github.com", "Acme", "Mismatched")
	initRepository(t, mismatched)
	runGit(t, mismatched, "remote", "add", "origin", "https://github.com/Acme/mismatched.git")
	wantMismatchedPath := filepath.Join(root, "github.com", "Acme", "mismatched")

	var stdoutBefore, stderrBefore bytes.Buffer
	code := run([]string{"doctor"}, &stdoutBefore, &stderrBefore)
	if code != 1 {
		t.Fatalf("doctor = %d, want 1, stderr: %s", code, stderrBefore.String())
	}
	before := stdoutBefore.String()
	if !strings.Contains(before, "ancestor-case-conflict") {
		t.Fatalf("doctor stdout = %q, want the ancestor conflict reported", before)
	}
	if !strings.Contains(before, "path-case-mismatch") || !strings.Contains(before, mismatched) {
		t.Fatalf("doctor stdout = %q, want the leaf mismatch of %s reported too, not swallowed by the ancestor conflict", before, mismatched)
	}

	var stdoutFix1, stderrFix1 bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdoutFix1, &stderrFix1)
	fix1 := stdoutFix1.String()
	if !strings.Contains(fix1, "ancestor-case-conflict") {
		t.Fatalf("doctor --fix stdout = %q, want the ancestor conflict still reported as outstanding", fix1)
	}
	contestedAncestor := filepath.Join(root, "github.com", "Acme")
	if strings.Contains(fix1, "renamed "+contestedAncestor+" to") {
		t.Fatalf("doctor --fix stdout = %q, want the contested ancestor %s never renamed", fix1, contestedAncestor)
	}
	wantFixLine := fmt.Sprintf("renamed %s to %s", mismatched, wantMismatchedPath)
	if !strings.Contains(fix1, wantFixLine) {
		t.Fatalf("doctor --fix stdout = %q, want it to fix the leaf mismatch: %q", fix1, wantFixLine)
	}
	if _, err := os.Stat(wantMismatchedPath); err != nil {
		t.Fatalf("doctor --fix did not rename %s to %s: %v", mismatched, wantMismatchedPath, err)
	}
	if _, err := os.Stat(widget); err != nil {
		t.Fatalf("doctor --fix moved the contested repository %s: %v", widget, err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("doctor --fix moved the contested repository %s: %v", other, err)
	}

	// Convergence: a second "--fix" run changes nothing further. The
	// leaf is already fixed, and the ancestor conflict is reported again,
	// identically, rather than flipping anything.
	var stdoutFix2, stderrFix2 bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdoutFix2, &stderrFix2)
	fix2 := stdoutFix2.String()
	if strings.Contains(fix2, "renamed") {
		t.Fatalf("doctor --fix (run 2) stdout = %q, want no further rename: the tree already converged", fix2)
	}
	if !strings.Contains(fix2, "ancestor-case-conflict") {
		t.Fatalf("doctor --fix (run 2) stdout = %q, want the ancestor conflict reported again", fix2)
	}
}

func TestRunDoctor_FixesTheSameLeafMismatchWithNoAncestorConflict(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	// The same leaf-only mismatch as above, with the contesting "widget"
	// and "other" repositories removed: no ancestor conflict exists at
	// all, so filterContestedPathCase has nothing to suppress. The leaf
	// mismatch is reported and fixed exactly as it is above, confirming
	// the ancestor conflict, not something else, was what suppressed it.
	mismatched := filepath.Join(root, "github.com", "Acme", "Mismatched")
	initRepository(t, mismatched)
	runGit(t, mismatched, "remote", "add", "origin", "https://github.com/Acme/mismatched.git")
	wantMismatchedPath := filepath.Join(root, "github.com", "Acme", "mismatched")

	var stdout, stderr bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdout, &stderr)
	out := stdout.String()
	if strings.Contains(out, "ancestor-case-conflict") {
		t.Fatalf("doctor --fix stdout = %q, want no ancestor conflict in this fixture", out)
	}
	wantFixLine := fmt.Sprintf("renamed %s to %s", mismatched, wantMismatchedPath)
	if !strings.Contains(out, wantFixLine) {
		t.Fatalf("doctor --fix stdout = %q, want it to fix the leaf mismatch: %q", out, wantFixLine)
	}
	if _, err := os.Stat(wantMismatchedPath); err != nil {
		t.Fatalf("doctor --fix did not rename %s to %s: %v", mismatched, wantMismatchedPath, err)
	}

	var stdout2, stderr2 bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdout2, &stderr2)
	if strings.Contains(stdout2.String(), "renamed") {
		t.Fatalf("doctor --fix (run 2) stdout = %q, want no further rename: the tree already converged", stdout2.String())
	}
}

// ownerEntryName reads the exact, on-disk spelling of the owner
// directory ("acme" or "Acme") under root/github.com: os.Stat alone
// cannot tell the two spellings apart on a case insensitive, case
// preserving filesystem (APFS, the macOS default), since it resolves
// both to the same entry either way. Reading the parent directory back
// reports the entry's own, exact spelling, the same technique
// TestRunDoctor_FixRenamesACaseOnlyPathMismatch already relies on.
func ownerEntryName(t *testing.T, root string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "github.com"))
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(root, "github.com"), err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory entries under %s = %v, want exactly one", filepath.Join(root, "github.com"), entries)
	}
	return entries[0].Name()
}

func TestRunDoctor_FixNeverReportsFixedOrExitsZeroForAStillMisplacedTwoSegmentMismatch(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	// Two segments differ at once: the host ("GitHub.com" instead of
	// "github.com") and the owner ("Acme" instead of "acme").
	// renameCaseOnly only ever renames the topmost differing segment per
	// call, the host here, so one "--fix" run cannot fully place this
	// repository.
	misCased := filepath.Join(root, "GitHub.com", "Acme", "widget")
	initRepository(t, misCased)
	runGit(t, misCased, "remote", "add", "origin", "https://github.com/acme/widget.git")

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--fix"}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("doctor --fix = 0, want non-zero: the repository is still misplaced after one run, stdout: %s", stdout.String())
	}

	// Only the host segment was renamed; the owner segment must still
	// read "Acme", the exact, on-disk spelling, not merely something
	// "os.Stat" resolves on a case insensitive filesystem.
	if got := ownerEntryName(t, root); got != "Acme" {
		t.Fatalf("owner directory entry = %q, want it still spelled %q after only one run", got, "Acme")
	}
	wantAfterOneRun := filepath.Join(root, "github.com", "Acme", "widget")
	if _, err := os.Stat(wantAfterOneRun); err != nil {
		t.Fatalf("doctor --fix did not rename the host segment to %s: %v", wantAfterOneRun, err)
	}

	out := stdout.String()
	if strings.Contains(out, "fixed [path-case-mismatch]") {
		t.Fatalf("doctor --fix stdout = %q, want no \"fixed [path-case-mismatch]\" line: the repository is still misplaced", out)
	}
}

// TestRunDoctor_FixNamesTheCurrentPathOfAMismatchItLeavesOutstanding covers
// a run with nothing but a case rename to repair: the finding that stays
// open after the first segment moved must name where the repository is
// now, not where it was before the rename.
func TestRunDoctor_FixNamesTheCurrentPathOfAMismatchItLeavesOutstanding(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")
	misCased := filepath.Join(root, "GitHub.com", "Acme", "widget")
	initRepository(t, misCased)
	addRemote(t, misCased, "https://github.com/acme/widget.git")
	// Render the managed files first, so the case rename is the only repair
	// "--fix" has left to make.
	var applyOut, applyErr bytes.Buffer
	if code := run([]string{"apply"}, &applyOut, &applyErr); code != 0 {
		t.Fatalf("apply = %d, stdout: %s stderr: %s", code, applyOut.String(), applyErr.String())
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"doctor", "--fix"}, &stdout, &stderr); code != 1 {
		t.Fatalf("doctor --fix = %d, want 1, stdout: %s stderr: %s", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "fixed [managed-file") {
		t.Fatalf("doctor --fix stdout = %q, want no managed file repair in this run", stdout.String())
	}
	now := filepath.Join(root, "github.com", "Acme", "widget")
	if !strings.Contains(stdout.String(), "on disk at "+now+",") {
		t.Errorf("doctor --fix stdout = %q, want the open finding to name the current path %s", stdout.String(), now)
	}
}

func TestRunDoctor_FixConvergesOnATwoSegmentMismatchOverSeveralRuns(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "root: "+root+"\n")

	misCased := filepath.Join(root, "GitHub.com", "Acme", "widget")
	initRepository(t, misCased)
	runGit(t, misCased, "remote", "add", "origin", "https://github.com/acme/widget.git")

	var stdout1, stderr1 bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdout1, &stderr1)
	if got := ownerEntryName(t, root); got != "Acme" {
		t.Fatalf("owner directory entry after run 1 = %q, want convergence to take more than one run", got)
	}

	var stdout2, stderr2 bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdout2, &stderr2)
	if got := ownerEntryName(t, root); got != "acme" {
		t.Fatalf("owner directory entry after run 2 = %q, want %q: doctor --fix did not converge", got, "acme")
	}
	if _, err := os.Stat(filepath.Join(root, "github.com", "acme", "widget")); err != nil {
		t.Fatalf("doctor --fix (run 2) did not converge to the fully placed path: %v", err)
	}
	if !strings.Contains(stdout2.String(), "fixed [path-case-mismatch]") {
		t.Fatalf("doctor --fix (run 2) stdout = %q, want the finding reported as fixed once fully placed", stdout2.String())
	}

	var stdout3, stderr3 bytes.Buffer
	run([]string{"doctor", "--fix"}, &stdout3, &stderr3)
	if strings.Contains(stdout3.String(), "renamed") {
		t.Fatalf("doctor --fix (run 3) stdout = %q, want no further rename: the tree already converged", stdout3.String())
	}
}

func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := map[string]int{}
	for _, v := range a {
		counts[v]++
	}
	for _, v := range b {
		counts[v]--
	}
	for _, c := range counts {
		if c != 0 {
			return false
		}
	}
	return true
}
