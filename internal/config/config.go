// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

// Package config loads gitcd's own configuration: the clone root, the path
// layout per host and the alias map a host's SSH host names resolve
// through. It never touches git configuration, which belongs to a later
// milestone.
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultRoot is the clone root applied when "root" is left out of the
// configuration file, or no file exists at all.
const DefaultRoot = "~/Code"

// DefaultLayout is the path template applied to a host without its own
// "layout" key, and to every host when "layout.default" is left out.
const DefaultLayout = "{host}/{owner}/{repo}"

// ErrUnknownKey is wrapped into the error Load returns when the
// configuration file carries a key this package does not recognise.
var ErrUnknownKey = errors.New("config: unknown key")

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
}

// Default returns the configuration applied when no file exists.
func Default() Config {
	return Config{
		Root:    DefaultRoot,
		Default: DefaultLayout,
		Hosts:   map[string]Host{},
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

// raw mirrors Config with every field optional, decoded with KnownFields so
// an unrecognised key fails loudly instead of being silently ignored.
type raw struct {
	Root   *string            `yaml:"root"`
	Layout *rawLayout         `yaml:"layout"`
	Hosts  map[string]rawHost `yaml:"hosts"`
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
		h := Host{Layout: cfg.Default}
		if rh.Layout != nil {
			h.Layout = *rh.Layout
		}
		for _, alias := range rh.Aliases {
			h.Aliases = append(h.Aliases, strings.ToLower(alias))
		}
		if rh.Protocol != nil {
			h.Protocol = *rh.Protocol
		}
		cfg.Hosts[host] = h
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

// LayoutFor returns the path template host, already lowercased, should
// render with: its own "layout" key, or Default when host carries none or
// is not configured at all.
func (c Config) LayoutFor(host string) string {
	if h, ok := c.Hosts[strings.ToLower(host)]; ok && h.Layout != "" {
		return h.Layout
	}
	return c.Default
}
