// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package testenv holds helpers shared by the tests of several packages.
// Only test files import it.
package testenv

import "testing"

// SetHome points every variable a Go program or git reads the home
// directory from at home, for the duration of the test. Unix reads HOME.
// Windows reads USERPROFILE, which is what os.UserHomeDir returns there,
// and APPDATA and LOCALAPPDATA for per user configuration.
func SetHome(t testing.TB, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", home)
	t.Setenv("LOCALAPPDATA", home)
}
