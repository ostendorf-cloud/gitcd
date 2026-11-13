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
	"path/filepath"
	"strings"
	"time"

	"github.com/ostendorf-cloud/gitcd/internal/git"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
)

// runInit implements "gitcd init": it adds exactly one "include.path" line
// to the user's global git configuration, pointing at the include file
// "gitcd apply" renders, so every identity profile rule actually takes
// effect. It refuses when a global identity key already sits after the
// last existing "[include]" section, since "git config --global --add"
// inserts a new key into that section rather than at the end of the file,
// and a key sitting after it would keep beating every includeIf value
// gitcd adds, silently.
func runInit(args []string, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("gitcd init", flag.ContinueOnError)
	fset.SetOutput(stderr)
	dryRun := fset.Bool("dry-run", false, "print what would change without writing anything")
	verbose := fset.Bool("verbose", false, "enable debug logging on stderr")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() != 0 {
		fmt.Fprintln(stderr, "gitcd init: no arguments are accepted")
		fset.Usage()
		return 2
	}
	logger := newLogger(*verbose, stderr)
	ctx := context.Background()

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd init: %v\n", err)
		return 1
	}
	includePath := gitconfig.IncludePath(home)

	globalPath, err := git.GlobalConfigPath()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd init: %v\n", err)
		return 1
	}
	logger.Debug("resolved the global git configuration file", "path", globalPath)

	content, err := os.ReadFile(globalPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(stderr, "gitcd init: read %s: %v\n", globalPath, err)
		return 1
	}

	if alreadyIncluded(content, includePath) {
		fmt.Fprintf(stdout, "gitcd is already initialized: %s already includes %s\n", globalPath, includePath)
		return 0
	}

	if key, found, checkErr := git.IdentityKeyAfterInclude(content, git.IdentityKeys); checkErr != nil {
		fmt.Fprintf(stderr, "gitcd init: %v\n", checkErr)
		return 1
	} else if found {
		fmt.Fprintf(
			stderr,
			"gitcd init: %s sets %s after an existing [include] section\n"+
				"gitcd would add its own include after that one, so %s would keep beating every profile\n"+
				"move %s above the [include] section, or into it, before running init again\n",
			globalPath, key, key, key,
		)
		return 1
	}

	if *dryRun {
		fmt.Fprintf(stdout, "would back up %s and add:\n\n[include]\n\tpath = %s\n", globalPath, includePath)
		return 0
	}

	if len(content) > 0 {
		backupPath := globalPath + ".gitcd-backup-" + time.Now().UTC().Format("20060102T150405Z")
		if err := os.WriteFile(backupPath, content, 0o600); err != nil {
			fmt.Fprintf(stderr, "gitcd init: back up %s: %v\n", globalPath, err)
			return 1
		}
		logger.Debug("backed up the global git configuration file", "path", backupPath)
		fmt.Fprintf(stdout, "backed up %s to %s\n", globalPath, backupPath)
	}

	if err := os.MkdirAll(filepath.Dir(includePath), 0o755); err != nil {
		fmt.Fprintf(stderr, "gitcd init: create %s: %v\n", filepath.Dir(includePath), err)
		return 1
	}
	if err := git.ConfigAddGlobal(ctx, "include.path", includePath); err != nil {
		fmt.Fprintf(stderr, "gitcd init: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "added include.path = %s to %s\n", includePath, globalPath)
	fmt.Fprintln(stdout, "run \"gitcd apply\" to render the profile files this include reads")
	return 0
}

// alreadyIncluded reports whether content already names includePath as an
// include path: a light textual check, since "gitcd apply" always renders
// the same absolute path for a given home directory, and that path is
// distinctive enough on its own.
func alreadyIncluded(content []byte, includePath string) bool {
	return len(content) > 0 && strings.Contains(string(content), includePath)
}
