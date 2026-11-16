// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
	"github.com/ostendorf-cloud/gitcd/internal/testenv"
)

// TestRunInit_GlobalKeyTextMatchesWhatGitResolves pins the text "init"
// prints about a global identity key to what git itself resolves for the
// same configuration: each case builds a real global file, the include
// file "apply" would render and a repository under a temporary home, asks
// git for the effective values after "init" ran, and expects the output
// to describe where each key sits.
func TestRunInit_GlobalKeyTextMatchesWhatGitResolves(t *testing.T) {
	const (
		globalEmail  = "jane@global.example.test"
		profileEmail = "jane@work.example.test"
		defaultEmail = "jane@default.example.test"
		localEmail   = "jane@pinned.example.test"
		globalName   = "Global Jane"
		profileName  = "Work Jane"
	)
	const (
		profileBoth     = "[user]\n\temail = " + profileEmail + "\n\tname = " + profileName + "\n"
		profileNameOnly = "[user]\n\tname = " + profileName + "\n"
		defaultEmailFl  = "[user]\n\temail = " + defaultEmail + "\n"
		includeProfile  = "[include]\n\tpath = {profile}\n"
		includeNowhere  = "[includeIf \"gitdir/i:/no/such/place/\"]\n\tpath = {profile}\n"
		includeDefault  = "[include]\n\tpath = {default}\n[include]\n\tpath = {profile}\n"
	)
	tests := []struct {
		name string
		// global is the global file, {include} standing for the include of
		// the file "apply" renders and {foreign} for an include path gitcd
		// did not write. Without {include}, "init" adds it.
		global      string
		includeFile string
		profileFile string
		defaultFile string
		localEmail  string
		wantExit    int
		// want holds the value git resolves per key, wantAfter whether the
		// key sits after an include section.
		want      map[string]string
		wantAfter map[string]bool
	}{
		{
			name:        "a profile sets the key and init adds the include after it",
			global:      "[user]\n\temail = " + globalEmail + "\n",
			includeFile: includeProfile,
			profileFile: profileBoth,
			want:        map[string]string{"user.email": profileEmail},
			wantAfter:   map[string]bool{"user.email": false},
		},
		{
			name:        "no profile reaches the repository",
			global:      "[user]\n\temail = " + globalEmail + "\n{include}",
			includeFile: includeNowhere,
			profileFile: profileBoth,
			want:        map[string]string{"user.email": globalEmail},
			wantAfter:   map[string]bool{"user.email": false},
		},
		{
			name:        "the profile leaves the key unset",
			global:      "[user]\n\temail = " + globalEmail + "\n{include}",
			includeFile: includeProfile,
			profileFile: profileNameOnly,
			want:        map[string]string{"user.email": globalEmail},
			wantAfter:   map[string]bool{"user.email": false},
		},
		{
			name:        "the rule profile leaves the key unset and the default profile sets it",
			global:      "[user]\n\temail = " + globalEmail + "\n{include}",
			includeFile: includeDefault,
			profileFile: profileNameOnly,
			defaultFile: defaultEmailFl,
			want:        map[string]string{"user.email": defaultEmail},
			wantAfter:   map[string]bool{"user.email": false},
		},
		{
			name:        "the repository carries its own value",
			global:      "[user]\n\temail = " + globalEmail + "\n{include}",
			includeFile: includeProfile,
			profileFile: profileBoth,
			localEmail:  localEmail,
			want:        map[string]string{"user.email": localEmail},
			wantAfter:   map[string]bool{"user.email": false},
		},
		{
			name:        "the key was appended after the gitcd include",
			global:      "{include}[user]\n\temail = " + globalEmail + "\n",
			includeFile: includeProfile,
			profileFile: profileBoth,
			want:        map[string]string{"user.email": globalEmail},
			wantAfter:   map[string]bool{"user.email": true},
		},
		{
			name:        "the key sits after an include gitcd did not write and init refuses",
			global:      "[include]\n\tpath = {foreign}\n[user]\n\temail = " + globalEmail + "\n",
			includeFile: includeProfile,
			profileFile: profileBoth,
			wantExit:    1,
			want:        map[string]string{"user.email": globalEmail},
			wantAfter:   map[string]bool{"user.email": true},
		},
		{
			name:        "one key sits before the include and the other after it",
			global:      "[user]\n\temail = " + globalEmail + "\n{include}[user]\n\tname = " + globalName + "\n",
			includeFile: includeProfile,
			profileFile: profileBoth,
			want:        map[string]string{"user.email": profileEmail, "user.name": globalName},
			wantAfter:   map[string]bool{"user.email": false, "user.name": true},
		},
		{
			name:        "the key sits between the gitcd include and a later foreign include",
			global:      "{include}[user]\n\temail = " + globalEmail + "\n[include]\n\tpath = {foreign}\n",
			includeFile: includeProfile,
			profileFile: profileBoth,
			want:        map[string]string{"user.email": globalEmail},
			wantAfter:   map[string]bool{"user.email": true},
		},
		{
			name:        "the key sits before the gitcd include and a foreign include follows",
			global:      "[user]\n\temail = " + globalEmail + "\n{include}[include]\n\tpath = {foreign}\n",
			includeFile: includeProfile,
			profileFile: profileBoth,
			want:        map[string]string{"user.email": profileEmail},
			wantAfter:   map[string]bool{"user.email": false},
		},
		{
			name:        "the key sits after a foreign include and before the gitcd include",
			global:      "[include]\n\tpath = {foreign}\n[user]\n\temail = " + globalEmail + "\n{include}",
			includeFile: includeProfile,
			profileFile: profileBoth,
			want:        map[string]string{"user.email": profileEmail},
			wantAfter:   map[string]bool{"user.email": false},
		},
		{
			name:        "the gitcd include appears twice and the key sits between them",
			global:      "{include}[user]\n\temail = " + globalEmail + "\n{include}",
			includeFile: includeProfile,
			profileFile: profileBoth,
			want:        map[string]string{"user.email": profileEmail},
			wantAfter:   map[string]bool{"user.email": false},
		},
		{
			name:        "one key sits before the gitcd include, the other between it and a foreign include",
			global:      "[user]\n\temail = " + globalEmail + "\n{include}[user]\n\tname = " + globalName + "\n[include]\n\tpath = {foreign}\n",
			includeFile: includeProfile,
			profileFile: profileBoth,
			want:        map[string]string{"user.email": profileEmail, "user.name": globalName},
			wantAfter:   map[string]bool{"user.email": false, "user.name": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := setupEnv(t)
			globalPath := globalConfigPath(home)
			profilePath := filepath.Join(home, "profile.gitconfig")
			defaultPath := filepath.Join(home, "default.gitconfig")
			expand := strings.NewReplacer(
				"{include}", "[include]\n\tpath = "+gitConfigEscaped(gitconfig.IncludePath(home))+"\n",
				"{foreign}", filepath.ToSlash(filepath.Join(home, "other.gitconfig")),
				"{profile}", filepath.ToSlash(profilePath),
				"{default}", filepath.ToSlash(defaultPath),
			)
			writeFile(t, globalPath, expand.Replace(tt.global))

			repo := filepath.Join(home, "repo")
			if err := os.MkdirAll(repo, 0o755); err != nil {
				t.Fatalf("create %s: %v", repo, err)
			}
			runGit(t, repo, "init", "--initial-branch=main")
			if tt.localEmail != "" {
				runGit(t, repo, "config", "user.email", tt.localEmail)
			}

			var stdout, stderr bytes.Buffer
			if code := run([]string{"init"}, &stdout, &stderr); code != tt.wantExit {
				t.Fatalf("init = %d, want %d, stderr: %s", code, tt.wantExit, stderr.String())
			}

			// "apply" renders the files the include reads; the test writes
			// them itself.
			writeFile(t, profilePath, tt.profileFile)
			if tt.defaultFile != "" {
				writeFile(t, defaultPath, tt.defaultFile)
			}
			writeFile(t, gitconfig.IncludePath(home), expand.Replace(tt.includeFile))

			for key, want := range tt.want {
				if got := testenv.GitValue(t, home, globalPath, repo, key); got != want {
					t.Fatalf("git resolves %s = %q, want %q", key, got, want)
				}
			}

			var before, after []string
			for _, key := range []string{"user.name", "user.email"} {
				isAfter, ok := tt.wantAfter[key]
				switch {
				case !ok:
				case isAfter:
					after = append(after, key)
				default:
					before = append(before, key)
				}
			}
			out := strings.Join(strings.Fields(stdout.String()), " ")
			want := []string{
				`"user.useConfigOnly" does not suppress a value configured here. Git takes the last value it reads.`,
				"A repository's own configuration beats the global file and every profile.",
			}
			notWant := []string{"every repository", "including one no rule matches", "leaves unset"}
			if len(before) > 0 {
				want = append(want,
					"Not after an include section: "+strings.Join(before, ", ")+". A profile that sets such a key wins when the gitcd include sits after it.",
					"Such a key still applies to a repository no profile reaches, or where no profile sets it.",
				)
			} else {
				notWant = append(notWant, "Not after an include section", "wins when the gitcd include sits after it")
			}
			if len(after) > 0 {
				want = append(want, "After an include section: "+strings.Join(after, ", ")+". Such a key wins over every profile.")
			} else {
				notWant = append(notWant, "After an include section")
			}
			for _, p := range want {
				if !strings.Contains(out, p) {
					t.Errorf("init stdout = %q, want it to contain %q", out, p)
				}
			}
			for _, p := range notWant {
				if strings.Contains(out, p) {
					t.Errorf("init stdout = %q, want it not to contain %q", out, p)
				}
			}
		})
	}
}

// TestRunInit_DoesNotSayAKeyBelongsInAProfileItRefuses covers the move
// target "init" names while it refuses it: the target is the
// defaultProfile, so the move stops, and the text must not promise the
// keys belong there.
func TestRunInit_DoesNotSayAKeyBelongsInAProfileItRefuses(t *testing.T) {
	home := setupEnv(t)
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), "profiles:\n  global:\n    user.name: Deliberate Name\ndefaultProfile: global\n")

	var stdout, stderr bytes.Buffer
	code := runInit([]string{"--move-identity=yes"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("init --move-identity=yes = 0, want a refusal for a defaultProfile target, stdout: %s", stdout.String())
	}
	out := strings.Join(strings.Fields(stdout.String()), " ")
	if !strings.Contains(out, `A move targets profile "global".`) {
		t.Errorf("init stdout = %q, want it to name the move target without promising it", out)
	}
	if strings.Contains(out, "belong in profile") {
		t.Errorf("init stdout = %q, want no claim that the keys belong in a profile the move refuses", out)
	}
	if !strings.Contains(stderr.String(), "defaultProfile") {
		t.Errorf("init stderr = %q, want it to name the defaultProfile conflict", stderr.String())
	}
}
