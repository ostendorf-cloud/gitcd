// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package layout_test

import (
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/layout"
	"github.com/ostendorf-cloud/gitcd/internal/remote"
)

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
			name:    "an unknown placeholder fails",
			tmpl:    "{host}/{owner}/{branch}",
			remote:  remote.Remote{Host: "github.com", Owner: "acme", Repo: "widget"},
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
