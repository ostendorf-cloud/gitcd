// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
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
