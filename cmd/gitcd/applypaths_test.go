// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
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
