// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestMoveRepository(t *testing.T) {
	tests := []struct {
		name string
		// prepare may change the world after the source was captured and
		// returns the destination path.
		prepare func(t *testing.T, dir, oldPath string) (newPath string)
		wantErr error
		// wantMoved reports whether the repository ends up at newPath.
		wantMoved bool
	}{
		{
			name: "unchanged source moves",
			prepare: func(_ *testing.T, dir, _ string) string {
				return filepath.Join(dir, "dest")
			},
			wantMoved: true,
		},
		{
			name: "source replaced by another repository",
			prepare: func(t *testing.T, dir, oldPath string) string {
				if err := os.Rename(oldPath, filepath.Join(dir, "moved-away")); err != nil {
					t.Fatalf("rename: %v", err)
				}
				initRepository(t, oldPath)
				return filepath.Join(dir, "dest")
			},
			wantErr: errSourceChanged,
		},
		{
			name: "source replaced by a symlink",
			prepare: func(t *testing.T, dir, oldPath string) string {
				target := filepath.Join(dir, "moved-away")
				if err := os.Rename(oldPath, target); err != nil {
					t.Fatalf("rename: %v", err)
				}
				symlinkOrSkip(t, target, oldPath)
				return filepath.Join(dir, "dest")
			},
			wantErr: errSourceChanged,
		},
		{
			name: "source removed",
			prepare: func(t *testing.T, dir, oldPath string) string {
				if err := os.RemoveAll(oldPath); err != nil {
					t.Fatalf("remove: %v", err)
				}
				return filepath.Join(dir, "dest")
			},
			wantErr: errSourceChanged,
		},
		{
			name: "destination appeared as an empty directory",
			prepare: func(t *testing.T, dir, _ string) string {
				dest := filepath.Join(dir, "dest")
				if err := os.Mkdir(dest, 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				return dest
			},
			wantErr: fs.ErrExist,
		},
		{
			name: "destination appeared as a file",
			prepare: func(t *testing.T, dir, _ string) string {
				dest := filepath.Join(dir, "dest")
				writeFile(t, dest, "keep")
				return dest
			},
			wantErr: fs.ErrExist,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupEnv(t)
			dir := t.TempDir()
			oldPath := filepath.Join(dir, "src")
			initRepository(t, oldPath)

			source, err := captureSource(oldPath)
			if err != nil {
				t.Fatalf("captureSource: %v", err)
			}
			newPath := tt.prepare(t, dir, oldPath)

			err = moveRepository(context.Background(), oldPath, newPath, source)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("moveRepository() error = %v, want %v", err, tt.wantErr)
			}
			_, gitErr := os.Stat(filepath.Join(newPath, ".git"))
			if moved := gitErr == nil; moved != tt.wantMoved {
				t.Fatalf("repository at the destination = %v, want %v", moved, tt.wantMoved)
			}
			if tt.wantErr != nil && errors.Is(tt.wantErr, fs.ErrExist) {
				// The path that was already there stays what it was.
				if _, err := os.Stat(filepath.Join(oldPath, ".git")); err != nil {
					t.Fatalf("source lost its .git after a refused move: %v", err)
				}
			}
		})
	}
}

func TestRenameCaseOnly_RefusesWhenTheSourceNoLongerMatches(t *testing.T) {
	setupEnv(t)
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "Acme", "widget")
	newPath := filepath.Join(dir, "acme", "widget")
	initRepository(t, oldPath)

	expect, err := captureSource(oldPath)
	if err != nil {
		t.Fatalf("captureSource: %v", err)
	}

	// Another repository takes the place of the one that was checked.
	if err := os.Rename(filepath.Join(dir, "Acme"), filepath.Join(dir, "elsewhere")); err != nil {
		t.Fatalf("rename: %v", err)
	}
	initRepository(t, oldPath)

	_, _, moved, err := renameCaseOnly(oldPath, newPath, expect)

	if !errors.Is(err, errSourceChanged) {
		t.Fatalf("renameCaseOnly() error = %v, want errSourceChanged", err)
	}
	if moved {
		t.Fatal("renameCaseOnly() reported a move, want none")
	}
	if _, err := os.Stat(filepath.Join(oldPath, ".git")); err != nil {
		t.Fatalf("the substituted repository was touched: %v", err)
	}
	// Read back by exact entry name, so a case insensitive file system
	// does not answer for the old spelling.
	if entryExistsExactly(filepath.Dir(newPath)) {
		t.Fatalf("renameCaseOnly() created %s, want nothing renamed", filepath.Dir(newPath))
	}
}

func TestRenameCaseOnly_SecondHopAndRestoreFailNamesTheTemporaryDirectory(t *testing.T) {
	setupEnv(t)
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "Acme", "widget")
	newPath := filepath.Join(dir, "acme", "widget")
	initRepository(t, oldPath)
	expect, err := captureSource(oldPath)
	if err != nil {
		t.Fatalf("captureSource: %v", err)
	}

	errSecond := errors.New("second hop failed")
	errRestore := errors.New("restore failed")
	var temp string
	calls := 0
	// The first rename is real, the second and third fail.
	rename := func(src, dst string) error {
		calls++
		switch calls {
		case 1:
			temp = dst
			return renameNoReplace(src, dst)
		case 2:
			return errSecond
		default:
			return errRestore
		}
	}

	_, _, moved, err := renameCaseOnlyWith(rename, oldPath, newPath, expect)

	var left *errLeftAtTemporaryName
	if !errors.As(err, &left) {
		t.Fatalf("error = %v, want *errLeftAtTemporaryName", err)
	}
	if !errors.Is(err, errSecond) || !errors.Is(err, errRestore) {
		t.Fatalf("error = %v, want it to wrap both the second rename and the restore failure", err)
	}
	if left.Temp != temp || left.Old != filepath.Join(dir, "Acme") || left.New != filepath.Join(dir, "acme") {
		t.Fatalf("error paths = %+v, want temp %s and both names", left, temp)
	}
	if moved {
		t.Fatal("moved = true, want false")
	}
	if _, err := os.Stat(filepath.Join(temp, "widget", ".git")); err != nil {
		t.Fatalf("the repository is not at the temporary name %s: %v", temp, err)
	}
}

func TestRenameCaseOnly_RenamesAnUnchangedSource(t *testing.T) {
	setupEnv(t)
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "Acme", "widget")
	newPath := filepath.Join(dir, "acme", "widget")
	initRepository(t, oldPath)

	expect, err := captureSource(oldPath)
	if err != nil {
		t.Fatalf("captureSource: %v", err)
	}

	_, _, moved, err := renameCaseOnly(oldPath, newPath, expect)
	if err != nil {
		t.Fatalf("renameCaseOnly() error = %v", err)
	}
	if !moved {
		t.Fatal("renameCaseOnly() moved = false, want true")
	}
	if !entryExistsExactly(filepath.Join(dir, "acme")) {
		t.Fatalf("%s does not exist under its new spelling", filepath.Join(dir, "acme"))
	}
}
