// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// errSourceChanged is returned when the repository a command checked is no
// longer what sits at its path when the command is about to move it.
var errSourceChanged = errors.New("the path no longer holds the repository that was checked")

// sourceIdentity is what a command saw at a repository's path when it
// checked it: the directory itself and its ".git" entry, as the file system
// reports them. Comparing it again right before a move catches a path that
// was swapped for another directory, or for a symlink, in between.
type sourceIdentity struct {
	dir os.FileInfo
	git os.FileInfo
}

// captureSource records the identity of the repository at path. It does not
// follow a symlink at path's own last segment, so a link is never mistaken
// for the directory it points at.
func captureSource(path string) (sourceIdentity, error) {
	dir, err := os.Lstat(path)
	if err != nil {
		return sourceIdentity{}, err
	}
	gitEntry, err := os.Lstat(filepath.Join(path, ".git"))
	if err != nil {
		return sourceIdentity{}, err
	}
	// On Windows, the file identity behind os.SameFile is not read by
	// Lstat. It is read the first time os.SameFile is called, from the path
	// the info was made for, so an identity captured here would be read
	// after a replacement and match the replacement. Comparing each info
	// with itself reads it now.
	if !os.SameFile(dir, dir) || !os.SameFile(gitEntry, gitEntry) {
		return sourceIdentity{}, fmt.Errorf("read the file identity of %s", path)
	}
	return sourceIdentity{dir: dir, git: gitEntry}, nil
}

// verify reports an error wrapping errSourceChanged unless path still holds
// the very directory and ".git" entry s recorded.
func (s sourceIdentity) verify(path string) error {
	now, err := captureSource(path)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", errSourceChanged, path, err)
	}
	if !os.SameFile(s.dir, now.dir) || !os.SameFile(s.git, now.git) {
		return fmt.Errorf("%w: %s", errSourceChanged, path)
	}
	return nil
}

// renameNoReplace moves the directory src to dst and fails, with an error
// wrapping fs.ErrExist, when anything already sits at dst, instead of
// writing into or over it.
//
// The explicit check gives a typed error for the common case. Behind it,
// renaming a directory never replaces a file, and os.Rename itself refuses
// an existing directory at dst, one that is not empty because the system
// call does, an empty one because the standard library checks first.
//
// Remaining: the standard library's check and the system call are two
// steps, so an empty directory created at dst in between is replaced.
// Nothing is lost by that, the replaced directory holds nothing, and
// closing it would take a platform specific no replace rename the standard
// library does not offer.
func renameNoReplace(src, dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return &fs.PathError{Op: "rename", Path: dst, Err: fs.ErrExist}
	}
	return os.Rename(src, dst)
}

// uniqueSiblingName returns a path next to path that does not exist, with a
// random suffix, for a temporary name nothing else can predict.
func uniqueSiblingName(path, tag string) (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("create a temporary name next to %s: %w", path, err)
	}
	return path + "." + tag + "-" + hex.EncodeToString(buf[:]), nil
}
