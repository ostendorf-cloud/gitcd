// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package doctor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/doctor"
	"github.com/ostendorf-cloud/gitcd/internal/testenv"
)

// The phrases each part of a global identity key finding must carry. Each
// one describes a behaviour the cases below observe in git itself.
const (
	phraseCannotSuppress = `"user.useConfigOnly" cannot suppress it`
	phraseProfileWins    = "A profile that sets this key overrides it when the gitcd include comes after it"
	phraseNoProfileSets  = "a repository no profile reaches or where no profile sets this key"
	phraseAfterWins      = "It sits after an include section, so it wins over every profile"
)

// TestCheckGlobal_MessageMatchesWhatGitResolves pins the wording of the
// global identity key finding to what git itself resolves for the same
// configuration: each case builds a real global file, an include file and
// a repository under a temporary home, asks git for the effective values,
// and expects the message of each key to carry the phrases that describe
// where that key sits.
func TestCheckGlobal_MessageMatchesWhatGitResolves(t *testing.T) {
	const (
		globalEmail  = "jane@global.example.test"
		profileEmail = "jane@work.example.test"
		defaultEmail = "jane@default.example.test"
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
		// global is the global file, {include} standing for the section
		// that includes the include file and {foreign} for an include
		// path gitcd did not write.
		global      string
		includeFile string
		profileFile string
		defaultFile string
		// want holds the value git resolves per key, wantAfter whether the
		// key sits after an include section.
		want      map[string]string
		wantAfter map[string]bool
	}{
		{
			name:        "a profile sets the key and the key sits before the include",
			global:      "[user]\n\temail = " + globalEmail + "\n{include}",
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
			name:        "the key was appended after the include",
			global:      "{include}[user]\n\temail = " + globalEmail + "\n",
			includeFile: includeProfile,
			profileFile: profileBoth,
			want:        map[string]string{"user.email": globalEmail},
			wantAfter:   map[string]bool{"user.email": true},
		},
		{
			name:        "the key sits after an include gitcd did not write",
			global:      "[include]\n\tpath = {foreign}\n[user]\n\temail = " + globalEmail + "\n",
			includeFile: includeProfile,
			profileFile: profileBoth,
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
			// Isolates the "git init" below; the values under test are read
			// through testenv.GitValue with its own environment.
			setupGitEnv(t)
			home := t.TempDir()
			repo := filepath.Join(home, "repo")
			if err := os.MkdirAll(repo, 0o755); err != nil {
				t.Fatalf("create %s: %v", repo, err)
			}
			run(t, repo, "init", "--initial-branch=main")

			profilePath := filepath.Join(home, "profile.gitconfig")
			defaultPath := filepath.Join(home, "default.gitconfig")
			includePath := filepath.Join(home, "includes.gitconfig")
			foreignPath := filepath.Join(home, "other.gitconfig")
			globalPath := filepath.Join(home, "gitconfig-global")
			expand := strings.NewReplacer(
				"{include}", "[include]\n\tpath = "+filepath.ToSlash(includePath)+"\n",
				"{foreign}", filepath.ToSlash(foreignPath),
				"{profile}", filepath.ToSlash(profilePath),
				"{default}", filepath.ToSlash(defaultPath),
			)
			files := map[string]string{
				profilePath: tt.profileFile,
				includePath: expand.Replace(tt.includeFile),
				globalPath:  expand.Replace(tt.global),
			}
			if tt.defaultFile != "" {
				files[defaultPath] = tt.defaultFile
			}
			for path, content := range files {
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatalf("write %s: %v", path, err)
				}
			}

			for key, want := range tt.want {
				if got := testenv.GitValue(t, home, globalPath, repo, key); got != want {
					t.Fatalf("git resolves %s = %q, want %q", key, got, want)
				}
			}

			findings, err := doctor.CheckGlobal([]byte(files[globalPath]), globalPath, includePath, "global", true)
			if err != nil {
				t.Fatalf("CheckGlobal() returned an unexpected error: %v", err)
			}

			// The order finding names exactly the keys the key messages call
			// after an include.
			var afterKeys []string
			for _, key := range []string{"user.name", "user.email"} {
				if tt.wantAfter[key] {
					afterKeys = append(afterKeys, key)
				}
			}
			order, hasOrder := findKind(findings, doctor.KindGlobalIdentityOrder)
			if hasOrder != (len(afterKeys) > 0) {
				t.Fatalf("CheckGlobal() = %+v, want an order finding only when a key sits after the include (keys %v)", findings, afterKeys)
			}
			for _, key := range afterKeys {
				if !strings.Contains(order.Message, key) {
					t.Errorf("order finding = %q, want it to name %s", order.Message, key)
				}
			}
			if hasOrder {
				for key, after := range tt.wantAfter {
					if !after && strings.Contains(order.Message, key) {
						t.Errorf("order finding = %q, want it not to name %s", order.Message, key)
					}
				}
			}
			for key, after := range tt.wantAfter {
				var msg string
				for _, f := range findings {
					if f.Kind == doctor.KindGlobalIdentityKey && strings.Contains(f.Message, " "+key+" = ") {
						msg = f.Message
					}
				}
				if msg == "" {
					t.Fatalf("CheckGlobal() = %+v, want a %s finding for %s", findings, doctor.KindGlobalIdentityKey, key)
				}
				wantPhrases := []string{phraseCannotSuppress, phraseProfileWins, phraseNoProfileSets}
				notPhrases := []string{phraseAfterWins}
				if after {
					wantPhrases, notPhrases = []string{phraseCannotSuppress, phraseAfterWins}, []string{phraseProfileWins, phraseNoProfileSets}
				}
				for _, p := range wantPhrases {
					if !strings.Contains(msg, p) {
						t.Errorf("finding for %s = %q, want it to contain %q", key, msg, p)
					}
				}
				for _, p := range notPhrases {
					if strings.Contains(msg, p) {
						t.Errorf("finding for %s = %q, want it not to contain %q", key, msg, p)
					}
				}
				for _, claim := range []string{"every repository", "including one no rule matches", "leaves unset"} {
					if strings.Contains(msg, claim) {
						t.Errorf("finding for %s = %q, want no claim %q", key, msg, claim)
					}
				}
			}
		})
	}
}
