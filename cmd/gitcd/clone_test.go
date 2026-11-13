// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunClone_DryRunPrintsThePathWithoutCloning(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	var stdout, stderr bytes.Buffer
	code := run([]string{"clone", "--dry-run", "https://example.test/acme/widget.git"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("clone --dry-run = %d, want 0, stderr: %s", code, stderr.String())
	}
	want := filepath.Join(root, "example.test", "acme", "widget")
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Fatalf("clone --dry-run stdout = %q, want %q", got, want)
	}
	if _, err := os.Stat(want); err == nil {
		t.Fatalf("clone --dry-run created %s, want it untouched", want)
	}
}

func TestRunClone_ClonesAFreshTargetThroughARewrittenOfflineURL(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	source := filepath.Join(home, "upstream")
	initRepository(t, source)

	input := "https://example.test/acme/widget.git"
	// The URL rewrite git itself applies before ever touching the network,
	// pointed at the local source instead: this clone never leaves the
	// machine.
	runGit(t, home, "config", "--global", "url.file://"+source+".insteadOf", input)

	var stdout, stderr bytes.Buffer
	code := run([]string{"clone", input}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("clone %q = %d, want 0, stderr: %s", input, code, stderr.String())
	}
	want := filepath.Join(root, "example.test", "acme", "widget")
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Fatalf("clone %q stdout = %q, want %q", input, got, want)
	}
	if _, err := os.Stat(filepath.Join(want, "README.md")); err != nil {
		t.Fatalf("cloned repository is missing its content: %v", err)
	}

	// A second clone of the same, rewritten input must still be idempotent:
	// gitcd must compare against the remote exactly as git stored it, not
	// against the rewritten target the insteadOf rule actually fetched
	// from, which "git remote get-url" would report instead.
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"clone", input}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("second clone %q = %d, want 0 for an idempotent clone, stderr: %s", input, code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Fatalf("second clone %q stdout = %q, want %q", input, got, want)
	}
}

func TestRunClone_ExistingTargetWithTheSameRemoteIsIdempotent(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	input := "https://example.test/acme/widget.git"
	dest := filepath.Join(root, "example.test", "acme", "widget")
	initRepository(t, dest)
	runGit(t, dest, "remote", "add", "origin", input)

	var stdout, stderr bytes.Buffer
	code := run([]string{"clone", input}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("clone %q = %d, want 0 for an idempotent clone, stderr: %s", input, code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != dest {
		t.Fatalf("clone %q stdout = %q, want %q", input, got, dest)
	}
}

func TestRunClone_ExistingTargetWithADifferentRemoteFailsNamingBoth(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	input := "https://example.test/acme/widget.git"
	otherRemote := "https://example.test/acme/other.git"
	dest := filepath.Join(root, "example.test", "acme", "widget")
	initRepository(t, dest)
	runGit(t, dest, "remote", "add", "origin", otherRemote)

	var stdout, stderr bytes.Buffer
	code := run([]string{"clone", input}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("clone %q = 0, want a failure for a conflicting remote", input)
	}
	if !strings.Contains(stderr.String(), input) || !strings.Contains(stderr.String(), otherRemote) {
		t.Fatalf("clone %q stderr = %q, want it to name both remotes", input, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("clone %q wrote to stdout on failure: %q", input, stdout.String())
	}
}

// TestRunClone_AzureDevOpsDefaultLayoutKeepsTheProjectAndAgreesAcrossProtocols
// guards against the default configuration, with no "~/.gitcd.yaml" at
// all, silently dropping the project segment and rendering the SSH and
// HTTPS forms of one Azure DevOps repository into two different trees.
func TestRunClone_AzureDevOpsDefaultLayoutKeepsTheProjectAndAgreesAcrossProtocols(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")
	want := filepath.Join(root, "dev.azure.com", "myorg", "myproject", "myrepo")

	var sshStdout, sshStderr bytes.Buffer
	sshCode := run(
		[]string{"clone", "--dry-run", "git@ssh.dev.azure.com:v3/myorg/myproject/myrepo"}, &sshStdout, &sshStderr,
	)
	if sshCode != 0 {
		t.Fatalf("clone --dry-run (ssh) = %d, want 0, stderr: %s", sshCode, sshStderr.String())
	}
	if got := strings.TrimSpace(sshStdout.String()); got != want {
		t.Fatalf("clone --dry-run (ssh) stdout = %q, want %q", got, want)
	}

	var httpsStdout, httpsStderr bytes.Buffer
	httpsCode := run(
		[]string{"clone", "--dry-run", "https://dev.azure.com/myorg/myproject/_git/myrepo"}, &httpsStdout, &httpsStderr,
	)
	if httpsCode != 0 {
		t.Fatalf("clone --dry-run (https) = %d, want 0, stderr: %s", httpsCode, httpsStderr.String())
	}
	if got := strings.TrimSpace(httpsStdout.String()); got != want {
		t.Fatalf("clone --dry-run (https) stdout = %q, want %q", got, want)
	}
}

// TestRunClone_AzureDevOpsBareShorthandRoundTripsTheRenderedPath guards
// against "gitcd clone" rejecting the exact string its own
// "{host}/{org}/{project}/{repo}" layout renders, which made the rendered
// path unreadable by the command that rendered it.
func TestRunClone_AzureDevOpsBareShorthandRoundTripsTheRenderedPath(t *testing.T) {
	home := setupEnv(t)
	root := filepath.Join(home, "Code")

	var stdout, stderr bytes.Buffer
	code := run([]string{"clone", "--dry-run", "dev.azure.com/myorg/myproject/myrepo"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("clone --dry-run (bare shorthand) = %d, want 0, stderr: %s", code, stderr.String())
	}
	want := filepath.Join(root, "dev.azure.com", "myorg", "myproject", "myrepo")
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Fatalf("clone --dry-run (bare shorthand) stdout = %q, want %q", got, want)
	}
}

func TestRunClone_InvalidRemoteFails(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"clone", "not-a-remote"}, &stdout, &stderr)

	if code == 0 {
		t.Fatal("clone with an invalid remote = 0, want a failure")
	}
	if stdout.Len() != 0 {
		t.Fatalf("clone with an invalid remote wrote to stdout: %q", stdout.String())
	}
}

func TestRunClone_ProfilePinsTheIdentityAtCloneTime(t *testing.T) {
	home := setupEnv(t)

	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  work:
    user.name: Work Identity
    user.email: work@example.test
`)

	source := filepath.Join(home, "upstream")
	initRepository(t, source)
	input := "https://example.test/acme/widget.git"
	runGit(t, home, "config", "--global", "url.file://"+source+".insteadOf", input)

	var stdout, stderr bytes.Buffer
	code := run([]string{"clone", "--profile", "work", input}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("clone --profile work = %d, want 0, stderr: %s", code, stderr.String())
	}

	dest := strings.TrimSpace(stdout.String())
	if dest == "" {
		t.Fatalf("clone --profile work printed nothing on stdout, stderr: %s", stderr.String())
	}
	got := runGitOutput(t, dest, "config", "--local", "--get", "user.email")
	if got != "work@example.test" {
		t.Fatalf("pinned user.email = %q, want %q", got, "work@example.test")
	}

	if !strings.Contains(stderr.String(), "user.email") {
		t.Fatalf("clone --profile work stderr = %q, want it to report the effective identity read back", stderr.String())
	}
	if !strings.Contains(stderr.String(), "expected profile") {
		t.Fatalf("clone --profile work stderr = %q, want it to report the expected profile through the resolver seam", stderr.String())
	}
}

// TestRunClone_ProfileWritesMarkersThatUnsetCanRemove guards against
// "clone --profile" pinning keys through "git clone -c" without recording
// the same "gitcd.profile" and "gitcd.keys" markers "profile set" writes:
// without them, "profile unset" has nothing to read back and refuses to
// clean up what the clone pinned.
func TestRunClone_ProfileWritesMarkersThatUnsetCanRemove(t *testing.T) {
	home := setupEnv(t)

	writeFile(t, filepath.Join(home, ".gitcd.yaml"), `
profiles:
  work:
    user.name: Work Identity
    user.email: work@example.test
`)

	source := filepath.Join(home, "upstream")
	initRepository(t, source)
	input := "https://example.test/acme/widget.git"
	runGit(t, home, "config", "--global", "url.file://"+source+".insteadOf", input)

	var stdout, stderr bytes.Buffer
	code := run([]string{"clone", "--profile", "work", input}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("clone --profile work = %d, want 0, stderr: %s", code, stderr.String())
	}
	dest := strings.TrimSpace(stdout.String())

	if got := runGitOutput(t, dest, "config", "--local", "--get", "gitcd.profile"); got != "work" {
		t.Fatalf("gitcd.profile = %q, want %q", got, "work")
	}
	keys := runGitOutput(t, dest, "config", "--local", "--get", "gitcd.keys")
	for _, want := range []string{"user.name", "user.email"} {
		if !strings.Contains(keys, want) {
			t.Fatalf("gitcd.keys = %q, want it to list %q", keys, want)
		}
	}

	var unsetStdout, unsetStderr bytes.Buffer
	unsetCode := run([]string{"profile", "unset", dest}, &unsetStdout, &unsetStderr)
	if unsetCode != 0 {
		t.Fatalf("profile unset after clone --profile = %d, want 0, stderr: %s", unsetCode, unsetStderr.String())
	}
	if localConfigGet(t, dest, "user.email") == nil {
		t.Fatal("profile unset after clone --profile left user.email set")
	}
	if localConfigGet(t, dest, "gitcd.profile") == nil {
		t.Fatal("profile unset after clone --profile left the gitcd.profile marker set")
	}
}

func TestRunClone_UnknownProfileFails(t *testing.T) {
	home := setupEnv(t)
	source := filepath.Join(home, "upstream")
	initRepository(t, source)

	var stdout, stderr bytes.Buffer
	code := run([]string{"clone", "--profile", "missing", source}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("clone --profile missing = 0, want a failure for an undefined profile")
	}
	if stdout.Len() != 0 {
		t.Fatalf("clone --profile missing wrote to stdout: %q", stdout.String())
	}
}

func TestRunClone_RequiresExactlyOneArgument(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"clone"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("clone with no argument = %d, want 2", code)
	}
}
