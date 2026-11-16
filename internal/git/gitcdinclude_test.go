// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package git_test

import (
	"slices"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/git"
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
