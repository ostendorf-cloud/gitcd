// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package doctor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/doctor"
	"github.com/ostendorf-cloud/gitcd/internal/gitconfig"
)

func TestCheckManagedFiles_SymlinkedRoot(t *testing.T) {
	home := setupGitEnv(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temporary directory: %v", err)
	}
	realRoot := filepath.Join(base, "volume", "Code")
	linkRoot := filepath.Join(base, "Code")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatalf("create %s: %v", realRoot, err)
	}
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("cannot create a symbolic link here, the account may lack the privilege: %v", err)
		}
		t.Fatalf("symlink %s -> %s: %v", linkRoot, realRoot, err)
	}

	cfg := config.Config{
		Root:     linkRoot,
		Default:  "{host}/{owner}/{repo}",
		Profiles: map[string]config.Profile{"work": {"user.email": "acme-work-identity"}},
		Rules:    []config.Rule{{Host: "github.com", Owner: "acme", Profile: "work"}},
	}
	write := func(t *testing.T, files map[string]string) {
		t.Helper()
		for path, content := range files {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("create %s: %v", filepath.Dir(path), err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}
		}
	}

	t.Run("a file rendered for the link only is stale and names the effect", func(t *testing.T) {
		// What apply wrote before it resolved the root: the link's own
		// condition and nothing else.
		cfgPlain := cfg
		cfgPlain.Root = filepath.Join(base, "plain")
		old, err := gitconfig.Render(cfgPlain, home)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		oldInclude := strings.ReplaceAll(old[gitconfig.IncludePath(home)], filepath.ToSlash(cfgPlain.Root), filepath.ToSlash(linkRoot))
		old[gitconfig.IncludePath(home)] = oldInclude
		write(t, old)

		findings, err := doctor.CheckManagedFiles(cfg, home)
		if err != nil {
			t.Fatalf("CheckManagedFiles() returned an unexpected error: %v", err)
		}
		if _, ok := findKind(findings, doctor.KindManagedFileStale); !ok {
			t.Fatalf("CheckManagedFiles() = %+v, want a %s finding", findings, doctor.KindManagedFileStale)
		}
		f, ok := findKind(findings, doctor.KindRootSymlink)
		if !ok {
			t.Fatalf("CheckManagedFiles() = %+v, want a %s finding", findings, doctor.KindRootSymlink)
		}
		if !f.Fixable || !strings.Contains(f.Message, realRoot) || !strings.Contains(f.Message, linkRoot) {
			t.Fatalf("finding = %+v, want it fixable and naming both %s and %s", f, linkRoot, realRoot)
		}
	})

	t.Run("a file rendered with both conditions is clean", func(t *testing.T) {
		rendered, err := gitconfig.Render(cfg, home)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		for _, root := range []string{linkRoot, realRoot} {
			if !strings.Contains(rendered[gitconfig.IncludePath(home)], "gitdir/i:"+filepath.ToSlash(root)+"/") {
				t.Fatalf("include file lacks a condition for %s:\n%s", root, rendered[gitconfig.IncludePath(home)])
			}
		}
		write(t, rendered)

		findings, err := doctor.CheckManagedFiles(cfg, home)
		if err != nil {
			t.Fatalf("CheckManagedFiles() returned an unexpected error: %v", err)
		}
		if len(findings) != 0 {
			t.Fatalf("CheckManagedFiles() = %+v, want no findings once disk matches the render", findings)
		}
	})

	t.Run("the root changes after apply: the file is stale", func(t *testing.T) {
		other := cfg
		other.Root = filepath.Join(base, "elsewhere")
		findings, err := doctor.CheckManagedFiles(other, home)
		if err != nil {
			t.Fatalf("CheckManagedFiles() returned an unexpected error: %v", err)
		}
		if _, ok := findKind(findings, doctor.KindManagedFileStale); !ok {
			t.Fatalf("CheckManagedFiles() = %+v, want a %s finding", findings, doctor.KindManagedFileStale)
		}
		if _, ok := findKind(findings, doctor.KindRootSymlink); ok {
			t.Fatalf("CheckManagedFiles() = %+v, want no %s finding for a root that is no symlink", findings, doctor.KindRootSymlink)
		}
	})
}

// TestCheckManagedFiles_UnrenderableSymlinkTarget covers a root whose
// link points at a directory the root check refuses. The include file is
// rendered for the configured root only and matches what is on disk, so the
// file is not stale, but the rules do not apply through the real path, and
// doctor says so, not fixable since no re-render changes it.
func TestCheckManagedFiles_UnrenderableSymlinkTarget(t *testing.T) {
	for _, target := range []string{`a"b`, "[l]", "a*b", "a\nb"} {
		t.Run(target, func(t *testing.T) {
			home := setupGitEnv(t)
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatalf("resolve the temporary directory: %v", err)
			}
			realRoot := filepath.Join(base, target)
			if err := os.MkdirAll(realRoot, 0o755); err != nil {
				t.Skipf("the file system refuses the directory name %q: %v", target, err)
			}
			linkRoot := filepath.Join(base, "Code")
			if err := os.Symlink(realRoot, linkRoot); err != nil {
				if runtime.GOOS == "windows" {
					t.Skipf("cannot create a symbolic link here, the account may lack the privilege: %v", err)
				}
				t.Fatalf("symlink %s -> %s: %v", linkRoot, realRoot, err)
			}
			cfg := config.Config{
				Root:     linkRoot,
				Default:  "{host}/{owner}/{repo}",
				Profiles: map[string]config.Profile{"work": {"user.email": "acme-work-identity"}},
				Rules:    []config.Rule{{Host: "github.com", Owner: "acme", Profile: "work"}},
			}
			rendered, err := gitconfig.Render(cfg, home)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if got := strings.Count(rendered[gitconfig.IncludePath(home)], "gitdir/i:"); got != 1 {
				t.Fatalf("include file holds %d gitdir conditions, want only the configured root's", got)
			}
			for path, content := range rendered {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("create %s: %v", filepath.Dir(path), err)
				}
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatalf("write %s: %v", path, err)
				}
			}

			findings, err := doctor.CheckManagedFiles(cfg, home)
			if err != nil {
				t.Fatalf("CheckManagedFiles() returned an unexpected error: %v", err)
			}
			if _, ok := findKind(findings, doctor.KindManagedFileStale); ok {
				t.Fatalf("CheckManagedFiles() = %+v, want no %s finding: the file matches the render", findings, doctor.KindManagedFileStale)
			}
			f, ok := findKind(findings, doctor.KindRootSymlink)
			if !ok {
				t.Fatalf("CheckManagedFiles() = %+v, want a %s finding", findings, doctor.KindRootSymlink)
			}
			if f.Fixable || !strings.Contains(f.Message, fmt.Sprintf("%q", realRoot)) || !strings.Contains(f.Message, fmt.Sprintf("%q", linkRoot)) ||
				strings.Contains(f.Message, "\n") {
				t.Fatalf("finding = %+v, want it not fixable and naming both %s and %s", f, linkRoot, realRoot)
			}
		})
	}
}
