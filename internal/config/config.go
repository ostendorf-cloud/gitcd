// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package config loads gitcd's own configuration: the clone root, the path
// layout per host, the alias map a host's SSH host names resolve through,
// and the identity profiles and rules that decide which profile applies to
// a remote. It never writes git configuration itself, which belongs to
// internal/git and internal/gitconfig.
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultRoot is the clone root applied when "root" is left out of the
// configuration file, or no file exists at all.
const DefaultRoot = "~/Code"

// DefaultLayout is the path template applied to a host without its own
// "layout" key, and to every host when "layout.default" is left out.
const DefaultLayout = "{host}/{owner}/{repo}"

// defaultAzureLayout is dev.azure.com's own built in layout, carrying the
// project segment DefaultLayout has no placeholder for. remote.Parse
// recognises Azure DevOps unconditionally, so Default ships a matching
// entry: without one, a host with no configuration file falls back to
// DefaultLayout, which drops the project and renders the SSH and HTTPS
// forms of one repository into two different paths.
const defaultAzureLayout = "{host}/{org}/{project}/{repo}"

// ErrUnknownKey is wrapped into the error Load returns when the
// configuration file carries a key this package does not recognise.
var ErrUnknownKey = errors.New("config: unknown key")

// ErrInvalidProfile is wrapped into the error Load returns when a profile,
// a rule or defaultProfile is malformed: a profile key that is not a valid
// "section.key" git config key, a profile key under the "remote" section, a
// rule naming a profile that does not exist, a rule with no host, a rule
// with a repository but no owner, or a defaultProfile naming a profile that
// does not exist.
var ErrInvalidProfile = errors.New("config: invalid profile configuration")

// configKeySection matches the section a git config key's "section.key" or
// "section.subsection.key" form starts with: a letter followed by letters,
// digits or "-".
var configKeySection = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)

// configKeyName matches the final segment of a git config key, after its
// last ".", and a key's subsection, which ValidateProfileKey holds to the
// same character class.
var configKeyName = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// ruleHost matches a rule's host: a host name, with no glob
// metacharacter, no quote and no path separator, so it cannot widen the
// glob or close the condition it is rendered into.
var ruleHost = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)

// ruleSegment matches one segment of a rule's owner, and a rule's repo
// whole, on the same terms as ruleHost.
var ruleSegment = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// profileName matches a profile name, which becomes one path segment:
// the name of the file "apply" renders the profile to. It has to begin
// and end with a letter or a digit, so the rendered file is neither
// hidden nor named in a form Windows silently rewrites.
var profileName = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)

// Profile is one named identity: a map from a git config key, in
// "section.key" form, to the string value it should carry. Values are git
// config strings: a boolean is written as "true" or "false", quoted like
// every other value.
type Profile map[string]string

// Rule matches a remote to a Profile by name. Host is required; Owner and
// Repo narrow the match further, Repo only meaningful alongside Owner. An
// empty field matches every value of that field.
type Rule struct {
	Host    string
	Owner   string
	Repo    string
	Profile string
}

// Host is one entry of the "hosts" map: the per host path layout, the SSH
// host aliases that resolve to it, and the protocol clone prefers.
type Host struct {
	// Layout is this host's own path template, or DefaultLayout when the
	// "layout" key under this host is left out.
	Layout string
	// Aliases are further host names, lowercased, that resolve to this
	// host. The map is not optional: two accounts on one forge are
	// commonly driven by distinct SSH host aliases bound to distinct
	// keys, and without the map the layout breaks for exactly that case.
	Aliases []string
	// Protocol is the clone protocol this host prefers, "ssh" or
	// "https". Empty when the configuration does not state one.
	Protocol string
}

// Config is gitcd's own configuration, loaded from ~/.gitcd.yaml or its
// XDG fallback.
type Config struct {
	// Root is the clone root, with a leading "~" already expanded to the
	// loading user's home directory.
	Root string
	// Default is the path template applied to a host with no entry, or no
	// "layout" key, under Hosts.
	Default string
	// Hosts maps a lowercased canonical host name to its settings.
	Hosts map[string]Host
	// Profiles maps a profile name to the git config keys it carries.
	Profiles map[string]Profile
	// Rules decides which Profiles entry a remote resolves to. Order here
	// is the order the file lists them in, not the order gitcd applies
	// them in: see internal/profile.SortRules.
	Rules []Rule
	// DefaultProfile is the profile applied to a remote no Rule matches.
	// Empty when the file sets none, in which case a remote no rule
	// matches gets no profile.
	DefaultProfile string
	// UseConfigOnly adds "user.useConfigOnly = true" to every rendered
	// profile, so a repository whose identity gitcd cannot determine
	// refuses to commit rather than falling back to a machine wide
	// identity. True unless the file opts out with "useConfigOnly: false".
	UseConfigOnly bool
	// AllowUnsafeKeys are the git config keys the file opts back in, one
	// by one, out of the families a profile may not otherwise carry, in
	// the spelling the file used. Every entry names one key: a wildcard
	// is not accepted, and an entry naming a key that is not refused by
	// default is an error rather than a no-op, so a typo is visible.
	AllowUnsafeKeys []string
}

// Default returns the configuration applied when no file exists.
func Default() Config {
	return Config{
		Root:          DefaultRoot,
		Default:       DefaultLayout,
		UseConfigOnly: true,
		Hosts: map[string]Host{
			"dev.azure.com": {
				Layout:  defaultAzureLayout,
				Aliases: []string{"ssh.dev.azure.com", "vs-ssh.visualstudio.com"},
			},
		},
	}
}

// rawHost mirrors Host with every field optional, so a key the file leaves
// out is told apart from one explicitly set to its zero value.
type rawHost struct {
	Layout   *string  `yaml:"layout"`
	Aliases  []string `yaml:"aliases"`
	Protocol *string  `yaml:"protocol"`
}

type rawLayout struct {
	Default *string `yaml:"default"`
}

// rawRule mirrors Rule for decoding.
type rawRule struct {
	Host    string `yaml:"host"`
	Owner   string `yaml:"owner"`
	Repo    string `yaml:"repo"`
	Profile string `yaml:"profile"`
}

// raw mirrors Config with every field optional, decoded with KnownFields so
// an unrecognised key fails loudly instead of being silently ignored.
type raw struct {
	Root           *string                      `yaml:"root"`
	Layout         *rawLayout                   `yaml:"layout"`
	Hosts          map[string]rawHost           `yaml:"hosts"`
	Profiles       map[string]map[string]string `yaml:"profiles"`
	Rules          []rawRule                    `yaml:"rules"`
	DefaultProfile *string                      `yaml:"defaultProfile"`
	UseConfigOnly  *bool                        `yaml:"useConfigOnly"`

	AllowUnsafeKeys []string `yaml:"allowUnsafeKeys"`
}

// Load reads the first of ~/.gitcd.yaml and $XDG_CONFIG_HOME/gitcd/config.yaml
// (or ~/.config/gitcd/config.yaml when XDG_CONFIG_HOME is unset) that
// exists, applies Default for every key it leaves out, and rejects an
// unknown key. Neither file existing yields Default().
func Load() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("config: find the home directory: %w", err)
	}

	for _, path := range candidatePaths(home) {
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return Config{}, fmt.Errorf("config: stat %s: %w", path, err)
		}
		return LoadFrom(path)
	}

	return defaultForHome(home)
}

// defaultForHome returns Default() with its Root expanded against home,
// the fallback every path through Load or LoadFrom that finds no
// configuration file to parse returns.
func defaultForHome(home string) (Config, error) {
	cfg := Default()
	root, err := expandHome(cfg.Root, home)
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	cfg.Root = root
	return cfg, nil
}

// Path returns the configuration file Load reads: the first of its
// candidate paths that exists, or its primary candidate, "~/.gitcd.yaml"
// resolved against the calling user's home directory, when neither does.
// A caller that edits the file a person maintains by hand, "init" moving
// a global identity key into a profile for instance, writes here, so an
// edit always lands on the same file Load would read back.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: find the home directory: %w", err)
	}
	for _, path := range candidatePaths(home) {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return candidatePaths(home)[0], nil
}

// candidatePaths lists, in the order Load tries them, the paths a
// configuration file may live at for a user whose home directory is home.
func candidatePaths(home string) []string {
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	return []string{
		filepath.Join(home, ".gitcd.yaml"),
		filepath.Join(xdg, "gitcd", "config.yaml"),
	}
}

// LoadFrom reads and validates the configuration file at path. A missing
// file yields Default().
func LoadFrom(path string) (cfg Config, err error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return Config{}, fmt.Errorf("config: find the home directory: %w", homeErr)
		}
		return defaultForHome(home)
	}
	if err != nil {
		return Config{}, fmt.Errorf("config: open %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("config: close %s: %w", path, cerr)
		}
	}()

	var r raw
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if decErr := dec.Decode(&r); decErr != nil && !errors.Is(decErr, io.EOF) {
		if isUnknownFieldError(decErr) {
			return Config{}, fmt.Errorf("config: parse %s: %w: %w", path, ErrUnknownKey, decErr)
		}
		return Config{}, fmt.Errorf("config: parse %s: %w", path, decErr)
	}

	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		return Config{}, fmt.Errorf("config: find the home directory: %w", homeErr)
	}

	cfg = Default()
	if r.Root != nil {
		cfg.Root = *r.Root
	}
	cfg.Root, err = expandHome(cfg.Root, home)
	if err != nil {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}

	if r.Layout != nil && r.Layout.Default != nil {
		cfg.Default = *r.Layout.Default
	}

	for name, rh := range r.Hosts {
		host := strings.ToLower(name)
		// h starts from the default entry for this host, if Default
		// shipped one: a key the file's own entry leaves out must keep
		// that default's value, not the generic fallback, or setting one
		// field silently drops every other field the default carried.
		h := cfg.Hosts[host]
		switch {
		case rh.Layout != nil:
			// Explicitly set, even to "": a cleared layout falls back to
			// the generic default through LayoutFor, same as an entry
			// with no layout at all.
			h.Layout = *rh.Layout
		case h.Layout == "":
			h.Layout = cfg.Default
		}
		if rh.Aliases != nil {
			// A present "aliases" key, even an empty list, replaces the
			// default's aliases rather than merging into them: a nil
			// slice (the key left out) is told apart from a non-nil, zero
			// length one (the key set to "[]") by yaml.v3 itself, so
			// "aliases: []" is how a user deliberately clears them.
			aliases := make([]string, 0, len(rh.Aliases))
			for _, alias := range rh.Aliases {
				aliases = append(aliases, strings.ToLower(alias))
			}
			h.Aliases = aliases
		}
		if rh.Protocol != nil {
			h.Protocol = *rh.Protocol
		}
		cfg.Hosts[host] = h
	}

	if r.UseConfigOnly != nil {
		cfg.UseConfigOnly = *r.UseConfigOnly
	}

	cfg.AllowUnsafeKeys = append([]string(nil), r.AllowUnsafeKeys...)

	if len(r.Profiles) > 0 {
		cfg.Profiles = make(map[string]Profile, len(r.Profiles))
		for name, keys := range r.Profiles {
			profile := make(Profile, len(keys))
			for key, value := range keys {
				profile[key] = value
			}
			cfg.Profiles[name] = profile
		}
	}

	for _, rr := range r.Rules {
		cfg.Rules = append(cfg.Rules, Rule{
			Host: strings.ToLower(rr.Host), Owner: rr.Owner, Repo: rr.Repo, Profile: rr.Profile,
		})
	}

	if r.DefaultProfile != nil {
		cfg.DefaultProfile = *r.DefaultProfile
	}

	// One gate over the assembled configuration, rather than a check per
	// field as it is decoded: internal/gitconfig.Render calls the same
	// method on whatever Config it is handed, so a configuration from a
	// file and one a caller built by hand are held to identical terms,
	// and neither can gain a field the other validates.
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}

	return cfg, nil
}

// isUnknownFieldError reports whether err is the error gopkg.in/yaml.v3
// returns from a KnownFields decoder for a field absent from the target
// type, the shape Load wraps as ErrUnknownKey.
func isUnknownFieldError(err error) bool {
	return strings.Contains(err.Error(), "not found in type")
}

// expandHome replaces a leading "~" in path with home, the way a shell
// would, so every later consumer of Config.Root can treat it as an
// ordinary absolute path.
func expandHome(path, home string) (string, error) {
	if path == "~" {
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:]), nil
	}
	if strings.HasPrefix(path, "~") {
		return "", fmt.Errorf("config: %q names another user's home directory, which gitcd cannot expand", path)
	}
	return path, nil
}

// AliasMap builds the map from every configured alias, lowercased, to the
// canonical host it resolves to, for remote.Parse.
func (c Config) AliasMap() map[string]string {
	aliases := map[string]string{}
	for host, h := range c.Hosts {
		for _, alias := range h.Aliases {
			aliases[alias] = host
		}
	}
	return aliases
}

// Keys returns p's git config keys, sorted, so a caller that writes or
// renders them gets a deterministic order.
func (p Profile) Keys() []string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// LayoutFor returns the path template host, already lowercased, should
// render with: its own "layout" key, or Default when host carries none or
// is not configured at all.
func (c Config) LayoutFor(host string) string {
	if h, ok := c.Hosts[strings.ToLower(host)]; ok && h.Layout != "" {
		return h.Layout
	}
	return c.Default
}
