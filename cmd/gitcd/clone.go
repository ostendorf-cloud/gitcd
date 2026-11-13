// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/git"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
	"github.com/ostendorf-cloud/gitcd/internal/layout"
	"github.com/ostendorf-cloud/gitcd/internal/profile"
	"github.com/ostendorf-cloud/gitcd/internal/remote"
)

// runClone implements "gitcd clone <url|shorthand>". It prints the path on
// stdout and is idempotent: a target that already exists with the same
// remote prints that path and exits 0, and one that exists with a
// different remote fails, naming both. "--profile" pins an identity
// profile at clone time through "git clone -c", rather than relying on the
// includeIf chain "gitcd apply" renders; either way, the effective
// identity is read back and logged once the clone finishes.
func runClone(args []string, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("gitcd clone", flag.ContinueOnError)
	fset.SetOutput(stderr)
	dryRun := fset.Bool("dry-run", false, "print the target path without cloning")
	verbose := fset.Bool("verbose", false, "enable debug logging on stderr")
	profileName := fset.String("profile", "", "pin an identity profile at clone time")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() != 1 {
		fmt.Fprintln(stderr, "gitcd clone: exactly one remote or host/owner/repo shorthand is required")
		fset.Usage()
		return 2
	}
	input := fset.Arg(0)
	logger := newLogger(*verbose, stderr)
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd clone: %v\n", err)
		return 1
	}

	var pinned config.Profile
	if *profileName != "" {
		prof, ok := cfg.Profiles[*profileName]
		if !ok {
			fmt.Fprintf(stderr, "gitcd clone: no profile named %q is configured\n", *profileName)
			return 1
		}
		pinned = prof
	}

	r, err := remote.Parse(input, cfg.AliasMap())
	if err != nil {
		fmt.Fprintf(stderr, "gitcd clone: %v\n", err)
		return 1
	}
	logger.Debug("parsed remote", "host", r.Host, "owner", r.Owner, "project", r.Project, "repo", r.Repo)

	rel, err := layout.Render(cfg.LayoutFor(r.Host), r)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd clone: %v\n", err)
		return 1
	}
	dest := filepath.Join(cfg.Root, rel)
	logger.Debug("rendered path", "path", dest)

	if git.IsRepository(dest) {
		code := existingClone(ctx, stdout, stderr, cfg, dest, input, r)
		if code == 0 {
			logIdentity(ctx, stderr, cfg, dest)
		}
		return code
	}

	if *dryRun {
		fmt.Fprintln(stdout, dest)
		return 0
	}

	cloneURL := input
	if remote.IsShorthand(input) {
		protocol := cfg.Hosts[strings.ToLower(r.Host)].Protocol
		cloneURL, err = r.URL(protocol)
		if err != nil {
			fmt.Fprintf(stderr, "gitcd clone: %v\n", err)
			return 1
		}
	}
	logger.Debug("cloning", "url", cloneURL, "dest", dest)

	if err := git.CloneWithConfig(ctx, cloneURL, dest, pinned, stderr); err != nil {
		fmt.Fprintf(stderr, "gitcd clone: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, dest)
	logIdentity(ctx, stderr, cfg, dest)
	return 0
}

// logIdentity resolves the profile the configured rules expect for dest and
// reads back the identity git actually resolves for it right now, through
// the same internal/profile.Repo seam "gitcd profile show" reports through,
// and logs both on stderr: a clone whose identity profile was meant to
// apply automatically then shows whether it actually did, without
// disturbing stdout's single line contract clone's shell wrapper depends
// on.
func logIdentity(ctx context.Context, stderr io.Writer, cfg config.Config, dest string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd clone: %v\n", err)
		return
	}

	repo, err := profile.Inspect(ctx, dest, cfg, gitconfig.Dir(home))
	if err != nil {
		fmt.Fprintf(stderr, "gitcd clone: read back the identity of %s: %v\n", dest, err)
		return
	}

	fmt.Fprintf(stderr, "gitcd clone: expected profile: %s\n", describeExpected(repo))
	fmt.Fprintf(stderr, "gitcd clone: user.email: %s\n", describeIdentity(repo.Email))
	fmt.Fprintf(stderr, "gitcd clone: user.signingkey: %s\n", describeIdentity(repo.SigningKey))
}

// existingClone handles "gitcd clone" for a dest that already holds a
// repository: it reads dest's own origin remote and compares it, by its
// parsed fields rather than its literal text, against the one just
// requested, so an SSH and an HTTPS URL for the same repository are not
// mistaken for a conflict.
func existingClone(
	ctx context.Context, stdout, stderr io.Writer, cfg config.Config, dest, input string, want remote.Remote,
) int {
	existingURL, err := git.RemoteURL(ctx, dest)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd clone: %s already exists: %v\n", dest, err)
		return 1
	}

	existing, err := remote.Parse(existingURL, cfg.AliasMap())
	if err != nil {
		fmt.Fprintf(
			stderr, "gitcd clone: %s already exists with a remote gitcd cannot parse: %s\n", dest, existingURL,
		)
		return 1
	}

	if existing == want {
		fmt.Fprintln(stdout, dest)
		return 0
	}

	fmt.Fprintf(
		stderr, "gitcd clone: %s already exists with a different remote: have %s, want %s\n", dest, existingURL, input,
	)
	return 1
}
