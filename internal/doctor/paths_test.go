// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package doctor_test

import (
	"path/filepath"

	"github.com/ostendorf-cloud/gitcd/internal/doctor"
)

// nativePaths returns repos with every Path and Want rewritten from slash
// to the running system's separator, so a table written with Unix paths
// matches how doctor splits a path on Windows.
func nativePaths(repos []doctor.RepoPath) []doctor.RepoPath {
	out := make([]doctor.RepoPath, len(repos))
	for i, rp := range repos {
		rp.Path = filepath.FromSlash(rp.Path)
		rp.Want = filepath.FromSlash(rp.Want)
		out[i] = rp
	}
	return out
}
