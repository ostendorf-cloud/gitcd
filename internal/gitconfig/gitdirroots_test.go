// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package gitconfig_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
)

// TestGitdirRoots_OnlyOneRootWhenNothingResolves covers a root that is no
// symlink but is spelled in a way that differs from its resolved path as a
// string, and a root a direct caller passes that is not absolute: neither
// gets a second block.
func TestGitdirRoots_OnlyOneRootWhenNothingResolves(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temporary directory: %v", err)
	}
	plain := filepath.Join(base, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatalf("create %s: %v", plain, err)
	}
	sep := string(filepath.Separator)

	tests := []struct {
		name string
		root string
	}{
		{name: "a trailing separator", root: plain + sep},
		{name: "a doubled separator", root: base + sep + sep + "plain"},
		{name: "a dot segment", root: filepath.Join(base, ".") + sep + "plain"},
		{name: "an empty root", root: ""},
		{name: "a relative root", root: "plain"},
		{name: "a dot root", root: "."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gitconfig.GitdirRoots(tt.root)
			if len(got) != 1 || got[0] != tt.root {
				t.Fatalf("GitdirRoots(%q) = %q, want only the root as given", tt.root, got)
			}
		})
	}
}

// TestRootResolution_RefusesWhatTheRootValidatorRefuses reuses
// config.ValidateRoot for the resolved path: a target the configured root
// could not be named would break the include file or widen a condition.
func TestRootResolution_RefusesWhatTheRootValidatorRefuses(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		wantErr bool
	}{
		{name: "a space", target: "my code"},
		{name: "non ASCII text", target: "Código-日本"},
		{name: "a double quote", target: `a"b`, wantErr: true},
		{name: "a line break", target: "a\nb", wantErr: true},
		{name: "a bracket expression", target: "[l]", wantErr: true},
		{name: "a wildcard", target: "a*b", wantErr: true},
		{name: "a question mark", target: "a?b", wantErr: true},
		{name: "a brace", target: "a{b}", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatalf("resolve the temporary directory: %v", err)
			}
			realRoot := filepath.Join(base, tt.target)
			if err := os.MkdirAll(realRoot, 0o755); err != nil {
				t.Skipf("the file system refuses the directory name %q: %v", tt.target, err)
			}
			linkRoot := filepath.Join(base, "Code")
			symlinkOrSkip(t, realRoot, linkRoot)

			resolved, err := gitconfig.RootResolution(linkRoot)
			if resolved != realRoot {
				t.Fatalf("RootResolution(%q) resolved = %q, want %q", linkRoot, resolved, realRoot)
			}
			if tt.wantErr {
				if !errors.Is(err, config.ErrInvalidProfile) {
					t.Fatalf("RootResolution(%q) error = %v, want config.ErrInvalidProfile", linkRoot, err)
				}
				if got := gitconfig.GitdirRoots(linkRoot); len(got) != 1 || got[0] != linkRoot {
					t.Fatalf("GitdirRoots(%q) = %q, want only the configured root", linkRoot, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("RootResolution(%q) returned an unexpected error: %v", linkRoot, err)
			}
			if got := gitconfig.GitdirRoots(linkRoot); len(got) != 2 || got[1] != realRoot {
				t.Fatalf("GitdirRoots(%q) = %q, want the link and %q", linkRoot, got, realRoot)
			}
		})
	}
}

func TestGitdirRoots(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temporary directory: %v", err)
	}
	realRoot := filepath.Join(base, "volume", "Code")
	linkRoot := filepath.Join(base, "Code")
	plain := filepath.Join(base, "plain")
	for _, dir := range []string{realRoot, plain} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	symlinkOrSkip(t, realRoot, linkRoot)

	tests := []struct {
		name string
		root string
		want []string
	}{
		{name: "plain directory", root: plain, want: []string{plain}},
		{name: "symlinked root", root: linkRoot, want: []string{linkRoot, realRoot}},
		{
			name: "missing directory below a symlink",
			root: filepath.Join(linkRoot, "new"),
			want: []string{filepath.Join(linkRoot, "new"), filepath.Join(realRoot, "new")},
		},
		{name: "missing directory", root: filepath.Join(plain, "new"), want: []string{filepath.Join(plain, "new")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gitconfig.GitdirRoots(tt.root)
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Fatalf("GitdirRoots(%q) = %q, want %q", tt.root, got, tt.want)
			}
		})
	}
}
