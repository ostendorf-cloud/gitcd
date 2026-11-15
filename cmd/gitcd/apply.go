// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
)

// runApply implements "gitcd apply": it renders every profile file and the
// include file "gitcd init" points the user's global git configuration at,
// and writes them. "--dry-run" prints the rendered files instead of
// writing them.
func runApply(args []string, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("gitcd apply", flag.ContinueOnError)
	fset.SetOutput(stderr)
	dryRun := fset.Bool("dry-run", false, "print the rendered files without writing them")
	verbose := fset.Bool("verbose", false, "enable debug logging on stderr")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() != 0 {
		fmt.Fprintln(stderr, "gitcd apply: no arguments are accepted")
		fset.Usage()
		return 2
	}
	logger := newLogger(*verbose, stderr)

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd apply: %v\n", err)
		return 1
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd apply: %v\n", err)
		return 1
	}

	rendered, err := gitconfig.Render(cfg, home)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd apply: %v\n", err)
		return 1
	}

	// A resolved root git would match but gitcd cannot render is a warning,
	// not a failure: the configured root's conditions are still written.
	if resolved, err := gitconfig.RootResolution(cfg.Root); err != nil {
		fmt.Fprintf(stderr, "gitcd apply: warning: root %q reaches %q through a symlink and no condition is rendered for it: %v\n",
			cfg.Root, resolved, err)
	}

	if *dryRun {
		paths := make([]string, 0, len(rendered))
		for path := range rendered {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			fmt.Fprintf(stdout, "%s:\n%s\n", path, rendered[path])
		}
		return 0
	}

	if err := writeRenderedFiles(rendered, gitconfig.Dir(home), logger, stdout); err != nil {
		fmt.Fprintf(stderr, "gitcd apply: %v\n", err)
		return 1
	}
	return 0
}

const (
	// managedDirMode is the mode of the managed directory and of every
	// directory below it. The files in it are git configuration naming
	// identities, signing keys and keys that change what git runs, so only
	// the user needs access.
	managedDirMode = 0o700
	// managedFileMode is the mode of every file under the managed directory.
	managedFileMode = 0o600
)

// writeRenderedFiles writes every path in rendered, creating its parent
// directory first, in a deterministic order: the mechanical write both
// "gitcd apply" and "gitcd doctor --fix" perform, kept in one place so
// the two never drift into two different ways of doing the same write.
//
// Directories are created 0700 and files 0600. A directory or file an
// earlier gitcd version already wrote wider, 0755 and 0644, is tightened to
// the same modes on the next write rather than left alone: gitcd owns every
// path under the managed directory and regenerates it in full, so no user
// set mode is lost, and a file left readable by others would keep exposing
// the identity data this change exists to protect. The managed directory
// itself is tightened too, even when no file is written below it directly.
func writeRenderedFiles(rendered gitconfig.Rendered, managedDir string, logger *slog.Logger, stdout io.Writer) error {
	paths := make([]string, 0, len(rendered))
	for path := range rendered {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		if err := ensurePrivateDir(managedDir); err != nil {
			return err
		}
		if dir := filepath.Dir(path); dir != managedDir {
			if err := ensurePrivateDir(dir); err != nil {
				return err
			}
		}
		if err := writePrivateFile(path, []byte(rendered[path])); err != nil {
			return err
		}
		logger.Debug("wrote a managed file", "path", path)
		fmt.Fprintln(stdout, path)
	}
	return nil
}

// ensurePrivateDir creates dir and its missing parents 0700 and tightens
// dir itself to 0700 when it already exists wider. Only dir itself is
// tightened; a parent that already existed keeps its mode.
func ensurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, managedDirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.Chmod(dir, managedDirMode); err != nil {
		return fmt.Errorf("set permissions on %s: %w", dir, err)
	}
	return nil
}

// errSymlinkAtManagedPath is returned when a symlink sits where gitcd
// would write a managed file. gitcd refuses it rather than write through
// the link, which would change the file the link points at, wherever that
// is, and set its mode to 0600.
var errSymlinkAtManagedPath = errors.New("a symlink sits at a managed file path")

// writePrivateFile writes content to path with mode 0600, tightening an
// existing file to it. The mode is set on the open file, not after the
// write, so the content is never readable by others between the two steps.
//
// A symlink at path is refused with errSymlinkAtManagedPath. A new file is
// created with O_EXCL, which never follows a link, and an existing one is
// checked with Lstat before the open and again against the open file
// before it is truncated, so a link put there in between is caught too.
func writePrivateFile(path string, content []byte) error {
	info, err := os.Lstat(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("write %s: %w", path, errSymlinkAtManagedPath)
	}
	if err == nil && !os.SameFile(info, info) {
		// On Windows Lstat does not read the file identity. os.SameFile
		// reads it on first use, so read it now, before the open. A false
		// result means the identity could not be read.
		return fmt.Errorf("write %s: read the file identity", path)
	}

	var f *os.File
	if err != nil {
		f, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, managedFileMode)
	} else {
		f, err = os.OpenFile(path, os.O_WRONLY, managedFileMode)
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if info != nil {
		opened, statErr := f.Stat()
		if statErr != nil || !os.SameFile(info, opened) {
			_ = f.Close()
			return fmt.Errorf("write %s: %w", path, errSymlinkAtManagedPath)
		}
		if err := f.Truncate(0); err != nil {
			_ = f.Close()
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	if err := f.Chmod(managedFileMode); err != nil {
		_ = f.Close()
		return fmt.Errorf("set permissions on %s: %w", path, err)
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
