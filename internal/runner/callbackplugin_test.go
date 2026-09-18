// Copyright 2026 Klaus Wissmann
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package runner

import (
	"path/filepath"
	"testing"
)

// TestPrefixRelativeShareDir covers both cases ResolveCallbackPluginDir's
// own doc comment names: the default no-prefix install (~/.local/bin/
// tangsible, which must land on exactly $XDG_DATA_HOME's own default) and
// a --prefix one (must land on exactly what install.sh's own DATA_DIR
// computes for the same --prefix) - one structural rule, no shared
// install-time state between install.sh and this function.
func TestPrefixRelativeShareDir(t *testing.T) {
	cases := []struct {
		name string
		exe  string
		want string
	}{
		{
			name: "default no-prefix install",
			exe:  "/home/user/.local/bin/tangsible",
			want: "/home/user/.local/share/tangsible",
		},
		{
			name: "--prefix install",
			exe:  "/opt/tangsible/bin/tangsible",
			want: "/opt/tangsible/share/tangsible",
		},
		{
			name: "system-wide install",
			exe:  "/usr/local/bin/tangsible",
			want: "/usr/local/share/tangsible",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := prefixRelativeShareDir(filepath.FromSlash(c.exe)); got != filepath.FromSlash(c.want) {
				t.Errorf("prefixRelativeShareDir(%q) = %q, want %q", c.exe, got, c.want)
			}
		})
	}
}
