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
	"gopkg.in/yaml.v3"
)

// defaultMovedProfileName is the profile "init" moves a global identity
// key into when "--profile" does not say otherwise. targetProfileConflict
// enforces that the chosen name, this default or an explicit "--profile",
// is always a dedicated profile, never an existing one chosen by
// guesswork, and never the profile defaultProfile already names: merging
// into whichever profile happens to already exist would silently
// overwrite a key that profile set on purpose, the moment it happened to
// share a name with one of IdentityKeys, and landing in defaultProfile
// would make the leaked identity the default identity again, every
// unmatched repository's own, exactly the bug this move exists to fix.
const defaultMovedProfileName = "global"

// defaultConfigMode is the mode a write through moveGlobalIdentity gives
// cfgPath when no file sits there yet to carry a mode forward: a user
// identity, name, email and signing key identifier rather than a secret,
// still personal enough that a conservative, owner only default beats a
// world readable one. Documented alongside the preservation behavior in
// docs/adr/0002-config-write-preserves-file-mode.md. Every later write
// instead keeps whatever mode the file already had; see moveGlobalIdentity.
const defaultConfigMode = 0o600

// backupNow is the clock a backup name is derived from. It is a variable
// only so a test can pin the name and plant a file there; nothing else
// assigns it.
var backupNow = time.Now

// backupPathFor returns the path a backup of original is written to: the
// original name plus a UTC timestamp with nanosecond resolution. Two runs
// get two names, so a second run does not collide with the first. The name
// is still predictable, which is why writeBackup refuses an existing file.
func backupPathFor(original string) string {
	return original + ".gitcd-backup-" + backupNow().UTC().Format("20060102T150405.000000000Z")
}

// writeBackup creates path with content, mode 0600, and fails with an error
// wrapping fs.ErrExist when anything already sits there, a file, a
// directory or a symlink, instead of writing into or through it. A
// collision is not resolved by picking another name: the caller aborts, so
// no original file is touched before every backup exists.
func writeBackup(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// backupFile is the one place that decides whether a file gets a backup and
// what is printed about it, for the real run and for --dry-run alike, so the
// announcement cannot drift from the action. Content that is empty, which
// includes a file that does not exist, gets no backup and no line. A dry run
// prints "would back up <path>" and writes nothing. A real run writes the
// backup and returns its path.
func backupFile(dryRun bool, stdout io.Writer, path string, content []byte) (string, error) {
	if len(content) == 0 {
		return "", nil
	}
	if dryRun {
		fmt.Fprintf(stdout, "would back up %s\n", path)
		return "", nil
	}
	backupPath := backupPathFor(path)
	if err := writeBackup(backupPath, content); err != nil {
		return "", err
	}
	fmt.Fprintf(stdout, "backed up %s to %s\n", path, backupPath)
	return backupPath, nil
}

// runInit implements "gitcd init". Beyond adding the "include.path" line
// described below, it first detects every identity key the global git
// configuration sets directly: "user.useConfigOnly" only stops git from
// guessing a default identity, it never suppresses a value that is
// already configured, so a key in this file defeats the refusal every
// profile's "useConfigOnly" is meant to provide for a repository no
// profile reaches. Git takes the last value, so a profile that sets the
// same key still wins while the include sits after the key.
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
	var globalBackedUp bool
	backupGlobalConfig := func() error {
		if globalBackedUp {
			return nil
		}
		backupPath, err := backupFile(*dryRun, stdout, globalPath, content)
		if err != nil {
			return fmt.Errorf("back up %s: %w", globalPath, err)
		}
		globalBackedUp = true
		if backupPath != "" {
			logger.Debug("backed up the global git configuration file", "path", backupPath)
		}
		return nil
	}

	if found := git.FindGlobalIdentityValues(content, git.IdentityKeys); len(found) > 0 {
		after, err := git.IdentityKeysAfterGitcdInclude(content, git.IdentityKeys, includePath)
		if err != nil {
			fmt.Fprintf(stderr, "gitcd init: %v\n", err)
			return 1
		}
		reportGlobalIdentityKeys(stdout, globalPath, found, after, target)

		move := decideMove(*moveIdentity, stdin, stderr, found, target)
		var cfgPath string
		if move {
			cfgPath, err = config.Path()
			if err != nil {
				fmt.Fprintf(stderr, "gitcd init: %v\n", err)
				return 1
			}
			conflict, err := targetProfileConflict(cfgPath, target)
			if err != nil {
				fmt.Fprintf(stderr, "gitcd init: %v\n", err)
				return 1
			}
			if conflict != "" {
				fmt.Fprintf(stderr, "gitcd init: %s\n", conflict)
				return 1
			}
		}
		switch {
		case !move:
			fmt.Fprintln(stdout, "not moving them: pass --move-identity=yes to move them without a prompt, or --move-identity=ask to be asked")
		case *dryRun:
			fmt.Fprintf(stdout, "would move %s into profile %q and remove them from %s\n", describeIdentityKeys(found), target, globalPath)
			// The same backups the real move makes, announced through the
			// same code.
			if err := backupGlobalConfig(); err != nil {
				fmt.Fprintf(stderr, "gitcd init: %v\n", err)
				return 1
			}
			existingYAML, err := os.ReadFile(cfgPath)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				fmt.Fprintf(stderr, "gitcd init: read %s: %v\n", cfgPath, err)
				return 1
			}
			if _, err := backupFile(true, stdout, cfgPath, existingYAML); err != nil {
				fmt.Fprintf(stderr, "gitcd init: back up %s: %v\n", cfgPath, err)
				return 1
			}
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

	if err := backupGlobalConfig(); err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	if *dryRun {
		fmt.Fprintf(stdout, "would add to %s:\n\n[include]\n\tpath = %s\n", globalPath, includePath)
		return 0
	}

	// The managed directory holds identity data, so it is created private
	// here as well, see writeRenderedFiles.
	if err := ensurePrivateDir(filepath.Dir(includePath)); err != nil {
		fmt.Fprintf(stderr, "gitcd init: %v\n", err)
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
	// git doubles every backslash it writes into a file, so a Windows path
	// only appears there in its escaped form.
	escaped := strings.ReplaceAll(includePath, `\`, `\\`)
	return len(content) > 0 &&
		(strings.Contains(string(content), includePath) || strings.Contains(string(content), escaped))
}

// reportGlobalIdentityKeys prints every key found, its value, and the
// profile it would belong in, to stdout: the acceptance this command's
// detection side exists for is naming them, regardless of whether a move
// follows.
//
// Git takes the last value, so what the text says depends on where a key
// sits: after is the subset of found that sits after an include section.
// Such a key wins over every profile the include loads. Any other key
// loses to a profile that sets it, once the gitcd include sits after it.
func reportGlobalIdentityKeys(stdout io.Writer, globalPath string, found []git.GlobalIdentityValue, after []string, target string) {
	fmt.Fprintf(stdout, "%s sets %s directly, outside any profile:\n", globalPath, describeIdentityKeys(found))
	for _, v := range found {
		fmt.Fprintf(stdout, "  %s = %s\n", v.Key, v.Value)
	}
	isAfter := make(map[string]bool, len(after))
	for _, k := range after {
		isAfter[k] = true
	}
	var before []string
	for _, v := range found {
		if !isAfter[v.Key] {
			before = append(before, v.Key)
		}
	}
	fmt.Fprintln(stdout, `"user.useConfigOnly" does not suppress a value configured here. Git takes the last value it reads.`)
	if len(before) > 0 {
		fmt.Fprintf(stdout, "Not after an include section: %s. A profile that sets such a key wins when the gitcd include sits after it.\n", strings.Join(before, ", "))
		fmt.Fprintln(stdout, "Such a key still applies to a repository no profile reaches, or where no profile sets it.")
	}
	if len(after) > 0 {
		fmt.Fprintf(stdout, "After an include section: %s. Such a key wins over every profile.\n", strings.Join(after, ", "))
	}
	fmt.Fprintln(stdout, "A repository's own configuration beats the global file and every profile.")
	fmt.Fprintf(stdout, "They belong in a profile, not in this file. A move targets profile %q.\n", target)
}

// describeIdentityKeys joins found's key names for a one line message.
func describeIdentityKeys(found []git.GlobalIdentityValue) string {
	names := make([]string, len(found))
	for i, v := range found {
		names[i] = v.Key
	}
	return strings.Join(names, ", ")
}

// targetProfileConflict reports, as a message ready to print, why "init"
// must refuse to move an identity key into profile target, or "" when
// target is clear to use. It reads cfgPath itself, with its own, lenient
// parse into only the two fields this check needs, rather than going
// through config.LoadFrom: this is purely a name collision check, which
// must not block on some unrelated problem the rest of the file carries,
// a problem moveGlobalIdentity's own, stricter validation is already
// responsible for catching once there is something to write. The decoder
// stays loose on purpose: it is never given KnownFields, so a field this
// struct does not name, "totallyUnknownKey" for instance, is simply
// ignored here rather than failing this parse, keeping that failure mode
// for the stricter check downstream, the one already responsible for it.
//
// A failure of this lenient parse itself, meaning the two fields it does
// name do not even decode, a "profiles" that is a list rather than a
// mapping for instance, is refused rather than waved through: a parse
// this forgiving failing at all means the file's shape could not be
// understood well enough to say there is no conflict, and "no conflict"
// must never be the answer to "I could not tell."
//
// Two collisions are refused, matching pull request 12's review:
//   - target already names defaultProfile. Never allowed, with no
//     override: the found keys are, by definition, a leaked identity that
//     "useConfigOnly" could not suppress; landing them in defaultProfile
//     would make that same leaked identity the default for every
//     repository no rule matches, again, the exact bug this move exists
//     to fix, just rearmed in a broader form.
//   - target already names an existing profile. Never allowed either,
//     with no override flag: AddProfileKeys overwrites a key by name, so
//     merging into a profile that already exists would silently replace
//     a value chosen on purpose the moment it happened to share a key
//     name with one of git.IdentityKeys. A dedicated profile costs
//     nothing to create under a name nothing else uses yet, so there is
//     no legitimate reason to merge instead, and the refusal names the
//     conflict so the user can rerun with a different "--profile".
func targetProfileConflict(cfgPath, target string) (string, error) {
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("read %s: %w", cfgPath, err)
	}

	var doc struct {
		Profiles       map[string]map[string]any `yaml:"profiles"`
		DefaultProfile string                    `yaml:"defaultProfile"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Sprintf(
			"%s does not parse well enough to check profile %q for a conflict: %v\n"+
				"refusing the move rather than risking a silent overwrite or a silently rearmed default",
			cfgPath, target, err,
		), nil
	}

	if doc.DefaultProfile != "" && doc.DefaultProfile == target {
		return fmt.Sprintf(
			"profile %q is the defaultProfile in %s: moving the leaked identity there would make every repository no rule\n"+
				"matches inherit it again, the exact bug this move exists to fix\n"+
				"choose a different name with --profile, or clear defaultProfile first and run \"gitcd apply\" again",
			target, cfgPath,
		), nil
	}
	if _, ok := doc.Profiles[target]; ok {
		return fmt.Sprintf(
			"profile %q already exists in %s: moving into it could silently overwrite a value chosen on purpose\n"+
				"choose a different, not yet used name with --profile",
			target, cfgPath,
		), nil
	}
	// Configuration validation refuses two profiles whose names differ only
	// in case. Refusing here, before any backup is written, keeps that
	// refusal from coming after both backups exist.
	for name := range doc.Profiles {
		if strings.EqualFold(name, target) {
			return fmt.Sprintf(
				"profile %q in %s differs from %q only in case, and the two would render to one file\n"+
					"choose a different, not yet used name with --profile",
				name, cfgPath, target,
			), nil
		}
	}
	return "", nil
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
// validates that the updated file actually loads, before removing each
// key from the global git configuration. Both files are backed up, the
// global one through backupGlobalConfig, before either is touched, and a
// backup that cannot be created aborts the move.
// Persisting the new copy before touching the original is deliberate: if
// the persist step fails, or the persisted copy does not load, the
// global configuration is never touched at all, and if a later removal
// fails partway through, every value is already safely recorded in the
// profile, not lost.
func moveGlobalIdentity(
	ctx context.Context, found []git.GlobalIdentityValue, target, globalPath string, backupGlobalConfig func() error, stdout io.Writer,
) error {
	cfgPath, err := config.Path()
	if err != nil {
		return err
	}

	existingYAML, err := os.ReadFile(cfgPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", cfgPath, err)
	}

	// Both backups are written before either original is touched, so a
	// backup that cannot be created, an existing file at its path
	// included, aborts the move with both originals unchanged.
	if err := backupGlobalConfig(); err != nil {
		return fmt.Errorf("%w: aborted before changing any file", err)
	}
	if _, err := backupFile(false, stdout, cfgPath, existingYAML); err != nil {
		return fmt.Errorf("back up %s: %w: aborted before changing any file", cfgPath, err)
	}

	keys := make(map[string]string, len(found))
	for _, v := range found {
		keys[v.Key] = v.Value
	}
	updatedYAML, err := config.AddProfileKeys(existingYAML, target, keys)
	if err != nil {
		return fmt.Errorf("%s: %w", cfgPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(cfgPath), err)
	}

	// cfgPath may itself be a symlink, the common shape for a
	// configuration tracked in a separate dotfiles repository: renaming
	// a file onto a symlink's own path replaces the link with an
	// ordinary file, silently detaching it from whatever it pointed at.
	// Resolving it first and writing through the resolved path keeps the
	// link itself, and the dotfiles copy it points at, intact. A cfgPath
	// that does not exist yet, the first run on a machine with no
	// configuration file at all, is not a symlink to resolve, and is
	// written at cfgPath directly. Resolved before the temporary file
	// below is even created: the mode that matters, next, is this
	// target's own, not a symlink's, which carries none of its own.
	writeTarget := cfgPath
	if resolved, err := filepath.EvalSymlinks(cfgPath); err == nil {
		writeTarget = resolved
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("resolve %s: %w", cfgPath, err)
	}

	// writeTarget keeps the mode it already had: a configuration file
	// the user set to, say, 0o600 on purpose must come back 0o600, not
	// silently widened by this write. writeTarget not existing yet is
	// not an error here, only the first-run case defaultConfigMode
	// exists for.
	mode := os.FileMode(defaultConfigMode)
	if info, err := os.Stat(writeTarget); err == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", writeTarget, err)
	}

	// Written to a temporary file first and validated with config.LoadFrom
	// before it ever reaches cfgPath: writing an updated configuration
	// that then fails to load, for instance because the file already
	// carried an unknown key, must never reach the point of removing the
	// identity from the global git configuration below, the comment on
	// this function's own name already argues for persisting before
	// destroying, and persisting something unreadable does not satisfy
	// that argument. Validating against a temporary file, rather than
	// cfgPath itself, means a failure here leaves cfgPath exactly as it
	// was, with nothing left to restore.
	//
	// The temporary file gets an unpredictable name from os.CreateTemp,
	// in the same directory as cfgPath so the later rename stays on one
	// filesystem, rather than a fixed, guessable name: a fixed name is a
	// symlink another process or a leftover from a previous run could
	// already occupy, and os.WriteFile follows a symlink wherever it
	// finds one, which would render this validation copy through that
	// link instead of writing an ordinary file gitcd fully controls.
	tmpFile, err := os.CreateTemp(filepath.Dir(cfgPath), filepath.Base(cfgPath)+".gitcd-validate-tmp-*")
	if err != nil {
		return fmt.Errorf("create a temporary file next to %s: %w", cfgPath, err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	// os.CreateTemp creates its file 0o600; set it to the mode the
	// rename below must carry onto writeTarget instead, computed above.
	if err := tmpFile.Chmod(mode); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("set permissions on %s: %w", tmpPath, err)
	}
	if _, err := tmpFile.Write(updatedYAML); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}

	cfg, err := config.LoadFrom(tmpPath)
	if err != nil {
		return fmt.Errorf("the updated configuration would not load, aborting before touching %s: %w", globalPath, err)
	}
	// A cheap belt and braces on top of LoadFrom succeeding: LoadFrom not
	// erroring only proves the rendered file is valid YAML and a valid
	// gitcd configuration, not that this particular edit actually landed
	// in it, which is exactly the gap a "profiles:" key decoding to a
	// "!!null" scalar opened. identityKeysLanded parses the same bytes
	// that are about to replace cfgPath, through the same loader every
	// other command uses, so it catches a future shape of this class
	// regardless of where in the render path it originates, and it runs
	// before the rename and before the unset loop below, so a failure
	// here guarantees the global keys are never removed.
	if err := identityKeysLanded(cfg, target, found); err != nil {
		return fmt.Errorf("the write that was supposed to move %s did not, aborting before touching %s: %w", describeIdentityKeys(found), globalPath, err)
	}

	if err := os.Rename(tmpPath, writeTarget); err != nil {
		return fmt.Errorf("write %s: %w", writeTarget, err)
	}
	fmt.Fprintf(stdout, "moved %s into profile %q in %s\n", describeIdentityKeys(found), target, writeTarget)

	for _, v := range found {
		if err := git.ConfigUnsetGlobal(ctx, v.Key); err != nil {
			return fmt.Errorf("remove %s from %s: %w", v.Key, globalPath, err)
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

// identityKeysLanded reports an error naming the first of found's keys
// that profile target does not carry in cfg with exactly the value found
// recorded, or nil when every one of them does. cfg is expected to come
// from parsing the exact bytes moveGlobalIdentity is about to rename into
// place, so a non-nil result here means that write does not contain what
// it claims to, and the caller must abort before going anywhere near the
// global git configuration's own keys.
func identityKeysLanded(cfg config.Config, target string, found []git.GlobalIdentityValue) error {
	profile := cfg.Profiles[target]
	for _, v := range found {
		got, ok := profile[v.Key]
		if !ok || got != v.Value {
			return fmt.Errorf("profile %q does not carry %s", target, v.Key)
		}
	}
	return nil
}
