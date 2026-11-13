// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/git"
	"github.com/ostendorf-cloud/gitcd/internal/layout"
	"github.com/ostendorf-cloud/gitcd/internal/remote"
)

// runOpen implements "gitcd open [query]": it resolves query the same
// way "gitcd path" does, an exact repository name or an already cloned
// "host/owner/repo" shorthand, defaulting to the current directory when
// query is left out, and opens that repository's remote in a browser.
// "--print" prints the URL on stdout instead of launching anything, the
// only path a test exercises: launching a real browser is never
// something a test does.
func runOpen(args []string, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("gitcd open", flag.ContinueOnError)
	fset.SetOutput(stderr)
	printOnly := fset.Bool("print", false, "print the URL instead of opening a browser")
	verbose := fset.Bool("verbose", false, "enable debug logging on stderr")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() > 1 {
		fmt.Fprintln(stderr, "gitcd open: at most one query is accepted")
		fset.Usage()
		return 2
	}
	logger := newLogger(*verbose, stderr)
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd open: %v\n", err)
		return 1
	}

	path, err := resolveOpenTarget(cfg, argOrDefault(fset, "."))
	if err != nil {
		fmt.Fprintf(stderr, "gitcd open: %v\n", err)
		return 1
	}
	logger.Debug("resolved repository", "path", path)

	return openRepo(ctx, stdout, stderr, cfg, path, *printOnly, launchBrowser)
}

// argOrDefault returns fset's one positional argument, or def when none
// was given.
func argOrDefault(fset *flag.FlagSet, def string) string {
	if fset.NArg() == 1 {
		return fset.Arg(0)
	}
	return def
}

// resolveOpenTarget resolves query to a repository path, exactly as
// "gitcd path" would: a literal path or "." is used as given, a query
// containing "/" is a "host/owner/repo" shorthand that must already be
// cloned, and anything else is an exact repository name searched for
// under cfg.Root.
func resolveOpenTarget(cfg config.Config, query string) (string, error) {
	if query == "." || query == "" {
		return query, nil
	}
	if !strings.Contains(query, "/") {
		matches, err := findByName(cfg.Root, query)
		if err != nil {
			return "", fmt.Errorf("search %s: %w", cfg.Root, err)
		}
		switch len(matches) {
		case 0:
			return "", fmt.Errorf("no repository named %q under %s", query, cfg.Root)
		case 1:
			return matches[0], nil
		default:
			return "", fmt.Errorf("%q matches more than one repository: %s", query, strings.Join(matches, ", "))
		}
	}

	r, err := remote.Parse(query, cfg.AliasMap())
	if err != nil {
		return "", err
	}
	rel, err := layout.Render(cfg.LayoutFor(r.Host), r)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(cfg.Root, rel)
	if _, err := os.Stat(dest); err != nil {
		return "", fmt.Errorf("%s has not been cloned yet", dest)
	}
	return dest, nil
}

// openRepo reads path's own remote, renders its web URL and either
// prints it or hands it to launch.
func openRepo(
	ctx context.Context, stdout, stderr io.Writer, cfg config.Config, path string, printOnly bool,
	launch func(context.Context, string) error,
) int {
	remoteURL, err := git.RemoteURL(ctx, path)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd open: %v\n", err)
		return 1
	}
	r, err := remote.Parse(remoteURL, cfg.AliasMap())
	if err != nil {
		fmt.Fprintf(stderr, "gitcd open: parse the remote of %s: %v\n", path, err)
		return 1
	}
	url := r.WebURL()

	if printOnly {
		fmt.Fprintln(stdout, url)
		return 0
	}

	if err := launch(ctx, url); err != nil {
		fmt.Fprintf(stderr, "gitcd open: %v\n", err)
		return 1
	}
	fmt.Fprintf(stderr, "gitcd open: opened %s\n", url)
	return 0
}

// launchBrowser opens url in the platform's own browser command: "open"
// on macOS, "xdg-open" on Linux and the other Unix targets, and "start"
// through "cmd" on Windows. None of these are a dependency gitcd carries;
// every platform already ships the command that opens a URL.
func launchBrowser(ctx context.Context, url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", url)
	case "windows":
		cmd = exec.CommandContext(ctx, "cmd", "/c", "start", "", url)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", url)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("open: launch a browser for %s: %w", url, err)
	}
	return nil
}
