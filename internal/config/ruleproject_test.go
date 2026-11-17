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

func TestLoad_RuleProject(t *testing.T) {
	tests := []struct {
		name string
		rule string
		// wantErr is the reason a rule is refused, nil for an accepted rule.
		wantErr     error
		wantProject string
	}{
		{
			name:        "a project next to an owner on dev.azure.com",
			rule:        "host: dev.azure.com\n    owner: acme\n    project: billing\n",
			wantProject: "billing",
		},
		{
			name:        "a project with a space",
			rule:        "host: dev.azure.com\n    owner: acme\n    project: My Project\n",
			wantProject: "My Project",
		},
		{
			name:        "a project with parentheses",
			rule:        "host: dev.azure.com\n    owner: acme\n    project: \"Billing (EU)\"\n",
			wantProject: "Billing (EU)",
		},
		{
			name:        "a project with an ampersand",
			rule:        "host: dev.azure.com\n    owner: acme\n    project: \"R&D\"\n",
			wantProject: "R&D",
		},
		{
			name:        "a project with the other punctuation Azure DevOps allows",
			rule:        "host: dev.azure.com\n    owner: acme\n    project: \"Ops! @^~ Team\"\n",
			wantProject: "Ops! @^~ Team",
		},
		{
			name:        "a project with a combining mark",
			rule:        "host: dev.azure.com\n    owner: acme\n    project: \"Mu\\u0308nchen\"\n",
			wantProject: "München",
		},
		{
			name:        "a project of exactly 64 characters",
			rule:        "host: dev.azure.com\n    owner: acme\n    project: " + strings.Repeat("a", 64) + "\n",
			wantProject: strings.Repeat("a", 64),
		},
		{
			name:        "a project with a precomposed accent",
			rule:        "host: dev.azure.com\n    owner: acme\n    project: \"Caf\\u00e9\"\n",
			wantProject: "Caf\u00e9",
		},
		{
			name:        "a project with a decomposed accent",
			rule:        "host: dev.azure.com\n    owner: acme\n    project: \"Cafe\\u0301\"\n",
			wantProject: "Cafe\u0301",
		},
		{
			name:    "a project starting with a combining mark",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: \"\\u0308abc\"\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a word starting with a combining mark",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: \"abc \\u0308def\"\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project holding the emoji variation selector",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: \"ab\\uFE0Fc\"\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project holding the text variation selector",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: \"ab\\uFE0Ec\"\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project holding a zero width space",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: \"ab\\u200Bc\"\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project holding a zero width non joiner",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: \"ab\\u200Cc\"\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project holding a zero width joiner",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: \"ab\\u200Dc\"\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project holding a word joiner",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: \"ab\\u2060c\"\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project holding a byte order mark",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: \"ab\\uFEFFc\"\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project of 65 characters",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: " + strings.Repeat("a", 65) + "\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project starting with an underscore",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: _billing\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project starting with a dot",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: .billing\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project ending with a dot",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: billing.\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project holding a percent sign",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: \"My%20Project\"\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project holding a bracket",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: \"a[b]\"\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project holding a question mark",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: \"a?\"\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project holding a backslash",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: \"a\\\\b\"\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project holding an apostrophe",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: \"it's\"\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:        "a project and a repo",
			rule:        "host: dev.azure.com\n    owner: acme\n    project: billing\n    repo: widget\n",
			wantProject: "billing",
		},
		{
			name:    "a project without an owner",
			rule:    "host: dev.azure.com\n    project: billing\n",
			wantErr: config.ErrProjectWithoutOwner,
		},
		{
			name:    "a project on a host whose remotes carry none",
			rule:    "host: github.com\n    owner: acme\n    project: billing\n",
			wantErr: config.ErrProjectHost,
		},
		{
			name:    "an owner holding the project on dev.azure.com",
			rule:    "host: dev.azure.com\n    owner: acme/billing\n",
			wantErr: config.ErrOwnerHoldsProject,
		},
		{
			name:    "an owner holding the project and a repo on dev.azure.com",
			rule:    "host: dev.azure.com\n    owner: acme/billing\n    repo: widget\n",
			wantErr: config.ErrOwnerHoldsProject,
		},
		{
			name:    "a project holding a wildcard",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: \"bill*\"\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project holding a path separator",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: \"a/b\"\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project holding a quote",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: \"a\\\" ]\"\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project with a trailing space",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: \"billing \"\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project with a doubled space",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: \"My  Project\"\n",
			wantErr: config.ErrInvalidProfile,
		},
		{
			name:    "a project naming the parent directory",
			rule:    "host: dev.azure.com\n    owner: acme\n    project: \"..\"\n",
			wantErr: config.ErrInvalidProfile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := withHome(t)
			writeFile(t, filepath.Join(home, ".gitcd.yaml"),
				"profiles:\n  work:\n    user.email: work@example.test\n\nrules:\n  - "+tt.rule+"    profile: work\n")

			got, err := config.Load()
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Load() error = %v, want it to wrap %v", err, tt.wantErr)
				}
				if !errors.Is(err, config.ErrInvalidProfile) {
					t.Fatalf("Load() error = %v, want it to wrap config.ErrInvalidProfile too", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() returned an unexpected error: %v", err)
			}
			if len(got.Rules) != 1 || got.Rules[0].Project != tt.wantProject {
				t.Fatalf("Load().Rules = %+v, want one rule with the project %q", got.Rules, tt.wantProject)
			}
		})
	}

	t.Run("an owner with a subgroup stays valid on another host", func(t *testing.T) {
		home := withHome(t)
		writeFile(t, filepath.Join(home, ".gitcd.yaml"),
			"profiles:\n  work:\n    user.email: work@example.test\n\nrules:\n  - host: gitlab.example.test\n    owner: acme/platform\n    profile: work\n")

		if _, err := config.Load(); err != nil {
			t.Fatalf("Load() returned an unexpected error: %v", err)
		}
	})
}
