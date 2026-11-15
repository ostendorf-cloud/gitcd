// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/config"
)

// TestUnsafeKeyReason_RestrictsWhatItSaysItDoes checks the gate key by
// key, with the value each key would really carry, in both directions
// at once: a key that hands git a command, a credential or a trust
// decision is restricted, and a key that is a plain setting is not.
//
// Both halves belong in one table. The gate is only as good as the
// narrower of the two: a gate that refuses everything holds the first
// half and is unusable, and one that refuses nothing holds the second
// and is pointless.
func TestUnsafeKeyReason_RestrictsWhatItSaysItDoes(t *testing.T) {
	t.Parallel()

	restricted := []struct{ key, value string }{
		// Commands, by name, under whatever section.
		{"core.sshCommand", "ssh -i /dev/null"},
		{"core.askPass", "/usr/local/bin/ask"},
		{"core.editor", "vim"},
		{"core.pager", "less"},
		{"core.alternateRefsCommand", "/usr/local/bin/refs"},
		{"core.gitProxy", "/usr/local/bin/proxy"},
		{"core.hooksPath", "/tmp/hooks"},
		{"core.fsmonitor", "/usr/local/bin/watch"},
		{"imap.tunnel", "/bin/sh"},
		{"instaweb.httpd", "/bin/sh"},
		{"sendemail.smtpServer", "/bin/sh"},
		// Settings that decide what git writes to the filesystem, which a
		// profile applied automatically per remote may not weaken.
		{"core.protectNTFS", "false"},
		{"core.protectHFS", "false"},
		{"core.sharedRepository", "0777"},
		{"core.symlinks", "false"},
		// A value that happens to look like a boolean, for a key whose
		// value git resolves to a program. "true", "false" and "yes" are
		// all programs on PATH, and an empty value is a boolean too.
		{"core.sshCommand", "false"},
		{"core.sshCommand", "yes"},
		{"core.sshCommand", ""},
		{"core.hooksPath", ""},
		{"core.fsmonitor", "true"},
		{"gpg.program", "true"},
		{"diff.ext.command", "false"},
		{"filter.lfs.clean", "false"},
		{"safe.directory", "true"},
		{"credential.helper", "true"},
		{"url.x.insteadOf", "true"},
		{"sequence.editor", "/usr/local/bin/seq"},
		{"diff.external", "/usr/local/bin/diff"},
		{"diff.jpeg.command", "/usr/local/bin/jdiff"},
		{"diff.jpeg.textconv", "/usr/local/bin/exif"},
		{"merge.theirs.driver", "/usr/local/bin/merge %A %O %B"},
		{"filter.lfs.clean", "git-lfs clean"},
		{"filter.lfs.smudge", "git-lfs smudge"},
		{"filter.lfs.process", "git-lfs filter-process"},
		{"gpg.program", "/usr/local/bin/gpg"},
		{"gpg.ssh.defaultKeyCommand", "/usr/local/bin/key"},
		{"ssh.variant", "simple"},
		{"init.templateDir", "/tmp/template"},
		{"uploadpack.packObjectsHook", "/usr/local/bin/hook"},
		{"http.proxy", "http://proxy.example.test"},
		{"difftool.vimdiff.cmd", "/usr/local/bin/vd"},
		{"difftool.vimdiff.path", "/usr/local/bin/vd"},
		{"mergetool.vimdiff.cmd", "/usr/local/bin/vd"},
		{"mergetool.vimdiff.path", "/usr/local/bin/vd"},
		// Trust and transport decisions.
		{"safe.directory", "/tmp/elsewhere"},
		{"safe.bareRepository", "all"},
		{"protocol.allow", "always"},
		{"protocol.ext.allow", "always"},
		// Credentials, URL rewriting and trace destinations, by section.
		{"credential.helper", "store"},
		{"credential.https://example.test.helper", "store"},
		{"alias.ci", "!touch /tmp/marker"},
		{"pager.log", "/usr/local/bin/pager"},
		{"url.gh.insteadOf", "https://github.com/"},
		{"url.gh.pushInsteadOf", "https://github.com/"},
		{"trace2.perfTarget", "/tmp/trace"},
		// A key no documented git setting matches, whatever it is called.
		{"core.somethingGitAddedLater", "/usr/local/bin/thing"},
		{"madeup.section.key", "value"},
		{"user.emial", "typo@example.test"},
	}

	for _, tt := range restricted {
		t.Run(tt.key+" is restricted", func(t *testing.T) {
			t.Parallel()
			if why, ok := config.UnsafeKeyReason(tt.key, tt.value); !ok {
				t.Fatalf("UnsafeKeyReason(%q, %q) = %q, %v, want it restricted", tt.key, tt.value, why, ok)
			}
		})
	}

	allowed := []struct{ key, value string }{
		// The identity and signing keys the profiles exist to carry.
		{"user.name", "Simon Ostendorf"},
		{"user.email", "simon@example.test"},
		{"user.signingKey", "ssh-ed25519 AAAAEXAMPLE"},
		{"user.useConfigOnly", "true"},
		{"gpg.format", "ssh"},
		{"gpg.ssh.allowedSignersFile", "/home/simon/.signers"},
		{"commit.gpgsign", "true"},
		{"tag.gpgsign", "true"},
		// The "core" settings that describe a working tree.
		{"core.autocrlf", "input"},
		{"core.eol", "lf"},
		{"core.attributesFile", "/home/simon/.gitattributes"},
		{"core.excludesFile", "/home/simon/.gitignore"},
		{"core.commentChar", ";"},
		{"core.commentString", "//"},
		{"core.sparseCheckout", "true"},
		{"core.sparseCheckoutCone", "true"},
		{"core.hideDotFiles", "dotGitOnly"},
		{"core.longPaths", "true"},
		{"core.fsync", "committed"},
		{"core.fsyncMethod", "fsync"},
		{"core.preloadIndex", "true"},
		{"core.splitIndex", "false"},
		{"core.commitGraph", "true"},
		{"core.multiPackIndex", "true"},
		{"core.ignoreStat", "false"},
		{"core.checkStat", "default"},
		{"core.notesRef", "refs/notes/commits"},
		{"core.worktree", "/home/simon/Code/widget"},
		{"core.deltaBaseCacheLimit", "96m"},
		{"core.packedGitLimit", "256m"},
		{"core.warnAmbiguousRefs", "true"},
		{"core.maxTreeDepth", "4096"},
		{"core.ignoreCase", "true"},
		{"core.precomposeUnicode", "true"},
		{"core.quotePath", "false"},
		{"core.whitespace", "trailing-space"},
		{"core.untrackedCache", "true"},
		{"core.logAllRefUpdates", "true"},
		// Booleans and numbers whose names read like a command.
		{"advice.ignoredHook", "false"},
		{"advice.waitingForEditor", "false"},
		{"column.clean", "never"},
		{"color.pager", "true"},
		{"difftool.prompt", "false"},
		{"mergetool.prompt", "false"},
		{"mergetool.keepBackup", "false"},
		{"mergetool.keepTemporaries", "false"},
		{"mergetool.writeToTemp", "true"},
		{"mergetool.vimdiff.layout", "LOCAL,BASE,REMOTE"},
		{"protocol.version", "2"},
		{"credential.useHttpPath", "true"},
		{"trace2.eventBrief", "true"},
		// Ordinary preferences.
		{"init.defaultBranch", "main"},
		{"pull.rebase", "true"},
		{"push.default", "simple"},
		{"fetch.prune", "true"},
		{"rebase.autoStash", "true"},
		{"diff.algorithm", "histogram"},
		{"merge.conflictStyle", "zdiff3"},
		{"status.showUntrackedFiles", "all"},
	}

	for _, tt := range allowed {
		t.Run(tt.key+" needs no opt in", func(t *testing.T) {
			t.Parallel()
			if why, ok := config.UnsafeKeyReason(tt.key, tt.value); ok {
				t.Fatalf("UnsafeKeyReason(%q, %q) = %q, %v, want it allowed", tt.key, tt.value, why, ok)
			}
		})
	}
}

// TestUnsafeKeyReason_KeysGitDoesNotEnumerate pins down the cost of the
// unfamiliar key rule, so it is a stated property rather than a
// surprise. git documents a section whose key names the user invents by
// the section alone, "filter.<driver>.clean" and "pager.<command>", so
// a key in one of those that is not one of the spellings git names is
// not in the generated list and is restricted, however ordinary it is.
// The remedy is the opt in, printed with the refusal.
func TestUnsafeKeyReason_KeysGitDoesNotEnumerate(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ key, value string }{
		{"filter.lfs.required", "true"},
		{"pager.diff", "false"},
	} {
		t.Run(tt.key+" is restricted until it is named", func(t *testing.T) {
			t.Parallel()
			if _, ok := config.UnsafeKeyReason(tt.key, tt.value); !ok {
				t.Fatalf("UnsafeKeyReason(%q, %q) allowed a key the generated list does not hold", tt.key, tt.value)
			}
		})
	}
}

// TestUnsafeKeyReason_TheValueNeverSetsAFamilyAside pins down what the
// set aside is and is not.
//
// It is a property of the section, not of the value. A value that looks
// inert is no evidence at all: "true", "false" and "yes" are programs on
// PATH, so a key git resolves to a program is restricted whatever it
// carries, and an empty value is a boolean too.
func TestUnsafeKeyReason_TheValueNeverSetsAFamilyAside(t *testing.T) {
	t.Parallel()

	t.Run("a boolean looking value does not set a resolved family aside", func(t *testing.T) {
		t.Parallel()
		for _, value := range []string{"true", "false", "yes", "no", "on", "off", "1", "0", ""} {
			if _, ok := config.UnsafeKeyReason("core.sshCommand", value); !ok {
				t.Errorf("UnsafeKeyReason(core.sshCommand, %q) allowed it, want a resolved key restricted at any value", value)
			}
		}
	})

	t.Run("a section git resolves nothing in is set aside", func(t *testing.T) {
		t.Parallel()
		for _, key := range []string{"advice.ignoredHook", "advice.waitingForEditor", "advice.updateSparsePath", "color.pager"} {
			if why, ok := config.UnsafeKeyReason(key, "false"); ok {
				t.Errorf("UnsafeKeyReason(%q, \"false\") = %q, want it allowed", key, why)
			}
		}
	})

	t.Run("the set aside does not reach a key git does not document", func(t *testing.T) {
		t.Parallel()
		if _, ok := config.UnsafeKeyReason("advice.somethingGitAddedLater", "false"); !ok {
			t.Fatal("UnsafeKeyReason() allowed an undocumented key in an inert section, want it restricted")
		}
	})
}

// TestExtraKnownGitKeys keeps the addendum honest. Every entry is there
// because "git help -c" does not print it, so an entry that the
// generated list has started carrying is a duplicate that should be
// removed rather than left to drift.
func TestExtraKnownGitKeys(t *testing.T) {
	t.Parallel()

	for _, key := range config.ExtraKnownGitKeysForTest() {
		if config.GeneratedGitKeyForTest(key) {
			t.Errorf("the addendum carries %q, which the generated list now holds too; remove the addendum entry", key)
		}
		if _, ok := config.UnsafeKeyReason(key, "true"); ok {
			t.Errorf("the addendum carries %q, which the family rules restrict anyway; it does not belong there", key)
		}
	}
}
