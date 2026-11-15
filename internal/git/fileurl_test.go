// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package git_test

import (
	"path/filepath"
	"strings"
)

// fileURL returns the file URL of an absolute path. A Windows path needs a
// leading slash and forward slashes: "file://C:\x" is not a URL git reads.
func fileURL(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return "file://" + path
}
