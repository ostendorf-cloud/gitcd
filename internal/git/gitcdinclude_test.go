// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package git_test

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/git"
	"github.com/ostendorf-cloud/gitcd/internal/testenv"
)

func TestIdentityKeysAfterGitcdInclude(t *testing.T) {
	const (
		gitcd     = "[include]\n\tpath = /home/jane/.config/gitcd/includes.gitconfig\n"
		foreign   = "[include]\n\tpath = /home/jane/other.gitconfig\n"
		email     = "[user]\n\temail = jane@global.example.test\n"
		name      = "[user]\n\tname = Jane Doe\n"
		includeAt = "/home/jane/.config/gitcd/includes.gitconfig"
	)
	tests := []struct {
		name        string
		content     string
		includePath string
		want        []string
	}{
		{
			name:        "a key after the gitcd include",
			content:     gitcd + email,
			includePath: includeAt,
			want:        []string{"user.email"},
		},
		{
			name:        "a key between the gitcd include and a later foreign include",
			content:     gitcd + email + foreign,
			includePath: includeAt,
			want:        []string{"user.email"},
		},
		{
			name:        "a key before the gitcd include and a foreign include after it",
			content:     email + gitcd + foreign,
			includePath: includeAt,
		},
		{
			name:        "a key after a foreign include and before the gitcd include",
			content:     foreign + email + gitcd,
			includePath: includeAt,
		},
		{
			name:        "the gitcd include twice with the key between them",
			content:     gitcd + email + gitcd,
			includePath: includeAt,
		},
		{
			name:        "the gitcd include twice with the key after both",
			content:     gitcd + gitcd + email,
			includePath: includeAt,
			want:        []string{"user.email"},
		},
		{
			name:        "one key before and one between the gitcd include and a foreign include",
			content:     email + gitcd + name + foreign,
			includePath: includeAt,
			want:        []string{"user.name"},
		},
		{
			name:        "a key after a foreign include when no gitcd include exists",
			content:     foreign + email,
			includePath: includeAt,
			want:        []string{"user.email"},
		},
		{
			name:        "an empty include path leaves the last include as the line",
			content:     gitcd + email + foreign,
			includePath: "",
		},
		{
			name:        "a Windows path git wrote with doubled backslashes",
			content:     "[include]\n\tpath = C:\\\\Users\\\\jane\\\\includes.gitconfig\n" + email + foreign,
			includePath: `C:\Users\jane\includes.gitconfig`,
			want:        []string{"user.email"},
		},
		{
			name:        "a Windows path written with forward slashes",
			content:     "[include]\n\tpath = C:/Users/jane/includes.gitconfig\n" + email + foreign,
			includePath: `C:\Users\jane\includes.gitconfig`,
			want:        []string{"user.email"},
		},
		{
			name:        "a Windows path written natively",
			content:     "[include]\n\tpath = C:\\Users\\jane\\includes.gitconfig\n" + email + foreign,
			includePath: `C:\Users\jane\includes.gitconfig`,
			want:        []string{"user.email"},
		},
		{
			name:        "a quoted value",
			content:     "[include]\n\tpath = \"" + includeAt + "\"\n" + email + foreign,
			includePath: includeAt,
			want:        []string{"user.email"},
		},
		{
			name:        "a quoted Windows value with doubled backslashes",
			content:     "[include]\n\tpath = \"C:\\\\Users\\\\jane\\\\includes.gitconfig\"\n" + email + foreign,
			includePath: `C:\Users\jane\includes.gitconfig`,
			want:        []string{"user.email"},
		},
		{
			name:        "a value followed by a semicolon comment",
			content:     "[include]\n\tpath = " + includeAt + " ; rendered by gitcd\n" + email + foreign,
			includePath: includeAt,
			want:        []string{"user.email"},
		},
		{
			name:        "a value followed by a hash comment",
			content:     "[include]\n\tpath = " + includeAt + " # rendered by gitcd\n" + email + foreign,
			includePath: includeAt,
			want:        []string{"user.email"},
		},
		{
			name:        "a quoted value followed by a comment",
			content:     "[include]\n\tpath = \"" + includeAt + "\" ; rendered by gitcd\n" + email + foreign,
			includePath: includeAt,
			want:        []string{"user.email"},
		},
		{
			name:        "a later foreign path that has the gitcd path as a prefix",
			content:     gitcd + email + "[include]\n\tpath = " + includeAt + ".old\n",
			includePath: includeAt,
			want:        []string{"user.email"},
		},
		{
			name:        "a comment naming the gitcd path on a foreign include",
			content:     gitcd + email + "[include]\n\tpath = /home/jane/other.gitconfig ; was " + includeAt + "\n",
			includePath: includeAt,
			want:        []string{"user.email"},
		},
		{
			name:        "a path key outside an include section does not count",
			content:     "[core]\n\tpath = " + includeAt + "\n" + foreign + email,
			includePath: includeAt,
			want:        []string{"user.email"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := git.IdentityKeysAfterGitcdInclude([]byte(tt.content), git.IdentityKeys, tt.includePath)
			if err != nil {
				t.Fatalf("IdentityKeysAfterGitcdInclude() returned an unexpected error: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("IdentityKeysAfterGitcdInclude() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIdentityKeysAfterGitcdInclude_HandEditedForms covers the two forms a
// person writes by hand: an "[includeIf]" section naming the include file
// and a "~/" path. The home directory is a string, so a Windows form runs
// on every system.
func TestIdentityKeysAfterGitcdInclude_HandEditedForms(t *testing.T) {
	const (
		email   = "[user]\n\temail = jane@global.example.test\n"
		foreign = "[include]\n\tpath = /home/jane/other.gitconfig\n"
		bare    = "[include]\n\tpath = /home/jane/.config/gitcd/includes.gitconfig\n"
		unixInc = "/home/jane/.config/gitcd/includes.gitconfig"
		winInc  = `C:\Users\jane\.config\gitcd\includes.gitconfig`
		cond    = "[includeIf \"gitdir:/home/jane/work/\"]\n"
	)
	tests := []struct {
		name        string
		home        string
		content     string
		includePath string
		want        []string
		wantNamed   bool
	}{
		{
			name:        "a tilde path with the key after it",
			home:        "/home/jane",
			content:     "[include]\n\tpath = ~/.config/gitcd/includes.gitconfig\n" + email + foreign,
			includePath: unixInc,
			want:        []string{"user.email"},
			wantNamed:   true,
		},
		{
			name:        "a quoted tilde path with a comment",
			home:        "/home/jane",
			content:     "[include]\n\tpath = \"~/.config/gitcd/includes.gitconfig\" ; gitcd\n" + email + foreign,
			includePath: unixInc,
			want:        []string{"user.email"},
			wantNamed:   true,
		},
		{
			name:        "a tilde path with the key before it",
			home:        "/home/jane",
			content:     email + "[include]\n\tpath = ~/.config/gitcd/includes.gitconfig\n" + foreign,
			includePath: unixInc,
			wantNamed:   true,
		},
		{
			name:        "a tilde path under a home directory with a trailing slash",
			home:        "/home/jane/",
			content:     "[include]\n\tpath = ~/.config/gitcd/includes.gitconfig\n" + email + foreign,
			includePath: unixInc,
			want:        []string{"user.email"},
			wantNamed:   true,
		},
		{
			name:        "a tilde path on Windows",
			home:        `C:\Users\jane`,
			content:     "[include]\n\tpath = ~/.config/gitcd/includes.gitconfig\n" + email + foreign,
			includePath: winInc,
			want:        []string{"user.email"},
			wantNamed:   true,
		},
		{
			name:        "a tilde path to another file under the home directory",
			home:        "/home/jane",
			content:     "[include]\n\tpath = ~/.config/gitcd/other.gitconfig\n" + email,
			includePath: unixInc,
			want:        []string{"user.email"},
		},
		{
			// Not the gitcd include, so the foreign include after the key is
			// the line and the key is not after it.
			name:        "a tilde path for another home directory",
			home:        "/home/john",
			content:     "[include]\n\tpath = ~/.config/gitcd/includes.gitconfig\n" + email + foreign,
			includePath: unixInc,
		},
		{
			name:        "a tilde user path is not expanded",
			home:        "/home/jane",
			content:     "[include]\n\tpath = ~jane/.config/gitcd/includes.gitconfig\n" + email + foreign,
			includePath: unixInc,
		},
		{
			name:        "an includeIf with the key after it",
			home:        "/home/jane",
			content:     cond + "\tpath = " + unixInc + "\n" + email + foreign,
			includePath: unixInc,
			want:        []string{"user.email"},
			wantNamed:   true,
		},
		{
			name:        "an includeIf with the key before it",
			home:        "/home/jane",
			content:     email + cond + "\tpath = " + unixInc + "\n" + foreign,
			includePath: unixInc,
			wantNamed:   true,
		},
		{
			name:        "an includeIf with a tilde path and the key after it",
			home:        "/home/jane",
			content:     cond + "\tpath = ~/.config/gitcd/includes.gitconfig\n" + email,
			includePath: unixInc,
			want:        []string{"user.email"},
			wantNamed:   true,
		},
		{
			name:        "an includeIf on Windows with doubled backslashes",
			home:        `C:\Users\jane`,
			content:     "[includeIf \"gitdir:C:/Users/jane/work/\"]\n\tpath = C:\\\\Users\\\\jane\\\\.config\\\\gitcd\\\\includes.gitconfig\n" + email,
			includePath: winInc,
			want:        []string{"user.email"},
			wantNamed:   true,
		},
		{
			name:        "an includeIf after a bare gitcd include with the key between them",
			home:        "/home/jane",
			content:     bare + email + cond + "\tpath = " + unixInc + "\n",
			includePath: unixInc,
			want:        []string{"user.email"},
			wantNamed:   true,
		},
		{
			name:        "an includeIf on a branch after a bare gitcd include with the key between them",
			home:        "/home/jane",
			content:     bare + email + "[includeIf \"onbranch:release\"]\n\tpath = " + unixInc + "\n",
			includePath: unixInc,
			want:        []string{"user.email"},
			wantNamed:   true,
		},
		{
			// Over-warns where the later condition holds, since the profile
			// loads again there. The safe side.
			name:        "an includeIf after another includeIf with the key between them",
			home:        "/home/jane",
			content:     cond + "\tpath = " + unixInc + "\n" + email + "[includeIf \"gitdir:/home/jane/play/\"]\n\tpath = " + unixInc + "\n",
			includePath: unixInc,
			want:        []string{"user.email"},
			wantNamed:   true,
		},
		{
			name:        "a bare gitcd include after an includeIf with the key between them",
			home:        "/home/jane",
			content:     cond + "\tpath = " + unixInc + "\n" + email + bare,
			includePath: unixInc,
			wantNamed:   true,
		},
		{
			name:        "an includeIf section written in lower case",
			home:        "/home/jane",
			content:     "[includeif \"gitdir:/home/jane/work/\"]\n\tpath = " + unixInc + "\n" + email + foreign,
			includePath: unixInc,
			want:        []string{"user.email"},
			wantNamed:   true,
		},
		{
			name:        "an includeIf section written in mixed case",
			home:        "/home/jane",
			content:     "[IncludeIF \"gitdir:/home/jane/work/\"]\n\tpath = " + unixInc + "\n" + email + foreign,
			includePath: unixInc,
			want:        []string{"user.email"},
			wantNamed:   true,
		},
		{
			name:        "an includeIf naming a foreign file",
			home:        "/home/jane",
			content:     cond + "\tpath = /home/jane/other.gitconfig\n" + email,
			includePath: unixInc,
		},
		{
			name:        "a path key in an unrelated section named like an includeIf subsection",
			home:        "/home/jane",
			content:     "[includeIfNot \"x\"]\n\tpath = " + unixInc + "\n" + foreign + email,
			includePath: unixInc,
			want:        []string{"user.email"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.SetHome(t, tt.home)
			got, err := git.IdentityKeysAfterGitcdInclude([]byte(tt.content), git.IdentityKeys, tt.includePath)
			if err != nil {
				t.Fatalf("IdentityKeysAfterGitcdInclude() returned an unexpected error: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("IdentityKeysAfterGitcdInclude() = %v, want %v", got, tt.want)
			}
			if named := git.NamesGitcdInclude([]byte(tt.content), tt.includePath); named != tt.wantNamed {
				t.Errorf("NamesGitcdInclude() = %v, want %v", named, tt.wantNamed)
			}
		})
	}
}

// TestIdentityKeysAfterGitcdInclude_EquivalentSpellings covers a path that
// names the include file through a ".." element or a symlink. Each case
// builds real files, since the match asks the file system.
func TestIdentityKeysAfterGitcdInclude_EquivalentSpellings(t *testing.T) {
	const (
		email   = "[user]\n\temail = jane@global.example.test\n"
		foreign = "[include]\n\tpath = /somewhere/else.gitconfig\n"
	)
	tests := []struct {
		name string
		// path is the include path as written, {home} standing for the
		// home directory, {real} for the directory the files live in and
		// {link} for a symlink to it.
		path string
		// homeIsLink makes the symlink the home directory, so includePath
		// is spelled through it.
		homeIsLink bool
		symlink    bool
		noFile     bool
		want       []string
	}{
		{
			name: "a tilde path with a dot dot element",
			path: "~/.config/gitcd/../gitcd/includes.gitconfig",
			want: []string{"user.email"},
		},
		{
			name: "an absolute path with a dot dot element",
			path: "{real}/.config/gitcd/../gitcd/includes.gitconfig",
			want: []string{"user.email"},
		},
		{
			name:       "an absolute path through a symlink while the home directory is the symlink",
			path:       "{real}/.config/gitcd/includes.gitconfig",
			homeIsLink: true,
			symlink:    true,
			want:       []string{"user.email"},
		},
		{
			name:    "an absolute path through a symlinked directory",
			path:    "{link}/.config/gitcd/includes.gitconfig",
			symlink: true,
			want:    []string{"user.email"},
		},
		{
			name: "another file in the same directory",
			path: "{real}/.config/gitcd/other.gitconfig",
		},
		{
			name:   "a dot dot path while the include file does not exist",
			path:   "~/.config/gitcd/../gitcd/includes.gitconfig",
			noFile: true,
		},
		{
			name: "a relative path",
			path: "../gitcd/includes.gitconfig",
		},
		{
			name: "a tilde user path",
			path: "~jane/.config/gitcd/includes.gitconfig",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.symlink && runtime.GOOS == "windows" {
				t.Skip("creating a symlink needs a privilege on Windows")
			}
			base := t.TempDir()
			dir := filepath.Join(base, ".config", "gitcd")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("create %s: %v", dir, err)
			}
			for _, name := range []string{"includes.gitconfig", "other.gitconfig"} {
				if tt.noFile && name == "includes.gitconfig" {
					continue
				}
				if err := os.WriteFile(filepath.Join(dir, name), []byte("[user]\n"), 0o644); err != nil {
					t.Fatalf("write %s: %v", name, err)
				}
			}
			home := base
			link := filepath.Join(t.TempDir(), "link")
			if tt.symlink {
				if err := os.Symlink(base, link); err != nil {
					t.Fatalf("symlink %s: %v", base, err)
				}
			}
			if tt.homeIsLink {
				home = link
			}
			testenv.SetHome(t, home)
			includePath := filepath.Join(home, ".config", "gitcd", "includes.gitconfig")
			path := strings.NewReplacer(
				"{real}", filepath.ToSlash(base),
				"{link}", filepath.ToSlash(link),
			).Replace(tt.path)
			content := "[include]\n\tpath = " + path + "\n" + email + foreign

			got, err := git.IdentityKeysAfterGitcdInclude([]byte(content), git.IdentityKeys, includePath)
			if err != nil {
				t.Fatalf("IdentityKeysAfterGitcdInclude() returned an unexpected error: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("IdentityKeysAfterGitcdInclude() = %v, want %v", got, tt.want)
			}
			if named := git.NamesGitcdInclude([]byte(content), includePath); named != (tt.want != nil) {
				t.Errorf("NamesGitcdInclude() = %v, want %v", named, tt.want != nil)
			}
		})
	}
}
