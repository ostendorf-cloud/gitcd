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

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/git"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
	"github.com/ostendorf-cloud/gitcd/internal/layout"
	"github.com/ostendorf-cloud/gitcd/internal/profile"
	"github.com/ostendorf-cloud/gitcd/internal/remote"
)

// runAdopt implements "gitcd adopt <path>": it moves an existing clone,
// not necessarily made by gitcd, into the path the configured layout
// would render for its own remote, fixes a "gitcd.profile" pin that no
// longer names the profile the rules currently imply, and prints the old
// and the new path.
//
// It refuses outright, before asking anything, a repository with
// "extensions.worktreeConfig" enabled: that setting lets a worktree carry
// its own copy of a config key gitcd's identity writes normally assume
// are shared, in "config.worktree" rather than the common "config" file,
// and nothing here resolves which of the two a given key would need, so
// guessing is refused rather than risking a write to the wrong one.
//
// It also refuses outright a linked worktree, and a main checkout that
// still has one: a linked worktree's ".git" file names an absolute path
// into its main checkout's own ".git/worktrees", and the main checkout's
// own administrative files name an absolute path back, so moving either
// end breaks the pair the moment the other is not moved along with it,
// git.IsRepository's own guard accepting the ".git" file a linked
// worktree carries just as readily as an ordinary clone's ".git"
// directory. Repairing both ends with "git worktree repair" after the
// move is possible, but adopt only moves one path per run, so it refuses
// rather than silently taking on the job of repairing a pair of
// directories it was not asked to move together.
//
// Moving the directory asks first, on stderr, reading the answer from
// stdin: the safe default is to never move anything, so a closed or
// empty stdin, and an unrecognised answer, both decline. "--yes" skips
// the prompt for a script that already consents. Only one path is
// accepted per run; adopting a whole tree is running this in a loop, the
// same choice issue #4 makes for "migrate".
func runAdopt(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fset := flag.NewFlagSet("gitcd adopt", flag.ContinueOnError)
	fset.SetOutput(stderr)
	yes := fset.Bool("yes", false, "move without asking first")
	dryRun := fset.Bool("dry-run", false, "print the old and new path without moving or writing anything")
	verbose := fset.Bool("verbose", false, "enable debug logging on stderr")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if fset.NArg() != 1 {
		fmt.Fprintln(stderr, "gitcd adopt: exactly one path is required")
		fset.Usage()
		return 2
	}
	logger := newLogger(*verbose, stderr)
	ctx := context.Background()

	oldPath, err := filepath.Abs(fset.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "gitcd adopt: %v\n", err)
		return 1
	}
	if !git.IsRepository(oldPath) {
		if _, statErr := os.Stat(oldPath); errors.Is(statErr, os.ErrNotExist) {
			fmt.Fprintf(stderr, "gitcd adopt: %s does not exist\n", oldPath)
			return 1
		}
		if git.IsBareRepository(ctx, oldPath) {
			fmt.Fprintf(stderr, "gitcd adopt: %s is a bare repository; adopt does not support one\n", oldPath)
			return 1
		}
		fmt.Fprintf(stderr, "gitcd adopt: %s is not a git repository\n", oldPath)
		return 1
	}

	worktreeMsg, err := adoptWorktreeConflict(ctx, oldPath)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd adopt: %v\n", err)
		return 1
	}
	if worktreeMsg != "" {
		fmt.Fprintf(stderr, "gitcd adopt: %s\n", worktreeMsg)
		return 1
	}

	worktreeConfig, ok, err := git.ConfigGetLocalBool(ctx, oldPath, "extensions.worktreeConfig")
	if err != nil {
		fmt.Fprintf(stderr, "gitcd adopt: %v\n", err)
		return 1
	}
	if ok && worktreeConfig {
		fmt.Fprintf(
			stderr,
			"gitcd adopt: %s has extensions.worktreeConfig enabled\n"+
				"a key gitcd would fix could belong in \"config\" or in \"config.worktree\"; adopt refuses to guess\n"+
				"disable extensions.worktreeConfig, or fix this repository's identity by hand, before adopting it\n",
			oldPath,
		)
		return 1
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd adopt: %v\n", err)
		return 1
	}

	remoteURL, err := git.RemoteURL(ctx, oldPath)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd adopt: read the origin remote of %s: %v\n", oldPath, err)
		return 1
	}
	r, err := remote.Parse(remoteURL, cfg.AliasMap())
	if err != nil {
		fmt.Fprintf(stderr, "gitcd adopt: %v\n", err)
		return 1
	}

	rel, err := layout.Render(cfg.LayoutFor(r.Host), r)
	if err != nil {
		fmt.Fprintf(stderr, "gitcd adopt: %v\n", err)
		return 1
	}
	newPath := filepath.Join(cfg.Root, rel)
	logger.Debug("rendered the target path", "old", oldPath, "new", newPath)

	if newPath == oldPath {
		fmt.Fprintln(stdout, oldPath)
		fmt.Fprintln(stdout, newPath)
		fmt.Fprintln(stderr, "gitcd adopt: already at the path the configured layout renders; nothing to move")
		return fixAdoptedConfig(ctx, stderr, cfg, r, newPath)
	}

	if conflict := adoptTargetConflict(ctx, cfg, newPath, r); conflict != "" {
		fmt.Fprintf(stderr, "gitcd adopt: %s\n", conflict)
		return 1
	}

	if rel, err := filepath.Rel(oldPath, newPath); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "." {
		fmt.Fprintf(stderr, "gitcd adopt: %s is inside %s; adopt refuses to move a repository into itself\n", newPath, oldPath)
		return 1
	}

	if *dryRun {
		fmt.Fprintln(stdout, oldPath)
		fmt.Fprintln(stdout, newPath)
		return 0
	}

	if !*yes {
		fmt.Fprintf(stderr, "move %s to %s? [y/N] ", oldPath, newPath)
		if !readYes(stdin) {
			fmt.Fprintln(stderr, "gitcd adopt: not moving: pass --yes to move without asking")
			return 1
		}
	}

	mkdirTarget := filepath.Dir(newPath)
	createdFrom := firstMissingAncestor(mkdirTarget)
	if err := os.MkdirAll(mkdirTarget, 0o755); err != nil {
		if removeErr := removeCreatedAncestor(createdFrom); removeErr != nil {
			fmt.Fprintf(
				stderr, "gitcd adopt: create %s: %v (also failed to remove %s, created for the move: %v)\n",
				mkdirTarget, err, createdFrom, removeErr,
			)
			return 1
		}
		fmt.Fprintf(stderr, "gitcd adopt: create %s: %v\n", mkdirTarget, err)
		return 1
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		if removeErr := removeCreatedAncestor(createdFrom); removeErr != nil {
			fmt.Fprintf(
				stderr, "gitcd adopt: move %s to %s: %v (also failed to remove %s, created for the move: %v)\n",
				oldPath, newPath, err, createdFrom, removeErr,
			)
			return 1
		}
		fmt.Fprintf(stderr, "gitcd adopt: move %s to %s: %v\n", oldPath, newPath, err)
		return 1
	}

	fmt.Fprintln(stdout, oldPath)
	fmt.Fprintln(stdout, newPath)
	return fixAdoptedConfig(ctx, stderr, cfg, r, newPath)
}

// adoptWorktreeConflict reports why oldPath must refuse to move at all,
// before anything else about it is even read, because it is a linked
// worktree or still has one of its own: a linked worktree's ".git" file
// names an absolute path into its main checkout's own
// ".git/worktrees", and the main checkout's own administrative files
// name an absolute path back, so moving either end without moving the
// other breaks the pair, silently, the moment a routine "git worktree
// prune" or "git gc" next runs. It returns "" when oldPath carries no
// linked worktree relationship at all.
func adoptWorktreeConflict(ctx context.Context, oldPath string) (string, error) {
	gitDir, err := git.Dir(ctx, oldPath)
	if err != nil {
		return "", fmt.Errorf("read the git directory of %s: %w", oldPath, err)
	}
	commonDir, err := git.CommonDir(ctx, oldPath)
	if err != nil {
		return "", fmt.Errorf("read the common directory of %s: %w", oldPath, err)
	}
	if gitDir != commonDir {
		return fmt.Sprintf(
			"%s is a linked worktree; moving it would break git's own link back to its main checkout\n"+
				"move a linked worktree with \"git worktree move %s <destination>\" instead",
			oldPath, oldPath,
		), nil
	}

	worktrees, err := git.Worktrees(ctx, oldPath)
	if err != nil {
		return "", fmt.Errorf("list the worktrees of %s: %w", oldPath, err)
	}
	// git reports every worktree path resolved through any symlink,
	// "git rev-parse --path-format=absolute" style; oldPath is only
	// "filepath.Abs" of whatever the user typed, so the two are compared
	// resolved the same way, or oldPath itself would misidentify as its
	// own, separate linked worktree whenever a symlink sits on its path,
	// a system temporary directory being the common case.
	resolvedOld := resolveSymlinks(oldPath)
	var others []string
	for _, w := range worktrees {
		if resolveSymlinks(w) != resolvedOld {
			others = append(others, w)
		}
	}
	if len(others) == 0 {
		return "", nil
	}
	return fmt.Sprintf(
		"%s has a linked worktree at %s; moving it would break git's own link to it\n"+
			"move the linked worktree first with \"git worktree move\", or remove it with \"git worktree remove\", then adopt this repository",
		oldPath, strings.Join(others, ", "),
	), nil
}

// resolveSymlinks resolves every symlink in path, falling back to path
// itself when it cannot, empty or not yet created being the common case.
func resolveSymlinks(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// firstMissingAncestor returns the topmost directory, walking up from
// dir, that does not yet exist on disk, or "" when dir already exists:
// "os.MkdirAll(dir, ...)" creates exactly this directory and everything
// below it, so "os.RemoveAll" on the same path undoes that MkdirAll
// precisely, leaving every ancestor directory that already existed
// before untouched.
func firstMissingAncestor(dir string) string {
	if _, err := os.Stat(dir); err == nil {
		return ""
	}
	missing := dir
	for {
		parent := filepath.Dir(missing)
		if parent == missing {
			return missing
		}
		if _, err := os.Stat(parent); err == nil {
			return missing
		}
		missing = parent
	}
}

// removeCreatedAncestor removes createdFrom, the directory
// firstMissingAncestor found before the "os.MkdirAll" that followed,
// undoing exactly what that call created. It is a no-op, successfully,
// when createdFrom is "": mkdirTarget already existed, so "os.MkdirAll"
// created nothing to undo. Calling it after either "os.MkdirAll" itself
// or the "os.Rename" that follows it fails leaves no directory behind
// either way, instead of only the latter.
func removeCreatedAncestor(createdFrom string) error {
	if createdFrom == "" {
		return nil
	}
	return os.RemoveAll(createdFrom)
}

// adoptTargetConflict reports why newPath must refuse to receive oldPath's
// repository, or "" when the move is clear to make: newPath already
// existing is always a conflict, named with the remote already found
// there when it can be read, since that is the detail a user deciding
// what to do next needs most. An "os.Stat" error other than "the path
// does not exist", a permission denied or a symlink loop for instance,
// is reported as a conflict too, rather than read as "no conflict": the
// move itself would very likely fail the same way, after having already
// asked for consent, and newPath might in fact already exist behind
// whatever is blocking the check.
func adoptTargetConflict(ctx context.Context, cfg config.Config, newPath string, want remote.Remote) string {
	if _, err := os.Stat(newPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ""
		}
		return fmt.Sprintf("cannot check whether %s already exists: %v", newPath, err)
	}
	if !git.IsRepository(newPath) {
		return fmt.Sprintf("%s already exists and is not a git repository", newPath)
	}
	existingURL, err := git.RemoteURL(ctx, newPath)
	if err != nil {
		return fmt.Sprintf("%s already exists: %v", newPath, err)
	}
	existing, err := remote.Parse(existingURL, cfg.AliasMap())
	if err != nil || existing != want {
		return fmt.Sprintf("%s already exists with a different remote: %s", newPath, existingURL)
	}
	return fmt.Sprintf("%s already exists with the same remote: it looks already adopted", newPath)
}

// fixAdoptedConfig brings newPath's identity in line with what the
// configured rules currently imply for r, the newly adopted repository's
// own remote, then logs it: the same expected-versus-effective report
// "gitcd clone" already prints after a clone. Only a pin gitcd itself
// wrote, the "gitcd.profile" marker, is ever rewritten here, and only
// when it already names a different profile than the rules now imply;
// a repository with no pin at all is left to the includeIf chain, the
// same common case mechanism a plain "gitcd clone" relies on, so adopt
// never invents a pin a clone would not have made either. An identity
// key set directly in this repository's own local configuration, with
// no "gitcd.profile" marker to say gitcd wrote it, is reported, not
// touched: adopt never overwrites a value it did not write.
func fixAdoptedConfig(ctx context.Context, stderr io.Writer, cfg config.Config, r remote.Remote, newPath string) int {
	res := profile.Resolve(r, cfg)

	pinned, err := git.ConfigGetLocal(ctx, newPath, "gitcd.profile")
	switch {
	case err == nil:
		if res.Profile != "" && pinned != res.Profile {
			prof, ok := cfg.Profiles[res.Profile]
			if !ok {
				fmt.Fprintf(stderr, "gitcd adopt: %s is pinned to %q, which no longer exists; left as is\n", newPath, pinned)
				break
			}
			for _, key := range prof.Keys() {
				if err := git.ConfigSetLocal(ctx, newPath, key, prof[key]); err != nil {
					fmt.Fprintf(stderr, "gitcd adopt: %v\n", err)
					return 1
				}
			}
			if err := writeProfileMarkers(ctx, newPath, res.Profile, prof); err != nil {
				fmt.Fprintf(stderr, "gitcd adopt: %v\n", err)
				return 1
			}
			fmt.Fprintf(stderr, "gitcd adopt: updated the pin from %q to %q, the profile the rules now imply\n", pinned, res.Profile)
		}
	case errors.Is(err, git.ErrConfigKeyNotSet):
		// No pin: the includeIf chain is left to apply the identity, the
		// same mechanism an ordinary "gitcd clone" relies on without a
		// "--profile" flag.
	default:
		fmt.Fprintf(stderr, "gitcd adopt: read gitcd.profile of %s: %v\n", newPath, err)
		return 1
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "gitcd adopt: %v\n", err)
		return 1
	}
	repo, err := profile.Inspect(ctx, newPath, cfg, gitconfig.Dir(home))
	if err != nil {
		fmt.Fprintf(stderr, "gitcd adopt: read back the identity of %s: %v\n", newPath, err)
		return 1
	}
	if repo.Email.Origin == profile.OriginOther {
		fmt.Fprintf(
			stderr, "gitcd adopt: %s sets user.email directly, outside any profile gitcd manages; left as is\n", newPath,
		)
	}
	fmt.Fprintf(stderr, "gitcd adopt: expected profile: %s\n", describeExpected(repo))
	fmt.Fprintf(stderr, "gitcd adopt: user.email: %s\n", describeIdentity(repo.Email))
	fmt.Fprintf(stderr, "gitcd adopt: user.signingkey: %s\n", describeIdentity(repo.SigningKey))
	return 0
}

// readYes reads one line from stdin and reports whether it is "y" or
// "yes", case insensitively. A closed or empty stdin, the shape a script
// or CI job's stdin commonly has, reaches EOF immediately and reports
// false, the safe default, without blocking.
func readYes(stdin io.Reader) bool {
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}
