// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
	"github.com/ostendorf-cloud/gitcd/internal/testenv"
)

// initBackupEnv gives the test a fresh home with a global git configuration
// of its own, outside the machine's, and returns the home. A nil
// globalContent leaves the file absent. The placeholder {include} in
// globalContent stands for the include file path gitcd renders for this home.
func initBackupEnv(t *testing.T, globalContent *string, yamlContent *string) string {
	t.Helper()
	home := t.TempDir()
	testenv.SetHome(t, home)
	globalPath := filepath.Join(home, "gitconfig-global")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", globalPath)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if globalContent != nil {
		content := strings.ReplaceAll(*globalContent, "{include}", gitconfig.IncludePath(home))
		writeFile(t, globalPath, content)
	}
	if yamlContent != nil {
		writeFile(t, filepath.Join(home, ".gitcd.yaml"), *yamlContent)
	}
	return home
}

// backupsAnnounced returns the originals a run's stdout says it backs up,
// relative to home, for both the dry run wording and the real run wording.
func backupsAnnounced(t *testing.T, stdout, home string) []string {
	t.Helper()
	var got []string
	for _, line := range strings.Split(stdout, "\n") {
		var path string
		switch {
		case strings.HasPrefix(line, "would back up "):
			path = strings.TrimPrefix(line, "would back up ")
		case strings.HasPrefix(line, "backed up "):
			path, _, _ = strings.Cut(strings.TrimPrefix(line, "backed up "), " to ")
		default:
			continue
		}
		rel, err := filepath.Rel(home, path)
		if err != nil {
			t.Fatalf("announced path %q is not under %s: %v", path, home, err)
		}
		got = append(got, filepath.ToSlash(rel))
	}
	slices.Sort(got)
	return got
}

// backupsOnDisk returns the originals, relative to home, that have a backup
// file beside them.
func backupsOnDisk(t *testing.T, home string) []string {
	t.Helper()
	var got []string
	err := filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if original, _, found := strings.Cut(path, ".gitcd-backup-"); found {
			rel, err := filepath.Rel(home, original)
			if err != nil {
				return err
			}
			got = append(got, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", home, err)
	}
	slices.Sort(got)
	return got
}

func TestRunInit_DryRunAnnouncesABackupExactlyWhenTheRealRunMakesOne(t *testing.T) {
	const (
		plainGlobal    = "[core]\n\teditor = vi\n"
		identityGlobal = "[user]\n\tname = Jane Doe\n\temail = jane@example.test\n"
		emptyContent   = ""
		gitcdYAML      = "profiles:\n  work:\n    user.email: work@example.test\nrules:\n  - host: github.com\n    profile: work\n"
	)
	strPtr := func(s string) *string { return &s }

	tests := []struct {
		name   string
		args   []string
		global *string
		yaml   *string
		// wantCode is the exit code of both runs.
		wantCode int
		// want lists the originals that get a backup, relative to home.
		want []string
	}{
		{
			name:   "global configuration already includes gitcd",
			global: strPtr("[include]\n\tpath = {include}\n"),
			want:   nil,
		},
		{
			name: "identity key after an existing include section is refused",
			global: strPtr("[include]\n\tpath = /some/other/include.gitconfig\n" +
				"[user]\n\tsigningkey = ssh-ed25519 AAAATRAILING\n"),
			wantCode: 1,
			want:     nil,
		},
		{
			name:   "global configuration with only whitespace",
			global: strPtr("\n  \n"),
			want:   []string{"gitconfig-global"},
		},
		{
			name:   "global configuration absent",
			global: nil,
			want:   nil,
		},
		{
			name:   "global configuration empty",
			global: strPtr(emptyContent),
			want:   nil,
		},
		{
			name:   "global configuration with content",
			global: strPtr(plainGlobal),
			want:   []string{"gitconfig-global"},
		},
		{
			name:   "move with no gitcd configuration file yet",
			args:   []string{"--move-identity=yes"},
			global: strPtr(identityGlobal),
			yaml:   nil,
			want:   []string{"gitconfig-global"},
		},
		{
			name:   "move with an empty gitcd configuration file",
			args:   []string{"--move-identity=yes"},
			global: strPtr(identityGlobal),
			yaml:   strPtr(emptyContent),
			want:   []string{"gitconfig-global"},
		},
		{
			name:   "move with a gitcd configuration file with content",
			args:   []string{"--move-identity=yes"},
			global: strPtr(identityGlobal),
			yaml:   strPtr(gitcdYAML),
			want:   []string{".gitcd.yaml", "gitconfig-global"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Dry run first, in a home of its own.
			dryHome := initBackupEnv(t, tt.global, tt.yaml)
			var dryOut, dryErr bytes.Buffer
			code := runInit(append([]string{"--dry-run"}, tt.args...), strings.NewReader(""), &dryOut, &dryErr)
			if code != tt.wantCode {
				t.Fatalf("init --dry-run = %d, want %d, stderr: %s", code, tt.wantCode, dryErr.String())
			}
			if onDisk := backupsOnDisk(t, dryHome); len(onDisk) != 0 {
				t.Fatalf("init --dry-run wrote backups for %v, want none", onDisk)
			}
			announced := backupsAnnounced(t, dryOut.String(), dryHome)

			// The real run, in a second home built the same way.
			realHome := initBackupEnv(t, tt.global, tt.yaml)
			var realOut, realErr bytes.Buffer
			code = runInit(tt.args, strings.NewReader(""), &realOut, &realErr)
			if code != tt.wantCode {
				t.Fatalf("init = %d, want %d, stderr: %s", code, tt.wantCode, realErr.String())
			}
			made := backupsOnDisk(t, realHome)

			if !slices.Equal(made, tt.want) {
				t.Fatalf("init made backups of %v, want %v", made, tt.want)
			}
			if !slices.Equal(announced, made) {
				t.Fatalf("init --dry-run announced backups of %v, real run made %v\ndry run output:\n%s", announced, made, dryOut.String())
			}
		})
	}
}
