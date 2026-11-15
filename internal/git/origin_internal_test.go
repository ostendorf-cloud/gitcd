// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package git

import "testing"

func TestUnquoteOrigin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, in, want string
	}{
		{name: "a plain path is unchanged", in: "/home/simon/.gitconfig", want: "/home/simon/.gitconfig"},
		{name: "a Windows path git quoted", in: `"C:\\Users\\RUNNER~1\\work.gitconfig"`, want: `C:\Users\RUNNER~1\work.gitconfig`},
		{name: "a UTF-8 name as git prints it, octal escaped", in: `"/tmp/r/C\303\263digo/c.gitconfig"`, want: "/tmp/r/Código/c.gitconfig"},
		{name: "a forward slash Windows path is unchanged", in: "C:/Users/x/config", want: "C:/Users/x/config"},
		{name: "a malformed quoted name is unchanged", in: `"C:\Users\x"`, want: `"C:\Users\x"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := unquoteOrigin(tt.in); got != tt.want {
				t.Fatalf("unquoteOrigin(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
