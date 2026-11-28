// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/config"
	"github.com/ostendorf-cloud/gitcd/internal/git"
)

// worktreeFixture is a managed repository with two linked worktrees in the
// organisation's own ".claude/worktrees/<branch>" layout.
type worktreeFixture struct {
	home  string
	main  string
	menu  string
	crash string
}

// newWorktreeFixture creates github.com/acme/widget under the default root
// with a remote, and the linked worktrees feat/20-menu and fix/31-crash.
func newWorktreeFixture(t *testing.T) worktreeFixture {
	t.Helper()
	home := setupEnv(t)
	main := filepath.Join(home, "Code", "github.com", "acme", "widget")
	initRepository(t, main)
	addRemote(t, main, "https://github.com/acme/widget.git")

	menu := filepath.Join(main, ".claude", "worktrees", "feat", "20-menu")
	crash := filepath.Join(main, ".claude", "worktrees", "fix", "31-crash")
	runGit(t, main, "worktree", "add", "-b", "feat/20-menu", menu)
	runGit(t, main, "worktree", "add", "-b", "fix/31-crash", crash)
	return worktreeFixture{home: home, main: main, menu: menu, crash: crash}
}

// resolved returns path with every symlink resolved, the form git reports a
// worktree in: a temporary directory on macOS sits behind one.
func resolved(t *testing.T, path string) string {
	t.Helper()
	got, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve %s: %v", path, err)
	}
	return got
}

func TestRunWorktree_OneMatchPrintsThePathWithoutAPicker(t *testing.T) {
	f := newWorktreeFixture(t)
	t.Chdir(f.main)

	var stdout, stderr bytes.Buffer
	// stdin carries a valid choice and claims to be a terminal: a single
	// match must never read it.
	code := runWorktree([]string{"menu"}, strings.NewReader("2\n"), true, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("worktree menu = %d, want 0, stderr: %s", code, stderr.String())
	}
	if got, want := strings.TrimSpace(stdout.String()), resolved(t, f.menu); got != want {
		t.Fatalf("worktree menu stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("worktree menu wrote to stderr: %q, want no picker for one match", stderr.String())
	}
}

func TestRunWorktree_FromInsideALinkedWorktreeStillListsEveryWorktree(t *testing.T) {
	f := newWorktreeFixture(t)
	t.Chdir(f.menu)

	var stdout, stderr bytes.Buffer
	// Order: the main checkout, then the linked worktrees by branch, so "3"
	// is fix/31-crash.
	code := runWorktree(nil, strings.NewReader("3\n"), true, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("worktree = %d, want 0, stderr: %s", code, stderr.String())
	}
	if got, want := strings.TrimSpace(stdout.String()), resolved(t, f.crash); got != want {
		t.Fatalf("worktree stdout = %q, want %q", got, want)
	}
	for _, want := range []string{"main (main)", "feat/20-menu", "fix/31-crash", "1) ", "2) ", "3) "} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("worktree stderr = %q, want it to list %q", stderr.String(), want)
		}
	}
}

func TestRunWorktree_PickerShowsTheDirtyStateOfEachWorktree(t *testing.T) {
	f := newWorktreeFixture(t)
	writeFile(t, filepath.Join(f.crash, "scratch.txt"), "work in progress\n")
	t.Chdir(f.main)

	var stdout, stderr bytes.Buffer
	code := runWorktree(nil, strings.NewReader("2\n"), true, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("worktree = %d, want 0, stderr: %s", code, stderr.String())
	}
	var menuLine, crashLine string
	for _, line := range strings.Split(stderr.String(), "\n") {
		switch {
		case strings.Contains(line, "feat/20-menu"):
			menuLine = line
		case strings.Contains(line, "fix/31-crash"):
			crashLine = line
		}
	}
	if !strings.Contains(menuLine, "clean") {
		t.Fatalf("the feat/20-menu line = %q, want it marked clean", menuLine)
	}
	if !strings.Contains(crashLine, "dirty") {
		t.Fatalf("the fix/31-crash line = %q, want it marked dirty", crashLine)
	}
}

func TestRunWorktree_AmbiguousWithoutATerminalListsEveryCandidateAndReadsNothing(t *testing.T) {
	f := newWorktreeFixture(t)
	t.Chdir(f.main)

	var stdout, stderr bytes.Buffer
	code := runWorktree(nil, strings.NewReader("2\n"), false, &stdout, &stderr)

	if code == 0 {
		t.Fatal("worktree = 0, want a failure without a terminal to pick on")
	}
	if stdout.Len() != 0 {
		t.Fatalf("worktree wrote to stdout: %q, want nothing", stdout.String())
	}
	if !strings.Contains(stderr.String(), "feat/20-menu") || !strings.Contains(stderr.String(), "fix/31-crash") {
		t.Fatalf("worktree stderr = %q, want both worktrees listed", stderr.String())
	}
}

func TestRunWorktree_NoMatchFailsWithoutOutput(t *testing.T) {
	f := newWorktreeFixture(t)
	t.Chdir(f.main)

	var stdout, stderr bytes.Buffer
	code := runWorktree([]string{"nowhere"}, strings.NewReader(""), false, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("worktree nowhere = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("worktree nowhere wrote to stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "nowhere") {
		t.Fatalf("worktree nowhere stderr = %q, want it to name the filter", stderr.String())
	}
}

func TestRunWorktree_RepoFlagNamesAnotherRepository(t *testing.T) {
	f := newWorktreeFixture(t)
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	code := runWorktree([]string{"--repo", "widget", "crash"}, strings.NewReader(""), false, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("worktree --repo widget crash = %d, want 0, stderr: %s", code, stderr.String())
	}
	if got, want := strings.TrimSpace(stdout.String()), resolved(t, f.crash); got != want {
		t.Fatalf("worktree --repo widget crash stdout = %q, want %q", got, want)
	}
}

func TestRunWorktree_RepoFlagIgnoresTheWorktreesAsRepositories(t *testing.T) {
	f := newWorktreeFixture(t)
	t.Chdir(t.TempDir())

	// "widget" is in the main checkout's path and in both worktrees' paths:
	// were a worktree a candidate, this query would be ambiguous.
	var stdout, stderr bytes.Buffer
	code := runWorktree([]string{"--repo", "widget", "menu"}, strings.NewReader(""), false, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("worktree --repo widget menu = %d, want 0, stderr: %s", code, stderr.String())
	}
	if got, want := strings.TrimSpace(stdout.String()), resolved(t, f.menu); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRunWorktree_OutsideARepositoryWithoutRepoFails(t *testing.T) {
	setupEnv(t)
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	code := runWorktree(nil, strings.NewReader(""), false, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("worktree outside a repository = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("worktree outside a repository wrote to stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--repo") {
		t.Fatalf("stderr = %q, want it to point at --repo", stderr.String())
	}
}

func TestRunWorktree_UnknownRepositoryFails(t *testing.T) {
	newWorktreeFixture(t)
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	code := runWorktree([]string{"--repo", "nowhere"}, strings.NewReader(""), false, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("worktree --repo nowhere = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing", stdout.String())
	}
}

func TestRunWorktree_TooManyArgumentsIsAUsageError(t *testing.T) {
	setupEnv(t)

	var stdout, stderr bytes.Buffer
	if code := runWorktree([]string{"a", "b"}, strings.NewReader(""), false, &stdout, &stderr); code != 2 {
		t.Fatalf("worktree a b = %d, want 2", code)
	}
}

func TestRunWorktree_PrunableWorktreeIsNotOffered(t *testing.T) {
	f := newWorktreeFixture(t)
	if err := os.RemoveAll(f.crash); err != nil {
		t.Fatalf("remove %s: %v", f.crash, err)
	}
	t.Chdir(f.main)

	// With fix/31-crash gone, feat/20-menu is the only linked worktree
	// left, so a filter that matches both main and it by name still lands
	// on one, and an empty filter lists two, never three.
	var stdout, stderr bytes.Buffer
	code := runWorktree(nil, strings.NewReader("2\n"), true, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("worktree = %d, want 0, stderr: %s", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "fix/31-crash") {
		t.Fatalf("worktree stderr = %q, want the pruned worktree left out", stderr.String())
	}
	if got, want := strings.TrimSpace(stdout.String()), resolved(t, f.menu); got != want {
		t.Fatalf("worktree stdout = %q, want %q", got, want)
	}
}

func TestRunWorktree_RepositoryWithoutLinkedWorktreesFails(t *testing.T) {
	home := setupEnv(t)
	main := filepath.Join(home, "Code", "github.com", "acme", "solo")
	initRepository(t, main)
	addRemote(t, main, "https://github.com/acme/solo.git")
	t.Chdir(main)

	var stdout, stderr bytes.Buffer
	code := runWorktree(nil, strings.NewReader(""), true, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("worktree in a repository with no linked worktree = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing", stdout.String())
	}

	repo := worktreeRepo{main: main, worktrees: []git.Worktree{{Path: main, Branch: "main"}}}
	_, err := chooseWorktree(context.Background(), repo, "", strings.NewReader(""), true, &bytes.Buffer{}, newLogger(false, &bytes.Buffer{}))
	if !errors.Is(err, errNoLinkedWorktree) {
		t.Fatalf("chooseWorktree error = %v, want errNoLinkedWorktree", err)
	}
}

func TestRun_WtIsAnAliasOfWorktree(t *testing.T) {
	f := newWorktreeFixture(t)
	t.Chdir(f.main)

	var stdout, stderr bytes.Buffer
	code := run([]string{"wt", "menu"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("wt menu = %d, want 0, stderr: %s", code, stderr.String())
	}
	if got, want := strings.TrimSpace(stdout.String()), resolved(t, f.menu); got != want {
		t.Fatalf("wt menu stdout = %q, want %q", got, want)
	}
}

func TestSelectWorktree_NoMatchIsTheTypedError(t *testing.T) {
	pool := []git.Worktree{{Path: "/r/widget", Branch: "main"}, {Path: "/r/widget/wt/x", Branch: "x"}}
	label := func([]git.Worktree) []string { return nil }

	_, err := selectWorktree(pool, "nowhere", "/r/widget", label, strings.NewReader(""), false, &bytes.Buffer{})

	if !errors.Is(err, errNoWorktreeMatch) {
		t.Fatalf("selectWorktree error = %v, want errNoWorktreeMatch", err)
	}
}

func TestMatchWorktrees(t *testing.T) {
	t.Parallel()

	main := git.Worktree{Path: "/r/widget", Branch: "main"}
	menu := git.Worktree{Path: "/r/widget/.claude/worktrees/feat/20-menu", Branch: "feat/20-menu"}
	menu2 := git.Worktree{Path: "/r/widget/.claude/worktrees/fix/21-menu-bar", Branch: "fix/21-menu-bar"}
	probe := git.Worktree{Path: "/r/widget/.claude/worktrees/probe", Detached: true}
	pool := []git.Worktree{main, menu, menu2, probe}

	tests := []struct {
		name   string
		filter string
		want   []git.Worktree
	}{
		{name: "an empty filter selects every worktree", filter: "", want: pool},
		{name: "a branch substring", filter: "20-me", want: []git.Worktree{menu}},
		{name: "a substring several branches carry", filter: "menu", want: []git.Worktree{menu, menu2}},
		{name: "an exact branch beats a longer substring match", filter: "feat/20-menu", want: []git.Worktree{menu}},
		{name: "an exact directory name of a detached worktree", filter: "probe", want: []git.Worktree{probe}},
		{name: "case is ignored", filter: "FEAT/20", want: []git.Worktree{menu}},
		{name: "the exact main branch wins over fuzzy hits", filter: "main", want: []git.Worktree{main}},
		{name: "a full path", filter: "/r/widget/.claude/worktrees/probe", want: []git.Worktree{probe}},
		{name: "no match", filter: "zzz", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := matchWorktrees(pool, tt.filter)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("matchWorktrees(%q) = %+v, want %+v", tt.filter, got, tt.want)
			}
		})
	}
}

// TestEnterableWorktrees_ParsesTheFixtureOutput feeds the picker input from
// "git worktree list --porcelain" output: a main checkout, two linked
// worktrees, and a prunable one.
func TestEnterableWorktrees_ParsesTheFixtureOutput(t *testing.T) {
	t.Parallel()

	out := "worktree /r/widget\nHEAD 1111111111111111111111111111111111111111\nbranch refs/heads/main\n\n" +
		"worktree /r/widget/.claude/worktrees/fix/31-crash\nHEAD 2222222222222222222222222222222222222222\nbranch refs/heads/fix/31-crash\n\n" +
		"worktree /r/widget/.claude/worktrees/feat/20-menu\nHEAD 3333333333333333333333333333333333333333\nbranch refs/heads/feat/20-menu\n\n" +
		"worktree /r/widget/.claude/worktrees/gone\nHEAD 4444444444444444444444444444444444444444\nbranch refs/heads/chore/gone\nprunable gitdir file points to non-existent location\n"

	got, linked := enterableWorktrees(git.ParseWorktrees(out))

	var branches []string
	for _, w := range got {
		branches = append(branches, w.Branch)
	}
	if want := []string{"main", "feat/20-menu", "fix/31-crash"}; !reflect.DeepEqual(branches, want) {
		t.Fatalf("enterableWorktrees branches = %v, want %v: main first, linked ones by branch, the prunable one left out", branches, want)
	}
	if linked != 2 {
		t.Fatalf("enterableWorktrees linked = %d, want 2", linked)
	}
}

func TestEnterableWorktrees_BareRepositoryEntryIsNotEnterable(t *testing.T) {
	t.Parallel()

	list := []git.Worktree{{Path: "/srv/widget.git", Bare: true}, {Path: "/srv/wt/main", Branch: "main"}}
	got, linked := enterableWorktrees(list)

	if linked != 1 || len(got) != 1 || got[0].Path != "/srv/wt/main" {
		t.Fatalf("enterableWorktrees = %+v, %d, want only the worktree of the bare repository", got, linked)
	}
}

func worktreeConfig(t *testing.T, home, yaml string) {
	t.Helper()
	writeFile(t, filepath.Join(home, ".gitcd.yaml"), yaml)
}

func TestRunWorktreeAdd_CreatesANestedWorktreeAndPrintsOnlyThePath(t *testing.T) {
	f := newWorktreeFixture(t)
	worktreeConfig(t, f.home, "worktreeDir: .claude/worktrees\n")
	t.Chdir(f.main)

	var stdout, stderr bytes.Buffer
	code := run([]string{"worktree", "add", "feat/40-search"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("worktree add = %d, want 0, stderr: %s", code, stderr.String())
	}
	want := filepath.Join(resolved(t, f.main), ".claude", "worktrees", "feat", "40-search")
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Fatalf("worktree add stdout = %q, want only the path %q: git's own output belongs on stderr", got, want)
	}
	if _, err := os.Stat(filepath.Join(want, "README.md")); err != nil {
		t.Fatalf("the worktree holds no checkout: %v", err)
	}
	if got := runGitOutput(t, want, "rev-parse", "--abbrev-ref", "HEAD"); got != "feat/40-search" {
		t.Fatalf("the new worktree is on %q, want feat/40-search", got)
	}
}

func TestRunWorktreeAdd_AbsoluteDirectoryWithRepoPlaceholder(t *testing.T) {
	f := newWorktreeFixture(t)
	dir := filepath.Join(t.TempDir(), "worktrees")
	worktreeConfig(t, f.home, "worktreeDir: '"+filepath.ToSlash(dir)+"/{repo}'\n")
	t.Chdir(f.main)

	var stdout, stderr bytes.Buffer
	code := run([]string{"worktree", "add", "feat/40-search"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("worktree add = %d, want 0, stderr: %s", code, stderr.String())
	}
	want := filepath.Join(dir, "widget", "feat", "40-search")
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Fatalf("worktree add stdout = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(want, "README.md")); err != nil {
		t.Fatalf("the worktree holds no checkout: %v", err)
	}
}

func TestRunWorktreeAdd_ThenWorktreeFindsIt(t *testing.T) {
	f := newWorktreeFixture(t)
	worktreeConfig(t, f.home, "worktreeDir: .worktrees\n")
	t.Chdir(f.main)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"worktree", "add", "feat/40-search"}, &stdout, &stderr); code != 0 {
		t.Fatalf("worktree add = %d, want 0, stderr: %s", code, stderr.String())
	}
	added := strings.TrimSpace(stdout.String())

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"worktree", "search"}, &stdout, &stderr); code != 0 {
		t.Fatalf("worktree search = %d, want 0, stderr: %s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != added {
		t.Fatalf("worktree search stdout = %q, want the added worktree %q", got, added)
	}
}

func TestRunWorktreeAdd_ChecksOutAnExistingBranch(t *testing.T) {
	f := newWorktreeFixture(t)
	worktreeConfig(t, f.home, "worktreeDir: .claude/worktrees\n")
	runGit(t, f.main, "branch", "topic")
	t.Chdir(f.main)

	var stdout, stderr bytes.Buffer
	code := run([]string{"worktree", "add", "topic"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("worktree add topic = %d, want 0, stderr: %s", code, stderr.String())
	}
	path := strings.TrimSpace(stdout.String())
	if got := runGitOutput(t, path, "rev-parse", "--abbrev-ref", "HEAD"); got != "topic" {
		t.Fatalf("the worktree is on %q, want the existing branch topic", got)
	}
}

func TestRunWorktreeAdd_NewBranchStartsAtBase(t *testing.T) {
	f := newWorktreeFixture(t)
	worktreeConfig(t, f.home, "worktreeDir: .claude/worktrees\n")
	runGit(t, f.main, "branch", "release")
	t.Chdir(f.crash)
	writeFile(t, filepath.Join(f.crash, "extra.txt"), "x\n")
	runGit(t, f.crash, "add", "extra.txt")
	runGit(t, f.crash, "commit", "-m", "extra")

	var stdout, stderr bytes.Buffer
	code := run([]string{"worktree", "add", "--base", "release", "topic"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("worktree add --base release topic = %d, want 0, stderr: %s", code, stderr.String())
	}
	path := strings.TrimSpace(stdout.String())
	if _, err := os.Stat(filepath.Join(path, "extra.txt")); err == nil {
		t.Fatal("the new branch carries a commit of the current worktree, want it started at --base release")
	}
	// Run from inside a linked worktree, the worktree still lands under the
	// main checkout's directory.
	if want := filepath.Join(resolved(t, f.main), ".claude", "worktrees", "topic"); path != want {
		t.Fatalf("worktree add stdout = %q, want %q", path, want)
	}
}

func TestRunWorktreeAdd_BaseWithAnExistingBranchIsRefused(t *testing.T) {
	f := newWorktreeFixture(t)
	worktreeConfig(t, f.home, "worktreeDir: .claude/worktrees\n")
	runGit(t, f.main, "branch", "topic")
	t.Chdir(f.main)

	var stdout, stderr bytes.Buffer
	code := run([]string{"worktree", "add", "--base", "main", "topic"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("worktree add --base main topic = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(f.main, ".claude", "worktrees", "topic")); err == nil {
		t.Fatal("the refused add still created the worktree")
	}
}

func TestRunWorktreeAdd_DryRunPrintsThePathAndCreatesNothing(t *testing.T) {
	f := newWorktreeFixture(t)
	worktreeConfig(t, f.home, "worktreeDir: .claude/worktrees\n")
	t.Chdir(f.main)

	var stdout, stderr bytes.Buffer
	code := run([]string{"worktree", "add", "--dry-run", "feat/40-search"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("worktree add --dry-run = %d, want 0, stderr: %s", code, stderr.String())
	}
	want := filepath.Join(resolved(t, f.main), ".claude", "worktrees", "feat", "40-search")
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if _, err := os.Stat(want); err == nil {
		t.Fatalf("--dry-run created %s", want)
	}
	if git.BranchExists(context.Background(), f.main, "feat/40-search") {
		t.Fatal("--dry-run created the branch")
	}
}

func TestRunWorktreeAdd_WithoutWorktreeDirRefuses(t *testing.T) {
	f := newWorktreeFixture(t)
	t.Chdir(f.main)

	var stdout, stderr bytes.Buffer
	code := run([]string{"worktree", "add", "feat/40-search"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("worktree add without worktreeDir = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing", stdout.String())
	}
	if !strings.Contains(stderr.String(), "worktreeDir") {
		t.Fatalf("stderr = %q, want it to name worktreeDir", stderr.String())
	}
	if git.BranchExists(context.Background(), f.main, "feat/40-search") {
		t.Fatal("the refused add created the branch")
	}
}

func TestRunWorktreeAdd_RequiresExactlyOneBranch(t *testing.T) {
	setupEnv(t)

	for _, args := range [][]string{{"worktree", "add"}, {"worktree", "add", "a", "b"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Fatalf("%v = %d, want 2", args, code)
		}
	}
}

func TestRunWorktreeAdd_ExistingPathIsRefused(t *testing.T) {
	f := newWorktreeFixture(t)
	worktreeConfig(t, f.home, "worktreeDir: .claude/worktrees\n")
	t.Chdir(f.main)

	// feat/20-menu already has its worktree at that very path.
	var stdout, stderr bytes.Buffer
	code := run([]string{"worktree", "add", "feat/20-menu"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("worktree add of an existing path = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing", stdout.String())
	}
}

func TestPlanWorktreeAdd(t *testing.T) {
	setupEnv(t)
	main := filepath.Join(t.TempDir(), "widget")
	initRepository(t, main)
	if err := os.MkdirAll(filepath.Join(main, ".claude", "worktrees", "taken"), 0o755); err != nil {
		t.Fatalf("create the existing path: %v", err)
	}

	tests := []struct {
		name    string
		dir     string
		branch  string
		want    string
		wantErr error
	}{
		{name: "a nested branch", dir: ".claude/worktrees", branch: "feat/20-menu", want: filepath.Join(main, ".claude", "worktrees", "feat", "20-menu")},
		{name: "a plain branch", dir: ".worktrees", branch: "topic", want: filepath.Join(main, ".worktrees", "topic")},
		{name: "an existing path", dir: ".claude/worktrees", branch: "taken", wantErr: errWorktreeExists},
		{name: "a parent segment in the branch", dir: ".claude/worktrees", branch: "../escape", wantErr: config.ErrInvalidWorktreeBranch},
		{name: "a parent segment hidden in the middle", dir: ".claude/worktrees", branch: "a/../../escape", wantErr: config.ErrInvalidWorktreeBranch},
		{name: "an absolute branch", dir: ".claude/worktrees", branch: "/tmp/escape", wantErr: config.ErrInvalidWorktreeBranch},
		{name: "a parent segment in the directory", dir: "../outside", branch: "topic", wantErr: config.ErrInvalidWorktreeDir},
		{name: "a name git refuses", dir: ".claude/worktrees", branch: "my branch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.WorktreeDir = tt.dir
			got, err := planWorktreeAdd(context.Background(), cfg, main, tt.branch)
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("planWorktreeAdd(%q) = %q, %v, want %v", tt.branch, got, err, tt.wantErr)
				}
			case tt.name == "a name git refuses":
				if err == nil {
					t.Fatalf("planWorktreeAdd(%q) = %q, want git's own refusal", tt.branch, got)
				}
			default:
				if err != nil {
					t.Fatalf("planWorktreeAdd(%q) returned an unexpected error: %v", tt.branch, err)
				}
				if got != tt.want {
					t.Fatalf("planWorktreeAdd(%q) = %q, want %q", tt.branch, got, tt.want)
				}
			}
		})
	}
}

func TestRunWorktreeAdd_EscapingBranchCreatesNothing(t *testing.T) {
	f := newWorktreeFixture(t)
	worktreeConfig(t, f.home, "worktreeDir: .claude/worktrees\n")
	t.Chdir(f.main)

	var stdout, stderr bytes.Buffer
	code := run([]string{"worktree", "add", "../../../escape"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("worktree add ../../../escape = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(f.main, "escape")); err == nil {
		t.Fatal("an escaping branch created a directory")
	}
}

func TestLoad_WorktreeDirWithAParentSegmentFailsEveryCommand(t *testing.T) {
	f := newWorktreeFixture(t)
	worktreeConfig(t, f.home, "worktreeDir: ../elsewhere\n")
	t.Chdir(f.main)

	var stdout, stderr bytes.Buffer
	code := run([]string{"worktree", "add", "topic"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("worktree add with an escaping worktreeDir = %d, want 1", code)
	}
	if _, err := os.Stat(filepath.Join(f.main, "..", "elsewhere")); err == nil {
		t.Fatal("an escaping worktreeDir created a directory")
	}
}
