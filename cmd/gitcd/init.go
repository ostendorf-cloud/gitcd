// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/git"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
)

// defaultMovedProfileName is the profile "init" moves a global identity
// key into when "--profile" does not say otherwise. It is always a
// dedicated profile, never an existing one chosen by guesswork: merging
// into whichever profile happens to already exist would silently
// overwrite a key that profile set on purpose, the moment it happened to
// share a name with one of IdentityKeys. A fresh, dedicated profile is
// never ambiguous and is always safe to merge into again on a later run.
const defaultMovedProfileName = "global"

// runInit implements "gitcd init". Beyond adding the "include.path" line
// described below, it first detects every identity key the global git
// configuration sets directly: "user.useConfigOnly" only stops git from
// guessing a default identity, it never suppresses a value that is
// already configured, so a key sitting anywhere in this file, matched by
// gitcd's own includeIf chain or not, defeats the refusal every profile's
// "useConfigOnly" is meant to provide for a repository no rule matches.
// "init" reports every such key, names the profile it would belong in,
// and, only when asked, moves it there and removes it from the global
// file, with a backup of both files it touches.
//
// "init" also adds exactly one "include.path" line to the user's global
// git configuration, pointing at the include file "gitcd apply" renders,
// so every identity profile rule actually takes effect. It refuses when a
// global identity key already sits after the last existing "[include]"
// section, since "git config --global --add" inserts a new key into that
// section rather than at the end of the file, and a key sitting after it
// would keep beating every includeIf value gitcd adds, silently.
func runInit(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("gitcd init", flag.ContinueOnError)
	fset.SetOutput(stderr)
	dryRun := fset.Bool("dry-run", false, "print what would change without writing anything")
	verbose := fset.Bool("verbose", false, "enable debug logging on stderr")
	moveIdentity := fset.String(
		"move-identity", "",
		`what to do with an identity key found directly in the global git configuration:
"yes" moves it without a prompt, "ask" prompts on stderr and reads the answer from stdin,
"no", the default, only reports it`,
	)
	profileFlag := fset.String("profile", "", "profile a moved identity key is written into (default \""+defaultMovedProfileName+"\")")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() != 0 {
		fmt.Fprintln(stderr, "gitcd init: no arguments are accepted")
		fset.Usage()
		return 2
	}
	switch *moveIdentity {
	case "", "no", "yes", "ask":
	default:
		fmt.Fprintf(stderr, "gitcd init: --move-identity must be \"yes\", \"ask\" or \"no\", got %q\n", *moveIdentity)
		return 2
	}
	target := defaultMovedProfileName
	if *profileFlag != "" {
		target = *profileFlag
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

	// A single backup of globalPath as it was found, shared between the
	// identity move below and the include line further down: both can
	// write to globalPath in the same run, and a second, later backup
	// would either collide with the first, since both resolve to the
	// same once per second timestamp, or, if it did not collide, record
	// an already modified file instead of the original one.
	var backupPath string
	backupGlobalConfig := func() error {
		if backupPath != "" || len(content) == 0 {
			return nil
		}
		backupPath = globalPath + ".gitcd-backup-" + time.Now().UTC().Format("20060102T150405Z")
		if err := os.WriteFile(backupPath, content, 0o600); err != nil {
			backupPath = ""
			return fmt.Errorf("gitcd init: back up %s: %w", globalPath, err)
		}
		logger.Debug("backed up the global git configuration file", "path", backupPath)
		fmt.Fprintf(stdout, "backed up %s to %s\n", globalPath, backupPath)
		return nil
	}

	if found := git.FindGlobalIdentityValues(content, git.IdentityKeys); len(found) > 0 {
		reportGlobalIdentityKeys(stdout, globalPath, found, target)

		move := decideMove(*moveIdentity, stdin, stderr, found, target)
		switch {
		case !move:
			fmt.Fprintln(stdout, "not moving them: pass --move-identity=yes to move them without a prompt, or --move-identity=ask to be asked")
		case *dryRun:
			fmt.Fprintf(stdout, "would move %s into profile %q and remove them from %s\n", describeIdentityKeys(found), target, globalPath)
		default:
			if err := moveGlobalIdentity(ctx, found, target, globalPath, backupGlobalConfig, stdout); err != nil {
				fmt.Fprintf(stderr, "gitcd init: %v\n", err)
				return 1
			}
			refreshed, err := os.ReadFile(globalPath)
			if err != nil {
				fmt.Fprintf(stderr, "gitcd init: read %s: %v\n", globalPath, err)
				return 1
			}
			content = refreshed
		}
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

	if err := backupGlobalConfig(); err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
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

// reportGlobalIdentityKeys prints every key found, its value, and the
// profile it would belong in, to stdout: the acceptance this command's
// detection side exists for is naming them, regardless of whether a move
// follows.
func reportGlobalIdentityKeys(stdout io.Writer, globalPath string, found []git.GlobalIdentityValue, target string) {
	fmt.Fprintf(stdout, "%s sets %s directly, outside any profile:\n", globalPath, describeIdentityKeys(found))
	for _, v := range found {
		fmt.Fprintf(stdout, "  %s = %s\n", v.Key, v.Value)
	}
	fmt.Fprintln(stdout, `"user.useConfigOnly" only stops git from guessing a default identity; it does`)
	fmt.Fprintln(stdout, "not suppress a value already configured here, so every repository gitcd")
	fmt.Fprintln(stdout, "manages, including one no rule matches, still gets an identity from this file.")
	fmt.Fprintf(stdout, "They belong in profile %q instead.\n", target)
}

// describeIdentityKeys joins found's key names for a one line message.
func describeIdentityKeys(found []git.GlobalIdentityValue) string {
	names := make([]string, len(found))
	for i, v := range found {
		names[i] = v.Key
	}
	return strings.Join(names, ", ")
}

// decideMove reports whether found should be moved, given moveIdentity,
// the "--move-identity" flag's value: "yes" moves without asking, "no"
// and the unset default never move and never read stdin, and "ask" prompts
// on stderr and reads the answer from stdin, defaulting to "no" on
// anything other than an explicit "y" or "yes", EOF included, so a closed
// or empty stdin, the shape a script or CI job's stdin commonly has,
// resolves to the safe answer without blocking.
func decideMove(moveIdentity string, stdin io.Reader, stderr io.Writer, found []git.GlobalIdentityValue, target string) bool {
	switch moveIdentity {
	case "yes":
		return true
	case "ask":
		fmt.Fprintf(stderr, "move %s into profile %q and remove them from the global git configuration? [y/N] ", describeIdentityKeys(found), target)
		line, _ := bufio.NewReader(stdin).ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		return answer == "y" || answer == "yes"
	default:
		return false
	}
}

// moveGlobalIdentity moves found into profile target: it writes the
// updated profile into the configuration file config.Path resolves,
// backing it up first when it already exists, before removing each key
// from the global git configuration, backing that up through
// backupGlobalConfig first. Persisting the new copy before touching the
// original is deliberate: if the persist step fails, the global
// configuration is never touched at all, and if a later removal fails
// partway through, every value is already safely recorded in the
// profile, not lost.
func moveGlobalIdentity(
	ctx context.Context, found []git.GlobalIdentityValue, target, globalPath string, backupGlobalConfig func() error, stdout io.Writer,
) error {
	cfgPath, err := config.Path()
	if err != nil {
		return fmt.Errorf("gitcd init: %w", err)
	}

	existingYAML, err := os.ReadFile(cfgPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("gitcd init: read %s: %w", cfgPath, err)
	}
	if len(existingYAML) > 0 {
		cfgBackupPath := cfgPath + ".gitcd-backup-" + time.Now().UTC().Format("20060102T150405Z")
		if err := os.WriteFile(cfgBackupPath, existingYAML, 0o600); err != nil {
			return fmt.Errorf("gitcd init: back up %s: %w", cfgPath, err)
		}
		fmt.Fprintf(stdout, "backed up %s to %s\n", cfgPath, cfgBackupPath)
	}

	keys := make(map[string]string, len(found))
	for _, v := range found {
		keys[v.Key] = v.Value
	}
	updatedYAML, err := config.AddProfileKeys(existingYAML, target, keys)
	if err != nil {
		return fmt.Errorf("gitcd init: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return fmt.Errorf("gitcd init: create %s: %w", filepath.Dir(cfgPath), err)
	}
	if err := os.WriteFile(cfgPath, updatedYAML, 0o644); err != nil {
		return fmt.Errorf("gitcd init: write %s: %w", cfgPath, err)
	}
	fmt.Fprintf(stdout, "moved %s into profile %q in %s\n", describeIdentityKeys(found), target, cfgPath)

	if err := backupGlobalConfig(); err != nil {
		return err
	}
	for _, v := range found {
		if err := git.ConfigUnsetGlobal(ctx, v.Key); err != nil {
			return fmt.Errorf("gitcd init: remove %s from %s: %w", v.Key, globalPath, err)
		}
	}
	fmt.Fprintf(stdout, "removed %s from %s\n", describeIdentityKeys(found), globalPath)
	fmt.Fprintf(
		stdout,
		"profile %q is not applied automatically: add a rule, or set it as the defaultProfile, then run \"gitcd apply\"\n"+
			"to reverse this, restore the backups above, or set the keys shown earlier back with \"git config --global\"\n",
		target,
	)
	return nil
}
