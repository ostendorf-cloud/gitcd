// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package remote_test

import (
	"testing"

	"github.com/ostendorf-cloud/gitcd/internal/remote"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		aliases map[string]string
		want    remote.Remote
		wantErr bool
	}{
		{
			name:  "ssh url",
			input: "ssh://git@github.com/acme/widget.git",
			want:  remote.Remote{Host: "github.com", Owner: "acme", Repo: "widget"},
		},
		{
			name:  "https url",
			input: "https://github.com/acme/widget.git",
			want:  remote.Remote{Host: "github.com", Owner: "acme", Repo: "widget"},
		},
		{
			name:  "scp style",
			input: "git@github.com:acme/widget.git",
			want:  remote.Remote{Host: "github.com", Owner: "acme", Repo: "widget"},
		},
		{
			name:  "azure devops https git form",
			input: "https://dev.azure.com/acme-corp/Widget/_git/widget-repo",
			want:  remote.Remote{Host: "dev.azure.com", Owner: "acme-corp", Project: "Widget", Repo: "widget-repo"},
		},
		{
			name:  "bare host owner repo shorthand",
			input: "github.com/acme/widget",
			want:  remote.Remote{Host: "github.com", Owner: "acme", Repo: "widget"},
		},
		{
			name:  "azure devops ssh v3 form without a configured alias",
			input: "git@ssh.dev.azure.com:v3/acme-corp/Widget/widget-repo",
			want: remote.Remote{
				Host: "ssh.dev.azure.com", Owner: "acme-corp", Project: "Widget", Repo: "widget-repo",
			},
		},
		{
			name:    "azure devops ssh v3 form with a configured alias",
			input:   "git@ssh.dev.azure.com:v3/acme-corp/Widget/widget-repo",
			aliases: map[string]string{"ssh.dev.azure.com": "dev.azure.com"},
			want: remote.Remote{
				Host: "dev.azure.com", Owner: "acme-corp", Project: "Widget", Repo: "widget-repo",
			},
		},
		{
			name:  "legacy visualstudio ssh host is recognised as azure devops",
			input: "git@vs-ssh.visualstudio.com:v3/acme-corp/Widget/widget-repo",
			want: remote.Remote{
				Host: "vs-ssh.visualstudio.com", Owner: "acme-corp", Project: "Widget", Repo: "widget-repo",
			},
		},
		{
			name:  "gitlab subgroup owner keeps its slash",
			input: "git@gitlab.com:group/subgroup/project.git",
			want:  remote.Remote{Host: "gitlab.com", Owner: "group/subgroup", Repo: "project"},
		},
		{
			name:    "host alias resolves to the canonical host",
			input:   "git@github-work:acme/widget.git",
			aliases: map[string]string{"github-work": "github.com"},
			want:    remote.Remote{Host: "github.com", Owner: "acme", Repo: "widget"},
		},
		{
			name:  "host is lowercased",
			input: "https://GitHub.COM/Acme/Widget.git",
			want:  remote.Remote{Host: "github.com", Owner: "Acme", Repo: "Widget"},
		},
		{
			name:    "empty input is rejected",
			input:   "",
			wantErr: true,
		},
		{
			name:    "a host without an owner and repository is rejected",
			input:   "github.com",
			wantErr: true,
		},
		{
			name:    "an azure devops path missing a segment is rejected",
			input:   "https://dev.azure.com/acme-corp/_git/widget-repo",
			wantErr: true,
		},
		{
			name:  "azure devops bare shorthand round trips the layout's own rendered path",
			input: "dev.azure.com/myorg/myproject/myrepo",
			want: remote.Remote{
				Host: "dev.azure.com", Owner: "myorg", Project: "myproject", Repo: "myrepo",
			},
		},
		{
			name:    "an azure devops https url without the bare shorthand still requires _git",
			input:   "https://dev.azure.com/myorg/myproject/myrepo",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := remote.Parse(tt.input, tt.aliases)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) = %+v, want an error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) returned an unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("Parse(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsShorthand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "bare host owner repo is shorthand", input: "github.com/acme/widget", want: true},
		{name: "ssh url is not shorthand", input: "ssh://git@github.com/acme/widget.git", want: false},
		{name: "https url is not shorthand", input: "https://github.com/acme/widget.git", want: false},
		{name: "scp style is not shorthand", input: "git@github.com:acme/widget.git", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := remote.IsShorthand(tt.input); got != tt.want {
				t.Fatalf("IsShorthand(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestRemote_URL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		remote   remote.Remote
		protocol string
		want     string
		wantErr  bool
	}{
		{
			name:   "https is the default protocol",
			remote: remote.Remote{Host: "github.com", Owner: "acme", Repo: "widget"},
			want:   "https://github.com/acme/widget.git",
		},
		{
			name:     "ssh renders the scp style form",
			remote:   remote.Remote{Host: "github.com", Owner: "acme", Repo: "widget"},
			protocol: "ssh",
			want:     "git@github.com:acme/widget.git",
		},
		{
			name:     "https renders the azure devops git form",
			remote:   remote.Remote{Host: "dev.azure.com", Owner: "acme-corp", Project: "Widget", Repo: "widget-repo"},
			protocol: "https",
			want:     "https://dev.azure.com/acme-corp/Widget/_git/widget-repo",
		},
		{
			name:     "ssh renders the azure devops v3 form against the fixed ssh host",
			remote:   remote.Remote{Host: "dev.azure.com", Owner: "acme-corp", Project: "Widget", Repo: "widget-repo"},
			protocol: "ssh",
			want:     "git@ssh.dev.azure.com:v3/acme-corp/Widget/widget-repo",
		},
		{
			name:     "an unsupported protocol is rejected",
			remote:   remote.Remote{Host: "github.com", Owner: "acme", Repo: "widget"},
			protocol: "git",
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.remote.URL(tt.protocol)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("URL(%q) = %q, want an error", tt.protocol, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("URL(%q) returned an unexpected error: %v", tt.protocol, err)
			}
			if got != tt.want {
				t.Fatalf("URL(%q) = %q, want %q", tt.protocol, got, tt.want)
			}
		})
	}
}

func TestRemote_WebURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		remote remote.Remote
		want   string
	}{
		{
			name:   "github renders the owner and repo path with no dot git suffix",
			remote: remote.Remote{Host: "github.com", Owner: "acme", Repo: "widget"},
			want:   "https://github.com/acme/widget",
		},
		{
			name:   "azure devops renders its own git path",
			remote: remote.Remote{Host: "dev.azure.com", Owner: "acme-corp", Project: "Widget", Repo: "widget-repo"},
			want:   "https://dev.azure.com/acme-corp/Widget/_git/widget-repo",
		},
		{
			// A self hosted forge, or any host this package has no
			// special case for, still gets a plausible guess: the
			// generic owner/repo shape GitHub, GitLab, Bitbucket and
			// Gitea all share.
			name:   "an unknown host falls back to the generic owner and repo shape",
			remote: remote.Remote{Host: "git.example.test", Owner: "acme", Repo: "widget"},
			want:   "https://git.example.test/acme/widget",
		},
		{
			name:   "a gitlab subgroup owner is preserved",
			remote: remote.Remote{Host: "gitlab.com", Owner: "acme/platform", Repo: "widget"},
			want:   "https://gitlab.com/acme/platform/widget",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.remote.WebURL(); got != tt.want {
				t.Fatalf("WebURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
