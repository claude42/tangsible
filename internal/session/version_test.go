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

package session

import "testing"

func TestParseCollectionVersion(t *testing.T) {
	// Real `ansible-galaxy collection list ansible.posix --format json` shape.
	const oneMatch = `{"/usr/lib/python3/dist-packages/ansible_collections":{"ansible.posix":{"version":"2.1.0"}}}`
	if v, loc, ok := parseCollectionVersion([]byte(oneMatch), "ansible.posix"); !ok || v != "2.1.0" ||
		loc != "/usr/lib/python3/dist-packages/ansible_collections" {
		t.Fatalf("oneMatch: got %q %q %v", v, loc, ok)
	}

	// Collection listed but with no real version (galaxy prints "*" for a
	// path-mode / dev install).
	const noVersion = `{"/x":{"ansible.posix":{"version":"*"}}}`
	if _, _, ok := parseCollectionVersion([]byte(noVersion), "ansible.posix"); ok {
		t.Error(`noVersion: expected ok=false for version "*"`)
	}

	// Requested collection just isn't there.
	const other = `{"/x":{"community.general":{"version":"9.0.0"}}}`
	if _, _, ok := parseCollectionVersion([]byte(other), "ansible.posix"); ok {
		t.Error("other: expected ok=false when ansible.posix absent")
	}

	// Not JSON at all (galaxy failed / printed a warning to stdout).
	if _, _, ok := parseCollectionVersion([]byte("WARNING: something\n"), "ansible.posix"); ok {
		t.Error("garbage: expected ok=false")
	}
}
