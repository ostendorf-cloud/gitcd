// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package doctor_test

import (
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/doctor"
)

func TestCheckOptedInKeys(t *testing.T) {
	t.Parallel()

	t.Run("a profile carrying only identity keys yields no finding", func(t *testing.T) {
		t.Parallel()

		findings := doctor.CheckOptedInKeys(config.Config{
			Profiles: map[string]config.Profile{
				"private": {"user.email": "simon@example.test", "commit.gpgsign": "true"},
			},
		})
		if len(findings) != 0 {
			t.Fatalf("CheckOptedInKeys() = %+v, want no findings", findings)
		}
	})

	t.Run("a key applied through the opt in list is reported with its profile", func(t *testing.T) {
		t.Parallel()

		findings := doctor.CheckOptedInKeys(config.Config{
			AllowUnsafeKeys: []string{"core.pager"},
			Profiles: map[string]config.Profile{
				"private": {"user.email": "simon@example.test", "core.pager": "less -FRX"},
			},
		})

		f, ok := findKind(findings, doctor.KindOptedInKey)
		if !ok {
			t.Fatalf("CheckOptedInKeys() = %+v, want a %s finding", findings, doctor.KindOptedInKey)
		}
		if !strings.Contains(f.Message, "core.pager") || !strings.Contains(f.Message, "private") {
			t.Fatalf("finding message = %q, want it to name both the key and the profile carrying it", f.Message)
		}
		if f.Fixable {
			t.Fatal("finding is marked fixable, want a deliberate opt in reported rather than repaired")
		}
	})

	t.Run("one finding per opted in key, in a stable order", func(t *testing.T) {
		t.Parallel()

		cfg := config.Config{
			AllowUnsafeKeys: []string{"core.pager", "credential.helper"},
			Profiles: map[string]config.Profile{
				"work":    {"credential.helper": "store"},
				"private": {"core.pager": "less -FRX"},
			},
		}

		first := doctor.CheckOptedInKeys(cfg)
		if len(first) != 2 {
			t.Fatalf("CheckOptedInKeys() = %+v, want one finding per opted in key", first)
		}
		if !strings.Contains(first[0].Message, "private") || !strings.Contains(first[1].Message, "work") {
			t.Fatalf("CheckOptedInKeys() = %+v, want the findings ordered by profile name", first)
		}
		second := doctor.CheckOptedInKeys(cfg)
		for i := range first {
			if first[i] != second[i] {
				t.Fatalf("CheckOptedInKeys() returned %+v then %+v, want the same order every time", first, second)
			}
		}
	})
}
