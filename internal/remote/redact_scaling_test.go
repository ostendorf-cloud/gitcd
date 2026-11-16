// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package remote_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ostendorf-cloud/gitcd/internal/remote"
)

// scalingUnits are repeated pieces of free text with many URL regions and no
// white space between them.
var scalingUnits = []string{",https://a@", "(https://a", "=https://a", " https://a@", "=https://a?"}

// bestOf returns the shortest of three runs of f, which keeps a slow or busy
// machine from inflating one sample.
func bestOf(f func()) time.Duration {
	best := time.Duration(1<<63 - 1)
	for range 3 {
		start := time.Now()
		f()
		if d := time.Since(start); d < best {
			best = d
		}
	}
	return best
}

// TestRedact_ScalesLinearly runs Redact at two sizes and compares the growth,
// not the absolute time: a linear function grows 8 times for 8 times the
// input, a quadratic one 64 times. The absolute cap is generous.
func TestRedact_ScalesLinearly(t *testing.T) {
	const small, large = 20 << 10, 160 << 10
	for _, unit := range scalingUnits {
		t.Run(unit, func(t *testing.T) {
			smallInput := strings.Repeat(unit, small/len(unit))
			largeInput := strings.Repeat(unit, large/len(unit))

			smallTime := bestOf(func() { remote.Redact(smallInput) })
			largeTime := bestOf(func() { remote.Redact(largeInput) })
			t.Logf("20 KB: %v, 160 KB: %v", smallTime, largeTime)

			if largeTime > 5*time.Second {
				t.Fatalf("Redact of 160 KB took %v, want well under 5s", largeTime)
			}
			// Below a millisecond the timer noise decides the ratio.
			if floor := max(smallTime, time.Millisecond); largeTime > 30*floor {
				t.Fatalf("Redact grew %v to %v for 8 times the input, want near linear growth", smallTime, largeTime)
			}
		})
	}
}

// TestRedact_MixedUnitsDoNotPanic runs Redact over every pair and triple of
// pieces that URL regions and their query, fragment and userinfo are made of,
// and checks the shape of what comes back.
func TestRedact_MixedUnitsDoNotPanic(t *testing.T) {
	t.Parallel()

	pieces := []string{
		",https://a@", "(https://a", "=https://a", " https://a@", "=https://a?", "?", "#", "@", " ", "\n",
		"x", "ssh://git@h/", "https://ORG@dev.azure.com/ORG/P/_git/R", "'", " ", "://",
	}
	const marker = "FAKEQUERYMARK"
	check := func(input string) {
		got := remote.Redact(input)
		// A URL that ends the text with a query is masked, whatever came before.
		tail := remote.Redact(input + " https://t.test/x?a=" + marker)
		if strings.Contains(tail, marker) {
			t.Fatalf("Redact(%q) = %q, want no query part", input+" https://t.test/x?a="+marker, tail)
		}
		// Every marker replaces at least one byte of text or follows a "?"
		// or "#", so the result stays within a small multiple of the input.
		if len(got) > 4*len(input)+3 {
			t.Fatalf("Redact(%q) = %q, want a result of at most four times the input", input, got)
		}
	}
	for _, a := range pieces {
		check(a)
		for _, b := range pieces {
			check(a + b)
			for _, c := range pieces {
				check(a + b + c)
				check(strings.Repeat(a+b+c, 4))
			}
		}
	}
}
