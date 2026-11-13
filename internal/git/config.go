// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// IdentityKeys are the git config keys a profile carries that gitcd's
// "init" refuses to shadow silently: the verified behaviour is that a
// global identity key placed after the include line beats every
// includeIf value, so one of these sitting after an existing include is
// reason enough for "init" to refuse.
var IdentityKeys = []string{"user.name", "user.email", "user.signingkey", "gpg.format", "commit.gpgsign"}

// GlobalConfigPath reports the file "git config --global" reads and
// writes: GIT_CONFIG_GLOBAL when set, matching every test and this
// package's own isolation convention, otherwise "~/.gitconfig" when it
// exists, otherwise the XDG fallback "$XDG_CONFIG_HOME/git/config" when it
// exists, otherwise "~/.gitconfig", the path git itself creates on its
// first global write.
func GlobalConfigPath() (string, error) {
	if p := os.Getenv("GIT_CONFIG_GLOBAL"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("git: find the home directory: %w", err)
	}
	primary := filepath.Join(home, ".gitconfig")
	if _, err := os.Stat(primary); err == nil {
		return primary, nil
	}
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	fallback := filepath.Join(xdg, "git", "config")
	if _, err := os.Stat(fallback); err == nil {
		return fallback, nil
	}
	return primary, nil
}

// ConfigAddGlobal runs "git config --global --add key value", letting git
// itself choose and create the global configuration file rather than gitcd
// writing one by path.
func ConfigAddGlobal(ctx context.Context, key, value string) error {
	cmd := exec.CommandContext(ctx, "git", "config", "--global", "--add", key, value)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git: add global %s: %w: %s", key, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ConfigSetLocal runs "git -C path config --local key value", letting git
// resolve the repository's own local configuration file rather than gitcd
// writing one by path: inside a linked worktree, ".git/config" is not that
// file.
func ConfigSetLocal(ctx context.Context, path, key, value string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "config", "--local", key, value)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git: set local %s in %s: %w: %s", key, path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ErrConfigKeyNotSet is returned by ConfigGetLocal and wrapped by
// ConfigUnsetLocal for a key that does not exist in the queried scope: the
// exit status git itself uses for "not found" when a config key is read or
// removed.
var ErrConfigKeyNotSet = errors.New("git: config key not set")

// ConfigGetLocal runs "git -C path config --local --get key" and returns
// its value. It wraps ErrConfigKeyNotSet when key carries no local value.
func ConfigGetLocal(ctx context.Context, path, key string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "config", "--local", "--get", key)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", fmt.Errorf("git: read local %s in %s: %w", key, path, ErrConfigKeyNotSet)
		}
		return "", fmt.Errorf("git: read local %s in %s: %w", key, path, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ConfigUnsetLocal runs "git -C path config --local --unset-all key",
// removing every local value key carries. It wraps ErrConfigKeyNotSet,
// rather than failing, when key already carries no local value: a caller
// unsetting a recorded key that config drift already removed treats that
// as a leftover to report, not a hard failure.
func ConfigUnsetLocal(ctx context.Context, path, key string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "config", "--local", "--unset-all", key)
	if out, err := cmd.CombinedOutput(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 5 {
			return fmt.Errorf("git: unset local %s in %s: %w", key, path, ErrConfigKeyNotSet)
		}
		return fmt.Errorf("git: unset local %s in %s: %w: %s", key, path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ConfigValue is one git config key's effective value together with the
// file git read it from.
type ConfigValue struct {
	Value string
	// Origin is the absolute path of the file the value came from, or ""
	// when git reports a non file origin such as the command line or an
	// environment variable.
	Origin string
}

// ShowOrigin runs "git -C path config --show-origin --get key" and reports
// the effective value together with its origin file. ok is false when key
// carries no value at all, which is not an error: a profile's signing key
// is commonly unset.
func ShowOrigin(ctx context.Context, path, key string) (value ConfigValue, ok bool, err error) {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "config", "--show-origin", "--get", key)
	out, runErr := cmd.Output()
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) && exitErr.ExitCode() == 1 {
			return ConfigValue{}, false, nil
		}
		return ConfigValue{}, false, fmt.Errorf("git: read %s of %s: %w", key, path, runErr)
	}

	line := strings.TrimRight(string(out), "\n")
	idx := strings.IndexByte(line, '\t')
	if idx == -1 {
		return ConfigValue{Value: line}, true, nil
	}
	origin, val := line[:idx], line[idx+1:]
	origin = strings.TrimPrefix(origin, "file:")
	if origin != "" && !filepath.IsAbs(origin) {
		// git reports a local scope file's origin relative to the
		// directory it ran in, "-C path" in this case, not relative to
		// this process's own working directory.
		abs, err := filepath.Abs(path)
		if err == nil {
			origin = filepath.Join(abs, origin)
		}
	}
	return ConfigValue{Value: val, Origin: origin}, true, nil
}

// Dir runs "git -C path rev-parse --path-format=absolute --git-dir" and
// returns the absolute path of path's own ".git" directory: for a linked
// worktree, this is the worktree's private metadata directory, not the
// repository's common directory.
func Dir(ctx context.Context, path string) (string, error) {
	return revParsePath(ctx, path, "--git-dir")
}

// CommonDir runs "git -C path rev-parse --path-format=absolute
// --git-common-dir" and returns the absolute path of the repository's
// common directory: for an ordinary clone this equals Dir, for a linked
// worktree it is the main checkout's ".git" directory, the one holding the
// local configuration every worktree of that repository shares.
func CommonDir(ctx context.Context, path string) (string, error) {
	return revParsePath(ctx, path, "--git-common-dir")
}

func revParsePath(ctx context.Context, path, arg string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--path-format=absolute", arg)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git: rev-parse %s in %s: %w", arg, path, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// configSectionHeader matches a git config section header line, capturing
// the section name and, when present, the quoted subsection.
var configSectionHeader = regexp.MustCompile(`^\s*\[\s*([A-Za-z0-9_.-]+)(?:\s+"([^"]*)")?\s*\]`)

// configKeyLine matches a git config key assignment line, capturing the
// key name.
var configKeyLine = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9-]*)\s*(?:=.*)?$`)

// IdentityKeyAfterInclude reports whether any of keys sits, in file order,
// after the last bare "[include]" section (no subsection) that content
// carries. "git config --global --add" inserts a new key into that last
// matching section rather than at the end of the file, so a key sitting
// after it there, in the file git ends up with once gitcd's own include
// line is added, would keep beating every includeIf value gitcd adds,
// silently. found is false, with no error, when content carries no bare
// "[include]" section at all, or when no key in keys sits after the one it
// does carry.
func IdentityKeyAfterInclude(content []byte, keys []string) (key string, found bool, err error) {
	lines := strings.Split(string(content), "\n")

	lastIncludeEnd := -1
	inBareInclude := false
	for i, line := range lines {
		if m := configSectionHeader.FindStringSubmatch(line); m != nil {
			inBareInclude = strings.EqualFold(m[1], "include") && m[2] == ""
			continue
		}
		if inBareInclude {
			lastIncludeEnd = i
		}
	}
	if lastIncludeEnd == -1 {
		return "", false, nil
	}

	wanted := make(map[string]bool, len(keys))
	for _, k := range keys {
		wanted[strings.ToLower(k)] = true
	}

	section := ""
	for _, line := range lines[lastIncludeEnd+1:] {
		if m := configSectionHeader.FindStringSubmatch(line); m != nil {
			section = strings.ToLower(m[1])
			continue
		}
		if section == "" {
			continue
		}
		if m := configKeyLine.FindStringSubmatch(line); m != nil {
			full := section + "." + strings.ToLower(m[1])
			if wanted[full] {
				for _, k := range keys {
					if strings.ToLower(k) == full {
						return k, true, nil
					}
				}
			}
		}
	}
	return "", false, nil
}

// sortedConfigPairs returns pairs's keys sorted, so a caller building
// "-c key=value" arguments gets a deterministic order.
func sortedConfigPairs(pairs map[string]string) []string {
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
