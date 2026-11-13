// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/config"
)

func TestAddProfileKeys(t *testing.T) {
	t.Parallel()

	t.Run("builds a fresh document when none exists yet", func(t *testing.T) {
		t.Parallel()
		out, err := config.AddProfileKeys(nil, "global", map[string]string{"user.email": "me@example.test"})
		if err != nil {
			t.Fatalf("AddProfileKeys() returned an unexpected error: %v", err)
		}

		path := filepath.Join(t.TempDir(), "gitcd.yaml")
		writeFile(t, path, string(out))
		cfg, err := config.LoadFrom(path)
		if err != nil {
			t.Fatalf("LoadFrom() the result returned an unexpected error: %v", err)
		}
		if got, want := cfg.Profiles["global"]["user.email"], "me@example.test"; got != want {
			t.Fatalf("profiles.global.user.email = %q, want %q", got, want)
		}
	})

	t.Run("adds a new profile without disturbing an existing one", func(t *testing.T) {
		t.Parallel()
		existing := "profiles:\n  work:\n    user.email: work@example.test\n"

		out, err := config.AddProfileKeys([]byte(existing), "global", map[string]string{"user.name": "Jane Doe"})
		if err != nil {
			t.Fatalf("AddProfileKeys() returned an unexpected error: %v", err)
		}

		path := filepath.Join(t.TempDir(), "gitcd.yaml")
		writeFile(t, path, string(out))
		cfg, err := config.LoadFrom(path)
		if err != nil {
			t.Fatalf("LoadFrom() the result returned an unexpected error: %v", err)
		}
		if got, want := cfg.Profiles["work"]["user.email"], "work@example.test"; got != want {
			t.Fatalf("profiles.work.user.email = %q, want %q, the existing profile must be untouched", got, want)
		}
		if got, want := cfg.Profiles["global"]["user.name"], "Jane Doe"; got != want {
			t.Fatalf("profiles.global.user.name = %q, want %q", got, want)
		}
	})

	t.Run("overwrites an existing key in the named profile rather than duplicating it", func(t *testing.T) {
		t.Parallel()
		existing := "profiles:\n  global:\n    user.email: old@example.test\n    user.name: Old Name\n"

		out, err := config.AddProfileKeys([]byte(existing), "global", map[string]string{"user.email": "new@example.test"})
		if err != nil {
			t.Fatalf("AddProfileKeys() returned an unexpected error: %v", err)
		}

		path := filepath.Join(t.TempDir(), "gitcd.yaml")
		writeFile(t, path, string(out))
		cfg, err := config.LoadFrom(path)
		if err != nil {
			t.Fatalf("LoadFrom() the result returned an unexpected error: %v", err)
		}
		if got, want := cfg.Profiles["global"]["user.email"], "new@example.test"; got != want {
			t.Fatalf("profiles.global.user.email = %q, want %q", got, want)
		}
		if got, want := cfg.Profiles["global"]["user.name"], "Old Name"; got != want {
			t.Fatalf("profiles.global.user.name = %q, want %q, an untouched key must survive", got, want)
		}
	})

	t.Run("preserves a comment elsewhere in the document", func(t *testing.T) {
		t.Parallel()
		existing := "# a hand written note a person left for themselves\nroot: ~/Code\n"

		out, err := config.AddProfileKeys([]byte(existing), "global", map[string]string{"user.email": "me@example.test"})
		if err != nil {
			t.Fatalf("AddProfileKeys() returned an unexpected error: %v", err)
		}
		if !strings.Contains(string(out), "a hand written note a person left for themselves") {
			t.Fatalf("AddProfileKeys() dropped the document's own comment:\n%s", out)
		}
	})

	t.Run("rejects a document that is not a mapping at its top level", func(t *testing.T) {
		t.Parallel()
		_, err := config.AddProfileKeys([]byte("- not\n- a\n- mapping\n"), "global", map[string]string{"user.email": "me@example.test"})
		if err == nil {
			t.Fatal("AddProfileKeys() succeeded, want an error for a non mapping top level document")
		}
	})

	t.Run("refuses a file holding more than one YAML document rather than dropping the rest", func(t *testing.T) {
		t.Parallel()
		existing := "profiles:\n  work:\n    user.email: work@example.test\n" +
			"---\n" +
			"profiles:\n  personal:\n    user.email: personal@example.test\n"

		_, err := config.AddProfileKeys([]byte(existing), "global", map[string]string{"user.email": "me@example.test"})
		if err == nil {
			t.Fatal("AddProfileKeys() succeeded, want a refusal for a multi document file")
		}
		if !errors.Is(err, config.ErrMultipleDocuments) {
			t.Fatalf("AddProfileKeys() error = %v, want it to wrap config.ErrMultipleDocuments", err)
		}
	})

	t.Run("accepts a file ending in a bare document separator with nothing after it", func(t *testing.T) {
		t.Parallel()
		existing := "profiles:\n  work:\n    user.email: work@example.test\n---\n"

		out, err := config.AddProfileKeys([]byte(existing), "global", map[string]string{"user.email": "me@example.test"})
		if err != nil {
			t.Fatalf("AddProfileKeys() returned an unexpected error for a trailing empty document: %v", err)
		}

		path := filepath.Join(t.TempDir(), "gitcd.yaml")
		writeFile(t, path, string(out))
		cfg, err := config.LoadFrom(path)
		if err != nil {
			t.Fatalf("LoadFrom() the result returned an unexpected error: %v", err)
		}
		if got, want := cfg.Profiles["work"]["user.email"], "work@example.test"; got != want {
			t.Fatalf("profiles.work.user.email = %q, want %q", got, want)
		}
	})
}
