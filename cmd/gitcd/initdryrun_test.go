// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// loadErrorPath matches the path a configuration load error names. The dry
// run validates in a scratch directory and the real run next to the file, so
// the two differ there by design.
var loadErrorPath = regexp.MustCompile(`config: parse \S+:`)

// normalizeInitStderr makes a run's stderr comparable with the run in the
// other home.
func normalizeInitStderr(stderr, home string) string {
	stderr = strings.ReplaceAll(stderr, home, "<home>")
	return loadErrorPath.ReplaceAllString(stderr, "config: parse <path>:")
}

func TestRunInit_DryRunMatchesTheRealRunWhenAMoveChangesTheOutcome(t *testing.T) {
	const (
		foreignInclude = "[include]\n\tpath = /some/other/include.gitconfig\n"
		identity       = "[user]\n\tname = Jane Doe\n\temail = jane@example.test\n"
		brokenSection  = "[broken\n"
		unknownKeyYAML = "totallyUnknownKey: 1\nprofiles:\n  work:\n    user.email: work@example.test\n"
	)
	strPtr := func(s string) *string { return &s }

	tests := []struct {
		name     string
		global   *string
		yaml     *string
		wantCode int
		// wantAdded reports whether both runs add the include.
		wantAdded      bool
		skipStderrDiff bool
	}{
		{
			name:      "identity keys after a foreign include are moved away first",
			global:    strPtr(foreignInclude + identity),
			wantCode:  0,
			wantAdded: true,
		},
		{
			name:      "identity keys after a foreign include with a gitcd configuration file",
			global:    strPtr(foreignInclude + identity),
			yaml:      strPtr("profiles:\n  work:\n    user.email: work@example.test\n"),
			wantCode:  0,
			wantAdded: true,
		},
		{
			name:     "gitcd configuration file with an unknown key fails validation",
			global:   strPtr(identity),
			yaml:     strPtr(unknownKeyYAML),
			wantCode: 1,
		},
		{
			name:     "unknown key together with a foreign include",
			global:   strPtr(foreignInclude + identity),
			yaml:     strPtr(unknownKeyYAML),
			wantCode: 1,
		},
		{
			// The real run reports the configuration first and never reaches
			// the global file.
			name:     "unknown key and a global file git cannot edit",
			global:   strPtr(identity + brokenSection),
			yaml:     strPtr(unknownKeyYAML),
			wantCode: 1,
		},
		{
			// The git error names a different file in each run, so only the
			// exit code is compared.
			name:           "global file git cannot edit",
			global:         strPtr(identity + brokenSection),
			wantCode:       1,
			skipStderrDiff: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := []string{"--move-identity=yes"}

			dryHome := initBackupEnv(t, tt.global, tt.yaml)
			// The preview keeps copies of identity files in the temporary
			// directory while it runs, so it must leave that directory empty.
			scratchRoot := filepath.Join(t.TempDir(), "tmp")
			if err := os.Mkdir(scratchRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
				t.Setenv(name, scratchRoot)
			}
			var dryOut, dryErr bytes.Buffer
			dryCode := runInit(append([]string{"--dry-run"}, args...), strings.NewReader(""), &dryOut, &dryErr)
			if left, err := os.ReadDir(scratchRoot); err != nil || len(left) != 0 {
				t.Fatalf("init --dry-run left %d entries in the temporary directory, err %v", len(left), err)
			}
			if onDisk := backupsOnDisk(t, dryHome); len(onDisk) != 0 {
				t.Fatalf("init --dry-run wrote backups for %v, want none", onDisk)
			}
			if got := readFileString(t, filepath.Join(dryHome, "gitconfig-global")); got != *tt.global {
				t.Fatalf("init --dry-run changed the global configuration:\n%s", got)
			}

			realHome := initBackupEnv(t, tt.global, tt.yaml)
			var realOut, realErr bytes.Buffer
			realCode := runInit(args, strings.NewReader(""), &realOut, &realErr)

			if realCode != tt.wantCode {
				t.Fatalf("init = %d, want %d, stderr: %s", realCode, tt.wantCode, realErr.String())
			}
			if dryCode != realCode {
				t.Fatalf("init --dry-run = %d, real run = %d\ndry stderr: %s\nreal stderr: %s", dryCode, realCode, dryErr.String(), realErr.String())
			}
			if dry, real := normalizeInitStderr(dryErr.String(), dryHome), normalizeInitStderr(realErr.String(), realHome); !tt.skipStderrDiff && dry != real {
				t.Fatalf("stderr differs\ndry run:\n%s\nreal run:\n%s", dry, real)
			}
			if added := strings.Contains(dryOut.String(), "would add to"); added != tt.wantAdded {
				t.Fatalf("init --dry-run announced the include = %v, want %v\n%s", added, tt.wantAdded, dryOut.String())
			}
			if added := strings.Contains(realOut.String(), "added include.path"); added != tt.wantAdded {
				t.Fatalf("init added the include = %v, want %v\n%s", added, tt.wantAdded, realOut.String())
			}
		})
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
