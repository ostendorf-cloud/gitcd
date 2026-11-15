// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/config"
)

// TestRunApply_WritesNowhereButTheManagedDirectory pins down where
// "apply" is allowed to put a file, rather than what it says while
// doing it. A profile name becomes the name of the file the profile is
// rendered to, so the name decides a path; this walks the whole home
// directory afterwards and asserts that every file "apply" produced
// sits under the directory gitcd owns and regenerates.
func TestRunApply_WritesNowhereButTheManagedDirectory(t *testing.T) {
	tests := []struct {
		name        string
		profileName string
	}{
		{name: "a name climbing above the managed directory", profileName: "../../escaped-profile"},
		{name: "a name climbing one level", profileName: "../escaped-profile"},
		{name: "a name naming a subdirectory", profileName: "nested/escaped-profile"},
		{name: "a name using a backslash separator", profileName: `nested\escaped-profile`},
		{name: "a name that is the parent directory", profileName: ".."},
		{name: "a name that is the current directory", profileName: "."},
		{name: "a name starting with a dot", profileName: ".hidden-profile"},
		{name: "an absolute name", profileName: "/etc/escaped-profile"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := setupEnv(t)
			writeFile(t, filepath.Join(home, ".gitcd.yaml"), profileAndRule(tt.profileName))

			var stdout, stderr bytes.Buffer
			run([]string{"apply"}, &stdout, &stderr)

			profilesDir := filepath.Join(home, ".config", "gitcd", "profiles")
			includePath := filepath.Join(home, ".config", "gitcd", "includes.gitconfig")
			for _, path := range renderedFiles(t, home) {
				if path == includePath {
					continue
				}
				if filepath.Dir(path) != profilesDir {
					t.Errorf("apply wrote %s, outside the profiles directory %s", path, profilesDir)
				}
			}
		})
	}

	t.Run("an ordinary name renders inside it", func(t *testing.T) {
		home := setupEnv(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), profileAndRule("work.eu_2"))

		var stdout, stderr bytes.Buffer
		if code := run([]string{"apply"}, &stdout, &stderr); code != 0 {
			t.Fatalf("apply = %d, want 0, stderr: %s", code, stderr.String())
		}

		want := filepath.Join(home, ".config", "gitcd", "profiles", "work.eu_2.gitconfig")
		if _, err := os.Stat(want); err != nil {
			t.Fatalf("apply did not write %s: %v", want, err)
		}
	})
}

// TestRunApply_GivesEveryProfileItsOwnFile asserts on what is on disk
// after "apply", not on what it rendered or printed. Two profile names
// that a case insensitive filesystem cannot tell apart map to one path,
// and the only place that shows up is the file: both rules' "path" lines
// still point somewhere real, "apply" still prints two paths and still
// exits 0, and one of the two identities is simply gone.
func TestRunApply_GivesEveryProfileItsOwnFile(t *testing.T) {
	profiles := map[string]string{
		"work": "work@example.test",
		"Work": "other@example.test",
	}

	home := setupEnv(t)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"),
		"profiles:\n  work:\n    user.email: work@example.test\n"+
			"  Work:\n    user.email: other@example.test\n"+
			"\nrules:\n  - host: github.com\n    owner: acme\n    profile: work\n"+
			"  - host: gitlab.com\n    owner: acme\n    profile: Work\n")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"apply"}, &stdout, &stderr); code != 0 {
		// Refused, so no file was written under either name and the
		// identity of neither profile can have been replaced.
		return
	}

	for name, email := range profiles {
		path := filepath.Join(home, ".config", "gitcd", "profiles", name+".gitconfig")
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("apply reported success but did not write %s: %v", path, err)
		}
		if !strings.Contains(string(content), email) {
			t.Errorf("%s holds %q, want the profile %q own address %q", path, content, name, email)
		}
	}
}

// TestRunApply_ReadsNoConfigurationTheGateNeverSaw asserts the property
// the whole restricted set depends on: what a profile applies is what
// the gate looked at.
//
// A key whose value makes git read another configuration file hands
// every key in that file to git ungated, so one such key is worth the
// entire set. The assertion is therefore on what git resolves, through
// "git config --show-origin", for keys that are restricted when named
// directly; checking the rendered text would not have caught this,
// because the rendered text is a correct "[include]" block.
func TestRunApply_ReadsNoConfigurationTheGateNeverSaw(t *testing.T) {
	smuggled := map[string]string{
		"core.sshCommand":   "/bin/echo reached-via-include",
		"core.hooksPath":    "/tmp/hooks-reached-via-include",
		"credential.helper": "/bin/echo",
	}

	for _, pointing := range []string{"include.path", `includeIf.gitdir:/.path`} {
		t.Run("a profile carrying "+pointing, func(t *testing.T) {
			home := setupEnv(t)

			// A real configuration file, holding keys a profile may not
			// carry, sitting where a profile would point at it.
			pulledIn := filepath.Join(home, "pulled-in.inc")
			var b strings.Builder
			for _, key := range sortedKeys(smuggled) {
				section, _, name, err := config.SplitKey(key)
				if err != nil {
					t.Fatalf("SplitKey(%q): %v", key, err)
				}
				b.WriteString("[" + section + "]\n\t" + name + " = " + smuggled[key] + "\n")
			}
			writeFile(t, pulledIn, b.String())

			writeFile(t, filepath.Join(home, ".gitcd.yaml"),
				"profiles:\n  private:\n    user.email: simon@example.test\n    "+
					quoteYAML(pointing)+": "+quoteYAML(pulledIn)+"\n"+
					"\nrules:\n  - host: github.com\n    owner: acme\n    profile: private\n")

			var stdout, stderr bytes.Buffer
			run([]string{"apply"}, &stdout, &stderr)

			// Whatever apply decided, the managed files are now wired in
			// the way "init" wires them, and git is asked what it
			// actually resolves in a repository the rule matches.
			repo := filepath.Join(home, "Code", "github.com", "acme", "widget")
			if err := os.MkdirAll(repo, 0o755); err != nil {
				t.Fatalf("create %s: %v", repo, err)
			}
			runGit(t, repo, "init", "--quiet")
			runGit(t, home, "config", "--global", "include.path",
				filepath.Join(home, ".config", "gitcd", "includes.gitconfig"))

			for _, key := range sortedKeys(smuggled) {
				if got := gitConfigOrigin(t, repo, key); got != "" {
					t.Errorf("git resolves %s = %q in %s, want a key the gate never saw to be unset", key, got, repo)
				}
			}

			// The include that wires gitcd's own managed file in is
			// expected; one pointing at the file the profile named is
			// not, whatever else it would have pulled in with it.
			for _, path := range gitConfigAll(t, repo, "include.path") {
				if path == pulledIn {
					t.Errorf("git resolves include.path = %q in %s, want no include the gate never saw", path, repo)
				}
			}
		})
	}
}

// gitConfigAll returns every value git resolves for key in the
// repository at dir, in the order git reports them.
func gitConfigAll(t *testing.T, dir, key string) []string {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "git", "config", "--get-all", key)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil
		}
		t.Fatalf("git config --get-all %s in %s: %v: %s", key, dir, err, out)
	}

	var values []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line != "" {
			values = append(values, line)
		}
	}
	return values
}

// gitConfigOrigin returns what git resolves for key in the repository at
// dir, with its origin, or "" when git finds no value. A file git cannot
// parse fails the test rather than reading as an empty value.
func gitConfigOrigin(t *testing.T, dir, key string) string {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "git", "config", "--show-origin", "--get", key)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		return strings.TrimRight(string(out), "\n")
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return ""
	}
	t.Fatalf("git config --show-origin --get %s in %s: %v: %s", key, dir, err, out)
	return ""
}

// TestRunApply_AppliesARepresentativeProfile runs a profile covering
// every kind of setting a person legitimately puts in one, and asserts
// that git reads each value back out of the file "apply" wrote.
//
// It exists to stop a later tightening of the restricted key gate from
// quietly refusing ordinary settings. A gate is only useful if what it
// lets through still works, and a refusal that has to be opted back in
// to set a boolean is as much a defect as an acceptance, so this is the
// half of the gate's behaviour that needs pinning down end to end.
func TestRunApply_AppliesARepresentativeProfile(t *testing.T) {
	keys := map[string]string{
		"user.name":                  "Simon Ostendorf",
		"user.email":                 "simon@example.test",
		"user.signingKey":            "ssh-ed25519 AAAAEXAMPLE",
		"gpg.format":                 "ssh",
		"gpg.ssh.allowedSignersFile": "/home/simon/.signers",
		"commit.gpgsign":             "true",
		"tag.gpgsign":                "true",
		"core.autocrlf":              "input",
		"core.attributesFile":        "/home/simon/.gitattributes",
		"core.excludesFile":          "/home/simon/.gitignore",
		"core.commentString":         "//",
		"core.sparseCheckout":        "true",
		"core.sparseCheckoutCone":    "true",
		"core.longPaths":             "true",
		"core.fsync":                 "committed",
		"core.fsyncMethod":           "fsync",
		"core.preloadIndex":          "true",
		"core.splitIndex":            "false",
		"core.commitGraph":           "true",
		"core.multiPackIndex":        "true",
		"core.ignoreStat":            "false",
		"core.checkStat":             "default",
		"core.notesRef":              "refs/notes/commits",
		"core.deltaBaseCacheLimit":   "96m",
		"core.warnAmbiguousRefs":     "true",
		"core.maxTreeDepth":          "4096",
		"advice.ignoredHook":         "false",
		"advice.waitingForEditor":    "false",
		"column.clean":               "never",
		"color.pager":                "true",
		"difftool.prompt":            "false",
		"mergetool.prompt":           "false",
		"mergetool.keepBackup":       "false",
		"protocol.version":           "2",
		"credential.useHttpPath":     "true",
		"init.defaultBranch":         "main",
		"pull.rebase":                "true",
		"diff.algorithm":             "histogram",
		"merge.conflictStyle":        "zdiff3",
	}

	home := setupEnv(t)
	var b strings.Builder
	b.WriteString("profiles:\n  private:\n")
	for _, key := range sortedKeys(keys) {
		b.WriteString("    " + quoteYAML(key) + ": " + quoteYAML(keys[key]) + "\n")
	}
	b.WriteString("\nrules:\n  - host: github.com\n    owner: acme\n    profile: private\n")
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), b.String())

	var stdout, stderr bytes.Buffer
	if code := run([]string{"apply"}, &stdout, &stderr); code != 0 {
		t.Fatalf("apply = %d, want 0, stderr: %s", code, stderr.String())
	}

	path := filepath.Join(home, ".config", "gitcd", "profiles", "private.gitconfig")
	for _, key := range sortedKeys(keys) {
		got := gitConfigValue(t, path, key)
		if got != keys[key] {
			t.Errorf("git read %s = %q out of %s, want %q", key, got, path, keys[key])
		}
	}
}

// gitConfigValue returns what git itself reads back for key out of the
// config file at path.
func gitConfigValue(t *testing.T, path, key string) string {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "git", "config", "--file", path, "--get", key)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("git config --file %s --get %s: %v: %s", path, key, err, out)
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}

// sortedKeys returns the keys of m, sorted, so a table driven over it
// reads and fails in a stable order.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// profileAndRule renders a configuration whose single profile, and the
// single rule naming it, both carry name.
func profileAndRule(name string) string {
	return "profiles:\n  " + quoteYAML(name) + ":\n    user.email: simon@example.test\n" +
		"\nrules:\n  - host: github.com\n    profile: " + quoteYAML(name) + "\n"
}

// quoteYAML renders s as a double quoted YAML scalar, so a name holding
// a backslash or a dot reaches the parser exactly as written.
func quoteYAML(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

// renderedFiles lists every git configuration file anywhere under home,
// so a file written outside the managed directory is found wherever it
// landed instead of only being looked for where it belongs.
func renderedFiles(t *testing.T, home string) []string {
	t.Helper()

	var found []string
	err := filepath.WalkDir(home, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".gitconfig") {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", home, err)
	}
	return found
}
