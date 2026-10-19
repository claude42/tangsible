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

func TestExtractVarsAtStopsOnlyFlag(t *testing.T) {
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
			name:     "present",
			args:     []string{"-i", "localhost,", "--vars-at-stops-only"},
			wantFlag: true,
			wantRest: []string{"-i", "localhost,"},
		},
		{
			name:     "present at the front",
			args:     []string{"--vars-at-stops-only", "-i", "localhost,"},
			wantFlag: true,
			wantRest: []string{"-i", "localhost,"},
		},
		{
			name:     "repeated",
			args:     []string{"--vars-at-stops-only", "--vars-at-stops-only"},
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
			gotFlag, gotRest := extractVarsAtStopsOnlyFlag(c.args)
			if gotFlag != c.wantFlag {
				t.Errorf("extractVarsAtStopsOnlyFlag(%v) flag = %v, want %v", c.args, gotFlag, c.wantFlag)
			}
			if !reflect.DeepEqual(gotRest, c.wantRest) {
				t.Errorf("extractVarsAtStopsOnlyFlag(%v) rest = %v, want %v", c.args, gotRest, c.wantRest)
			}
		})
	}
}
