// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/git"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
	"github.com/ostendorf-cloud/gitcd/internal/profile"
)

const profileUsage = `gitcd profile pins, removes or inspects a repository's identity profile.

Usage:
  gitcd profile set <name> [path]     pin a profile to one repository's local config
  gitcd profile unset [path]          remove a pinned profile
  gitcd profile show [path]           print the expected and effective identity
`

// runProfile dispatches "gitcd profile <set|unset|show>".
func runProfile(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, profileUsage)
		return 2
	}
	switch args[0] {
	case "set":
		return runProfileSet(args[1:], stdout, stderr)
	case "unset":
		return runProfileUnset(args[1:], stdout, stderr)
	case "show":
		return runProfileShow(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "gitcd profile: unknown subcommand %q\n%s", args[0], profileUsage)
		return 2
	}
}

// runProfileSet implements "gitcd profile set <name> [path]": it writes
// name's git config keys to path's local configuration, defaulting path to
// ".", through "git -C path config --local", plus the marker
// "gitcd.profile" and the "gitcd.keys" list of keys it wrote, so "profile
// unset" later knows exactly what to remove.
func runProfileSet(args []string, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("gitcd profile set", flag.ContinueOnError)
	fset.SetOutput(stderr)
	dryRun := fset.Bool("dry-run", false, "print the keys that would be written without writing them")
	verbose := fset.Bool("verbose", false, "enable debug logging on stderr")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() < 1 || fset.NArg() > 2 {
		fmt.Fprintln(stderr, "gitcd profile set: a profile name, and an optional path, are required")
		fset.Usage()
		return 2
	}
	name := fset.Arg(0)
	path := "."
	if fset.NArg() == 2 {
		path = fset.Arg(1)
	}
	logger := newLogger(*verbose, stderr)
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd profile set: %v\n", err)
		return 1
	}
	prof, ok := cfg.Profiles[name]
	if !ok {
		fmt.Fprintf(stderr, "gitcd profile set: no profile named %q is configured\n", name)
		return 1
	}
	keys := prof.Keys()

	if *dryRun {
		for _, key := range keys {
			fmt.Fprintf(stdout, "%s = %s\n", key, prof[key])
		}
		fmt.Fprintf(stdout, "gitcd.profile = %s\n", name)
		fmt.Fprintf(stdout, "gitcd.keys = %s\n", strings.Join(keys, ","))
		return 0
	}

	for _, key := range keys {
		if err := git.ConfigSetLocal(ctx, path, key, prof[key]); err != nil {
			fmt.Fprintf(stderr, "gitcd profile set: %v\n", err)
			return 1
		}
		logger.Debug("wrote a local config key", "path", path, "key", key)
	}
	if err := git.ConfigSetLocal(ctx, path, "gitcd.profile", name); err != nil {
		fmt.Fprintf(stderr, "gitcd profile set: %v\n", err)
		return 1
	}
	if err := git.ConfigSetLocal(ctx, path, "gitcd.keys", strings.Join(keys, ",")); err != nil {
		fmt.Fprintf(stderr, "gitcd profile set: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, name)
	return 0
}

// runProfileUnset implements "gitcd profile unset [path]": it removes
// exactly the keys "gitcd.keys" recorded for path, defaulting path to ".",
// then the "gitcd.profile" and "gitcd.keys" markers themselves. A recorded
// key that no longer carries a local value, config having drifted since
// "profile set" wrote it, is reported as a leftover rather than failing
// the command.
func runProfileUnset(args []string, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("gitcd profile unset", flag.ContinueOnError)
	fset.SetOutput(stderr)
	dryRun := fset.Bool("dry-run", false, "print the keys that would be removed without removing them")
	verbose := fset.Bool("verbose", false, "enable debug logging on stderr")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() > 1 {
		fmt.Fprintln(stderr, "gitcd profile unset: at most one path is accepted")
		fset.Usage()
		return 2
	}
	path := "."
	if fset.NArg() == 1 {
		path = fset.Arg(0)
	}
	logger := newLogger(*verbose, stderr)
	ctx := context.Background()

	recorded, err := git.ConfigGetLocal(ctx, path, "gitcd.keys")
	if errors.Is(err, git.ErrConfigKeyNotSet) {
		fmt.Fprintf(stderr, "gitcd profile unset: %s has no profile pinned through gitcd\n", path)
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "gitcd profile unset: %v\n", err)
		return 1
	}
	keys := strings.Split(recorded, ",")

	if *dryRun {
		for _, key := range keys {
			fmt.Fprintln(stdout, key)
		}
		fmt.Fprintln(stdout, "gitcd.profile")
		fmt.Fprintln(stdout, "gitcd.keys")
		return 0
	}

	var leftovers []string
	for _, key := range keys {
		if err := git.ConfigUnsetLocal(ctx, path, key); err != nil {
			if errors.Is(err, git.ErrConfigKeyNotSet) {
				leftovers = append(leftovers, key)
				continue
			}
			fmt.Fprintf(stderr, "gitcd profile unset: %v\n", err)
			return 1
		}
		logger.Debug("removed a local config key", "path", path, "key", key)
	}

	for _, marker := range []string{"gitcd.profile", "gitcd.keys"} {
		if err := git.ConfigUnsetLocal(ctx, path, marker); err != nil && !errors.Is(err, git.ErrConfigKeyNotSet) {
			fmt.Fprintf(stderr, "gitcd profile unset: %v\n", err)
			return 1
		}
	}

	if len(leftovers) > 0 {
		fmt.Fprintln(stdout, "already removed outside gitcd, nothing left to unset:")
		for _, key := range leftovers {
			fmt.Fprintf(stdout, "  %s\n", key)
		}
		return 1
	}
	return 0
}

// runProfileShow implements "gitcd profile show [path]": it prints the
// profile the configured rules expect for path's remote, the rule that
// matched, and the identity git actually resolves right now, with its
// origin.
func runProfileShow(args []string, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("gitcd profile show", flag.ContinueOnError)
	fset.SetOutput(stderr)
	verbose := fset.Bool("verbose", false, "enable debug logging on stderr")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() > 1 {
		fmt.Fprintln(stderr, "gitcd profile show: at most one path is accepted")
		fset.Usage()
		return 2
	}
	path := "."
	if fset.NArg() == 1 {
		path = fset.Arg(0)
	}
	newLogger(*verbose, stderr)
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd profile show: %v\n", err)
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd profile show: %v\n", err)
		return 1
	}

	repo, err := profile.Inspect(ctx, path, cfg, gitconfig.Dir(home))
	if err != nil {
		fmt.Fprintf(stderr, "gitcd profile show: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "remote: %s\n", repo.RemoteURL)
	fmt.Fprintf(stdout, "expected profile: %s\n", describeExpected(repo))
	fmt.Fprintf(stdout, "user.email: %s\n", describeIdentity(repo.Email))
	fmt.Fprintf(stdout, "user.signingkey: %s\n", describeIdentity(repo.SigningKey))
	return 0
}

func describeExpected(repo profile.Repo) string {
	if repo.ExpectedProfile == "" {
		return "none (no rule matched and no default profile is configured)"
	}
	if repo.MatchedRule == nil {
		return fmt.Sprintf("%s (default profile, no rule matched)", repo.ExpectedProfile)
	}
	rule := repo.MatchedRule
	var fields []string
	if rule.Host != "" {
		fields = append(fields, fmt.Sprintf("host=%q", rule.Host))
	}
	if rule.Owner != "" {
		fields = append(fields, fmt.Sprintf("owner=%q", rule.Owner))
	}
	if rule.Repo != "" {
		fields = append(fields, fmt.Sprintf("repo=%q", rule.Repo))
	}
	return fmt.Sprintf("%s (rule: %s)", repo.ExpectedProfile, strings.Join(fields, " "))
}

func describeIdentity(id profile.Identity) string {
	if id.Value == "" && id.Origin == "" {
		return "(unset)"
	}
	return fmt.Sprintf("%s (%s)", id.Value, originLabel(id.Origin))
}

func originLabel(origin string) string {
	switch origin {
	case profile.OriginLocal:
		return "pinned locally"
	case profile.OriginManaged:
		return "a profile applied through gitcd apply"
	case profile.OriginOther:
		return "set elsewhere"
	default:
		return "unknown"
	}
}
