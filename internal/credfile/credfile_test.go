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

package credfile

import (
	"os"
	"reflect"
	"testing"
)

func TestRewrite_ReplacesAskFlagsWithPrivateFiles(t *testing.T) {
	asked := []string{}
	prompt := func(label string) (string, error) {
		asked = append(asked, label)
		return "pw-" + label, nil
	}
	out, cleanup, err := Rewrite([]string{"-i", "inv", "-K", "--ask-vault-pass", "-K", "-e", "x=1"}, prompt)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(asked, []string{"BECOME password", "Vault password"}) {
		t.Errorf("asked = %v (repeated flag must prompt once)", asked)
	}
	if len(out) != 8 || out[0] != "-i" || out[2] != "--become-password-file" || out[4] != "--vault-password-file" || out[7] != "x=1" {
		t.Fatalf("out = %v", out)
	}
	for i, want := range map[int]string{3: "pw-BECOME password", 5: "pw-Vault password"} {
		fi, err := os.Stat(out[i])
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v, want 0600", out[i], fi.Mode().Perm())
		}
		if b, _ := os.ReadFile(out[i]); string(b) != want {
			t.Errorf("%s = %q, want %q", out[i], b, want)
		}
	}
	cleanup()
	if _, err := os.Stat(out[3]); !os.IsNotExist(err) {
		t.Error("cleanup left the password file behind")
	}
}

func TestRewrite_NothingToAskLeavesArgsAlone(t *testing.T) {
	in := []string{"-i", "inv", "--tags", "x"}
	out, cleanup, err := Rewrite(in, func(string) (string, error) {
		t.Fatal("must not prompt")
		return "", nil
	})
	cleanup()
	if err != nil || !reflect.DeepEqual(out, in) {
		t.Errorf("out=%v err=%v", out, err)
	}
}
