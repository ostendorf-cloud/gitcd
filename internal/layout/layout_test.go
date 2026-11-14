// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package layout_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/layout"
	"github.com/ostendorf-cloud/gitcd/internal/remote"
)

// rightToLeftOverride is U+202E, a Unicode formatting character that
// changes the reading direction of the text around it. Built from its
// code point, rather than written as a literal rune in source, so the
// character itself never has to appear directly in this file.
const rightToLeftOverride = rune(0x202E)

func TestRender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tmpl    string
		remote  remote.Remote
		want    string
		wantErr bool
	}{
		{
			name:   "default template renders host owner repo",
			tmpl:   "{host}/{owner}/{repo}",
			remote: remote.Remote{Host: "github.com", Owner: "acme", Repo: "widget"},
			want:   "github.com/acme/widget",
		},
		{
			name:   "azure template renders the project with the org placeholder",
			tmpl:   "{host}/{org}/{project}/{repo}",
			remote: remote.Remote{Host: "dev.azure.com", Owner: "acme-corp", Project: "Widget", Repo: "widget-repo"},
			want:   "dev.azure.com/acme-corp/Widget/widget-repo",
		},
		{
			name:   "owner with a subgroup renders as nested directories",
			tmpl:   "{host}/{owner}/{repo}",
			remote: remote.Remote{Host: "gitlab.com", Owner: "group/subgroup", Repo: "project"},
			want:   "gitlab.com/group/subgroup/project",
		},
		{
			name:    "a template needing a field the remote has none of fails",
			tmpl:    "{host}/{org}/{project}/{repo}",
			remote:  remote.Remote{Host: "github.com", Owner: "acme", Repo: "widget"},
			wantErr: true,
		},
		{
			name: "a template with no placeholder for a field the remote carries fails",
			tmpl: "{host}/{owner}/{repo}",
			remote: remote.Remote{
				Host: "dev.azure.com", Owner: "acme-corp", Project: "Widget", Repo: "widget-repo",
			},
			wantErr: true,
		},
		{
			name:    "an unknown placeholder fails",
			tmpl:    "{host}/{owner}/{branch}",
			remote:  remote.Remote{Host: "github.com", Owner: "acme", Repo: "widget"},
			wantErr: true,
		},
		{
			name:    "a repo naming a parent directory fails",
			tmpl:    "{host}/{owner}/{repo}",
			remote:  remote.Remote{Host: "github.com", Owner: "acme", Repo: ".."},
			wantErr: true,
		},
		{
			name:    "an owner naming a parent directory fails",
			tmpl:    "{host}/{owner}/{repo}",
			remote:  remote.Remote{Host: "github.com", Owner: "..", Repo: "widget"},
			wantErr: true,
		},
		{
			name:    "one segment of a subgroup owner naming a parent directory fails",
			tmpl:    "{host}/{owner}/{repo}",
			remote:  remote.Remote{Host: "gitlab.com", Owner: "group/..", Repo: "widget"},
			wantErr: true,
		},
		{
			name:    "a host naming a parent directory fails",
			tmpl:    "{host}/{owner}/{repo}",
			remote:  remote.Remote{Host: "..", Owner: "acme", Repo: "widget"},
			wantErr: true,
		},
		{
			name:    "a project naming a parent directory fails",
			tmpl:    "{host}/{org}/{project}/{repo}",
			remote:  remote.Remote{Host: "dev.azure.com", Owner: "acme-corp", Project: "..", Repo: "widget-repo"},
			wantErr: true,
		},
		{
			name:    "an absolute repo fails",
			tmpl:    "{host}/{owner}/{repo}",
			remote:  remote.Remote{Host: "github.com", Owner: "acme", Repo: "/etc"},
			wantErr: true,
		},
		{
			name:    "a repo naming the current directory fails",
			tmpl:    "{host}/{owner}/{repo}",
			remote:  remote.Remote{Host: "github.com", Owner: "acme", Repo: "."},
			wantErr: true,
		},
		{
			name:    "a doubled slash leaves an empty component inside a subgroup owner, which fails",
			tmpl:    "{host}/{owner}/{repo}",
			remote:  remote.Remote{Host: "gitlab.com", Owner: "group//subgroup", Repo: "widget"},
			wantErr: true,
		},
		{
			name:    "an owner carrying a backslash fails, on every host operating system",
			tmpl:    "{host}/{owner}/{repo}",
			remote:  remote.Remote{Host: "github.com", Owner: `acme\..`, Repo: "widget"},
			wantErr: true,
		},
		{
			name:    "a repo with no visible content fails",
			tmpl:    "{host}/{owner}/{repo}",
			remote:  remote.Remote{Host: "github.com", Owner: "acme", Repo: " "},
			wantErr: true,
		},
		{
			name:    "an owner carrying a colon fails, on every host operating system",
			tmpl:    "{host}/{owner}/{repo}",
			remote:  remote.Remote{Host: "github.com", Owner: "a:b", Repo: "widget"},
			wantErr: true,
		},
		{
			name:    "a repo ending in a trailing dot fails",
			tmpl:    "{host}/{owner}/{repo}",
			remote:  remote.Remote{Host: "github.com", Owner: "acme", Repo: "widget."},
			wantErr: true,
		},
		{
			name:    "a repo ending in a trailing space fails",
			tmpl:    "{host}/{owner}/{repo}",
			remote:  remote.Remote{Host: "github.com", Owner: "acme", Repo: "widget "},
			wantErr: true,
		},
		{
			name:    "an owner carrying a right to left override character fails",
			tmpl:    "{host}/{owner}/{repo}",
			remote:  remote.Remote{Host: "github.com", Owner: "acme" + string(rightToLeftOverride) + "widget", Repo: "widget"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := layout.Render(tt.tmpl, tt.remote)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Render(%q, %+v) = %q, want an error", tt.tmpl, tt.remote, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Render(%q, %+v) returned an unexpected error: %v", tt.tmpl, tt.remote, err)
			}
			if got != tt.want {
				t.Fatalf("Render(%q, %+v) = %q, want %q", tt.tmpl, tt.remote, got, tt.want)
			}
		})
	}
}

func TestParsePath(t *testing.T) {
	t.Parallel()

	defaultTemplate := "{host}/{owner}/{repo}"
	templates := []layout.Template{
		{Host: "dev.azure.com", Template: "{host}/{org}/{project}/{repo}"},
	}

	tests := []struct {
		name    string
		path    string
		want    remote.Remote
		wantErr bool
	}{
		{
			name: "default template round trips host owner repo",
			path: "github.com/acme/widget",
			want: remote.Remote{Host: "github.com", Owner: "acme", Repo: "widget"},
		},
		{
			name: "a subgroup owner absorbs the extra path segments",
			path: "gitlab.com/group/subgroup/project",
			want: remote.Remote{Host: "gitlab.com", Owner: "group/subgroup", Repo: "project"},
		},
		{
			name: "a host specific template is preferred over the default",
			path: "dev.azure.com/acme-corp/Widget/widget-repo",
			want: remote.Remote{Host: "dev.azure.com", Owner: "acme-corp", Project: "Widget", Repo: "widget-repo"},
		},
		{
			name:    "a path too short for any template fails",
			path:    "github.com",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := layout.ParsePath(tt.path, templates, defaultTemplate)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParsePath(%q) = %+v, want an error", tt.path, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePath(%q) returned an unexpected error: %v", tt.path, err)
			}
			if got != tt.want {
				t.Fatalf("ParsePath(%q) = %+v, want %+v", tt.path, got, tt.want)
			}
		})
	}
}

// TestRenderKeepsTheRenderedDestinationInsideRoot drives remote.Parse and
// layout.Render together, one accepted remote form at a time, the way
// "gitcd clone", "path", "open", "doctor" and "adopt" all do: parse the
// remote, render it, then join the result onto the configured root. A
// rejected case asserts that no destination is produced at all, rather
// than matching a particular error string; an accepted case asserts that
// the destination still resolves, via filepath.Rel, to a child of root.
func TestRenderKeepsTheRenderedDestinationInsideRoot(t *testing.T) {
	t.Parallel()

	const root = "/srv/code"

	tests := []struct {
		name     string
		input    string
		aliases  map[string]string
		tmpl     string
		wantDest string
		wantErr  bool
	}{
		{
			name:    "the bare host owner repo shorthand with a parent directory owner is rejected",
			input:   "github.com/../widget",
			tmpl:    "{host}/{owner}/{repo}",
			wantErr: true,
		},
		{
			name:    "the scp style form with a parent directory component is rejected",
			input:   "git@github.com:acme/../etc/widget.git",
			tmpl:    "{host}/{owner}/{repo}",
			wantErr: true,
		},
		{
			name:    "an https url with a parent directory component is rejected",
			input:   "https://github.com/acme/../../etc/widget.git",
			tmpl:    "{host}/{owner}/{repo}",
			wantErr: true,
		},
		{
			name:    "an ssh url whose encoded path separator introduces a parent directory component is rejected",
			input:   "ssh://git@github.com/owner%2F..%2Fsecret/widget.git",
			tmpl:    "{host}/{owner}/{repo}",
			wantErr: true,
		},
		{
			name:    "a remote reached through a host alias with a parent directory component is rejected",
			input:   "git@work:acme/../etc/widget.git",
			aliases: map[string]string{"work": "github.com"},
			tmpl:    "{host}/{owner}/{repo}",
			wantErr: true,
		},
		{
			name:    "an azure devops ssh remote with a parent directory project is rejected",
			input:   "git@ssh.dev.azure.com:v3/acme-corp/../widget-repo",
			tmpl:    "{host}/{org}/{project}/{repo}",
			wantErr: true,
		},
		{
			name:    "the scp style form with a second colon in the owner is rejected",
			input:   "git@github.com:a:b/repo.git",
			tmpl:    "{host}/{owner}/{repo}",
			wantErr: true,
		},
		{
			name:    "an https url whose encoded repository decodes to no visible content is rejected",
			input:   "https://github.com/acme/%20",
			tmpl:    "{host}/{owner}/{repo}",
			wantErr: true,
		},
		{
			name:     "a gitlab subgroup owner still renders inside root",
			input:    "git@gitlab.com:group/subgroup/project.git",
			tmpl:     "{host}/{owner}/{repo}",
			wantDest: "gitlab.com/group/subgroup/project",
		},
		{
			name:     "an azure devops https remote still renders inside root",
			input:    "https://dev.azure.com/acme-corp/Widget/_git/widget-repo",
			tmpl:     "{host}/{org}/{project}/{repo}",
			wantDest: "dev.azure.com/acme-corp/Widget/widget-repo",
		},
		{
			name:     "an ssh host alias still renders inside root",
			input:    "git@github-work:acme/widget.git",
			aliases:  map[string]string{"github-work": "github.com"},
			tmpl:     "{host}/{owner}/{repo}",
			wantDest: "github.com/acme/widget",
		},
		{
			name:     "the bare host owner repo shorthand still renders inside root",
			input:    "github.com/acme/widget",
			tmpl:     "{host}/{owner}/{repo}",
			wantDest: "github.com/acme/widget",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, err := remote.Parse(tt.input, tt.aliases)
			if err != nil {
				t.Fatalf("remote.Parse(%q) returned an unexpected error: %v", tt.input, err)
			}

			rel, err := layout.Render(tt.tmpl, r)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("layout.Render(%q, %+v) = %q, want an error", tt.tmpl, r, rel)
				}
				return
			}
			if err != nil {
				t.Fatalf("layout.Render(%q, %+v) returned an unexpected error: %v", tt.tmpl, r, err)
			}

			dest := filepath.Join(root, rel)
			want := filepath.Join(root, tt.wantDest)
			if dest != want {
				t.Fatalf("dest = %q, want %q", dest, want)
			}
			assertDestInsideRoot(t, root, dest)
		})
	}
}

// assertDestInsideRoot fails the test when dest does not resolve, through
// filepath.Rel, to root itself or to a child of it: the property every
// caller that joins Render's own result onto root relies on.
func assertDestInsideRoot(t *testing.T, root, dest string) {
	t.Helper()

	relToRoot, err := filepath.Rel(root, dest)
	if err != nil {
		t.Fatalf("filepath.Rel(%q, %q): %v", root, dest, err)
	}
	if relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
		t.Fatalf("rendered destination %q escapes root %q (relative %q)", dest, root, relToRoot)
	}
}
