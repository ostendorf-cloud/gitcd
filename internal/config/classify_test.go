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
		// Keys whose value makes git read configuration from elsewhere,
		// which hands every key in that file to git ungated.
		{"include.path", "/tmp/pulled-in.inc"},
		{"includeIf.path", "/tmp/pulled-in.inc"},
		// A program git runs, named or selected.
		{"browser.firefox.path", "/usr/local/bin/ff"},
		{"man.viewer.path", "/usr/local/bin/man"},
		{"instaweb.modulePath", "/usr/lib/apache2"},
		{"web.browser", "firefox"},
		{"help.browser", "firefox"},
		{"instaweb.browser", "firefox"},
		{"man.viewer", "man"},
		// Whether git verifies, what it verifies with, and what it
		// writes to the filesystem.
		{"http.sslVerify", "false"},
		{"http.sslCAInfo", "/tmp/ca.pem"},
		{"http.sslCAPath", "/tmp/ca"},
		{"http.sslBackend", "openssl"},
		{"http.curloptResolve", "example.test:443:127.0.0.1"},
		{"http.sslCert", "/tmp/client.pem"},
		{"http.sslKey", "/tmp/client.key"},
		{"http.cookieFile", "/tmp/cookies"},
		{"http.saveCookies", "true"},
		{"http.extraHeader", "Authorization: Basic Zm9v"},
		{"http.followRedirects", "always"},
		{"http.pinnedPubkey", "sha256//x"},
		{"credential.protectProtocol", "false"},
		{"credential.sanitizePrompt", "false"},
		{"transfer.credentialsInUrl", "allow"},
		{"transfer.fsckObjects", "false"},
		{"fetch.fsckObjects", "false"},
		{"receive.fsckObjects", "false"},
		{"gpg.revocationFile", "/tmp/revoked"},
		{"gpg.ssh.revocationFile", "/tmp/revoked"},
		{"gpg.ssh.allowedSignersFile", "/home/simon/.signers"},
		{"gpg.minTrustLevel", "undefined"},
		// A severity or a skip list turns off the same object checks the
		// "fsckObjects" switches turn on, in all three spellings git
		// documents.
		{"fsck.missingEmail", "ignore"},
		{"fsck.skipList", "/tmp/skip"},
		{"fsck.gitmodulesPath", "ignore"},
		{"fetch.fsck.skipList", "/tmp/skip"},
		{"receive.fsck.missingEmail", "ignore"},
		{"receive.gitmodulesPath", "ignore"},
		// A form git documents as a custom command, and refuses to
		// propagate out of .gitmodules itself.
		{"submodule.lib.update", "!/bin/sh"},
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
		// The third category: a file git reads as data. None grants trust
		// or decides a verification, so each stays allowed deliberately,
		// and this is where that decision is pinned.
		{"core.attributesFile", "/home/simon/.gitattributes"},
		{"core.excludesFile", "/home/simon/.gitignore"},
		{"mailmap.file", "/home/simon/.mailmap"},
		{"diff.orderFile", "/home/simon/.diff-order"},
		{"blame.ignoreRevsFile", "/home/simon/.git-blame-ignore-revs"},
		{"format.signatureFile", "/home/simon/.signature"},
		// Keys ending in "path" that name no path git resolves.
		{"core.quotePath", "false"},
		{"credential.useHttpPath", "true"},
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

// TestUnsafeKeyReason_RefusesAnIncludeForBeingAnInclude pins which rule
// refuses an include, not only that one does.
//
// Both spellings stay refused with the include rule deleted: "path" is
// a suffix family, and a real condition carries characters the
// subsection class rejects before the gate is reached at all. So
// "is it refused" tells the rules apart, and the stated reason is what
// does. The other two reasons are asserted beside it, so a reason that
// matched everything would fail the test rather than pass it.
func TestUnsafeKeyReason_RefusesAnIncludeForBeingAnInclude(t *testing.T) {
	t.Parallel()

	const (
		includeReason = "makes git read configuration from a file no gate has seen"
		sectionReason = "sets a command git runs, or rewrites the URL git contacts"
		suffixReason  = "names a command git runs, or a path git resolves one from"
	)

	for _, tt := range []struct{ name, key, value, want string }{
		{"an unconditional include", "include.path", "/home/simon/.gitcd/work.inc", includeReason},
		{
			"a conditional include carrying its condition",
			"includeIf.gitdir:~/Code/acme/.path", "/home/simon/.gitcd/acme.inc", includeReason,
		},
		{"a conditional include without a condition", "includeIf.path", "/home/simon/.gitcd/acme.inc", includeReason},
		{"an include spelled in upper case", "INCLUDE.PATH", "/home/simon/.gitcd/work.inc", includeReason},
		{
			"a conditional include spelled in lower case",
			"includeif.gitdir:~/Code/acme/.path", "/home/simon/.gitcd/acme.inc", includeReason,
		},
		{"an alias, which a restricted section owns", "alias.ci", "!touch /tmp/marker", sectionReason},
		{"a hooks path, which the suffix family owns", "core.hooksPath", "/home/simon/.githooks", suffixReason},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			why, ok := config.UnsafeKeyReason(tt.key, tt.value)
			if !ok {
				t.Fatalf("UnsafeKeyReason(%q, %q) allowed it, want it restricted", tt.key, tt.value)
			}
			if why != tt.want {
				t.Errorf("UnsafeKeyReason(%q, %q) = %q, want %q", tt.key, tt.value, why, tt.want)
			}
		})
	}
}

// TestUnsafeKeyReason_SaysWhichCategoryRefusedTheKey asserts the reason
// each category gives, not only that the key is refused.
//
// A key in either category is refused either way, so the reason is the
// whole of what a wrong category changes, and it is the part a person
// acts on. A key that names no program may not be refused for naming
// one: the reader would go looking for a command that is not there. The
// suffix family's reason is asserted beside the others, so a reason that
// matched everything fails the test rather than passing it.
func TestUnsafeKeyReason_SaysWhichCategoryRefusedTheKey(t *testing.T) {
	t.Parallel()

	const (
		trustReason  = "decides what git verifies, trusts, contacts or writes, rather than naming a command"
		fsckReason   = "decides whether one of git's object checks still applies"
		suffixReason = "names a command git runs, or a path git resolves one from"
	)

	for _, tt := range []struct{ name, key, value, want string }{
		{"one half of a content filter", "filter.lfs.clean", "git-lfs clean", "names a program git runs for this filter"},
		{"a submodule update command", "submodule.lib.update", "!/bin/sh", "names a program git runs for this submodule"},
		{"a protection deciding what git writes", "core.protectNTFS", "false", trustReason},
		{"the trust store an ssh signature is checked against", "gpg.ssh.allowedSignersFile", "/home/simon/.signers", trustReason},
		{"a directory of CA certificates", "http.sslCAPath", "/tmp/ca", trustReason},
		{"an address every request for a host goes to", "http.curloptResolve", "example.test:443:127.0.0.1", trustReason},
		{"an object check severity", "fsck.missingEmail", "ignore", fsckReason},
		{"an object check severity on the receiving side", "receive.fsck.missingEmail", "ignore", fsckReason},
		{"a path git resolves a command from", "core.hooksPath", "/home/simon/.githooks", suffixReason},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			why, ok := config.UnsafeKeyReason(tt.key, tt.value)
			if !ok {
				t.Fatalf("UnsafeKeyReason(%q, %q) allowed it, want it restricted", tt.key, tt.value)
			}
			if why != tt.want {
				t.Errorf("UnsafeKeyReason(%q, %q) = %q, want %q", tt.key, tt.value, why, tt.want)
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
