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

package debugverb

import (
	"reflect"
	"testing"
)

func TestExtractAllVarsFlag(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantFlag bool
		wantRest []string
	}{
		{
			name:     "absent",
			args:     []string{"-i", "localhost,"},
			wantFlag: false,
			wantRest: []string{"-i", "localhost,"},
		},
		{
			name:     "present, long form",
			args:     []string{"-i", "localhost,", "--all-vars"},
			wantFlag: true,
			wantRest: []string{"-i", "localhost,"},
		},
		{
			name:     "present, short form",
			args:     []string{"-i", "localhost,", "-a"},
			wantFlag: true,
			wantRest: []string{"-i", "localhost,"},
		},
		{
			name:     "present at the front",
			args:     []string{"--all-vars", "-i", "localhost,"},
			wantFlag: true,
			wantRest: []string{"-i", "localhost,"},
		},
		{
			name:     "repeated, both spellings",
			args:     []string{"--all-vars", "-a"},
			wantFlag: true,
			wantRest: []string{},
		},
		{
			name:     "empty",
			args:     []string{},
			wantFlag: false,
			wantRest: []string{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotFlag, gotRest := extractAllVarsFlag(c.args)
			if gotFlag != c.wantFlag {
				t.Errorf("extractAllVarsFlag(%v) flag = %v, want %v", c.args, gotFlag, c.wantFlag)
			}
			if !reflect.DeepEqual(gotRest, c.wantRest) {
				t.Errorf("extractAllVarsFlag(%v) rest = %v, want %v", c.args, gotRest, c.wantRest)
			}
		})
	}
}
