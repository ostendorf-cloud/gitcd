// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package gitconfig_test

import (
	"path/filepath"
	"runtime"
	"strings"
)

// homeDir and rootDir are the fixed home and clone root the rendering
// tests use. They are absolute on every system: Render refuses a root
// that is not, and a path with no drive letter is not absolute on Windows.
var (
	homeDir = absPath("/home/simon")
	// rootDir is not under /home: Render also renders the root with its
	// symlinks resolved, and /home is a link into /System/Volumes/Data on
	// macOS, so a root there would render a second condition on that host
	// only.
	rootDir = absPath("/srv/simon/Code")
)

// absPath turns a slash separated Unix path into an absolute path of the
// running system.
func absPath(p string) string {
	if runtime.GOOS == "windows" {
		return "C:" + filepath.FromSlash(p)
	}
	return p
}

// unixPaths rewrites every path in text that absPath produced back to its
// Unix spelling, so a golden file written on Linux holds on Windows too.
// It does nothing elsewhere.
func unixPaths(text string) string {
	if runtime.GOOS != "windows" {
		return text
	}
	text = strings.ReplaceAll(text, `\\`, "/")
	text = strings.ReplaceAll(text, `\`, "/")
	text = strings.ReplaceAll(text, "C:/srv/", "/srv/")
	return strings.ReplaceAll(text, "C:/home/", "/home/")
}
