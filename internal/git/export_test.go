// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package git

// FetchError exposes fetchError to the external test package.
var FetchError = fetchError

// ConfigPairsCarryCredential exposes configPairsCarryCredential to the
// external test package.
var ConfigPairsCarryCredential = configPairsCarryCredential

// ConfiguredURLsCarryCredential exposes configuredURLsCarryCredential with
// the runner as a plain function type.
func ConfiguredURLsCarryCredential(run func(args ...string) ([]byte, error)) bool {
	return configuredURLsCarryCredential(run)
}
