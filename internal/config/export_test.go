// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package config

// ExtraKnownGitKeysForTest exposes the addendum to the generated git key
// list, so a test can check every entry is still one the generated list
// does not carry.
func ExtraKnownGitKeysForTest() []string {
	return extraKnownGitKeys
}

// GeneratedGitKeyForTest reports whether the generated list, without the
// addendum, carries the "section.name" pair key.
func GeneratedGitKeyForTest(key string) bool {
	return generatedGitKeys()[key]
}
