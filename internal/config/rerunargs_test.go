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

package config

import (
	"slices"
	"testing"
)

func TestParsePassthroughArgs(t *testing.T) {
	cases := []struct {
		name         string
		args         []string
		wantTags     string
		wantSkipTags string
		wantHosts    string
		wantRest     []string
	}{
		{
			name:     "no tags or hosts",
			args:     []string{"-i", "inv.ini"},
			wantRest: []string{"-i", "inv.ini"},
		},
		{
			name:     "--tags space form",
			args:     []string{"--tags", "foo,bar"},
			wantTags: "foo,bar",
		},
		{
			name:     "--tags equals form",
			args:     []string{"--tags=foo,bar"},
			wantTags: "foo,bar",
		},
		{
			name:     "-t short space form",
			args:     []string{"-t", "foo"},
			wantTags: "foo",
		},
		{
			name:         "--skip-tags space form",
			args:         []string{"--skip-tags", "foo,bar"},
			wantSkipTags: "foo,bar",
		},
		{
			name:         "--skip-tags equals form",
			args:         []string{"--skip-tags=foo,bar"},
			wantSkipTags: "foo,bar",
		},
		{
			name:      "--limit space form",
			args:      []string{"--limit", "somehost"},
			wantHosts: "somehost",
		},
		{
			name:      "-l short space form",
			args:      []string{"-l", "host1,host2"},
			wantHosts: "host1,host2",
		},
		{
			name:         "combined and interspersed with rest",
			args:         []string{"-i", "inv.ini", "--tags", "foo,bar", "-e", "x=1", "--skip-tags", "baz", "-l", "host1,host2"},
			wantTags:     "foo,bar",
			wantSkipTags: "baz",
			wantHosts:    "host1,host2",
			wantRest:     []string{"-i", "inv.ini", "-e", "x=1"},
		},
		{
			name:     "repeated --tags occurrences are comma-joined",
			args:     []string{"--tags", "foo", "--tags", "bar"},
			wantTags: "foo,bar",
		},
		{
			name:         "repeated --skip-tags occurrences are comma-joined",
			args:         []string{"--skip-tags", "foo", "--skip-tags", "bar"},
			wantSkipTags: "foo,bar",
		},
		{
			name:     "dangling flag with no value falls through to Rest",
			args:     []string{"--tags"},
			wantRest: []string{"--tags"},
		},
		{
			name: "empty args",
			args: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParsePassthroughArgs(c.args)
			if got.Tags != c.wantTags || got.SkipTags != c.wantSkipTags || got.Hosts != c.wantHosts || !slices.Equal(got.Rest, c.wantRest) {
				t.Errorf("parsePassthroughArgs(%v) = %+v, want Tags=%q SkipTags=%q Hosts=%q Rest=%v",
					c.args, got, c.wantTags, c.wantSkipTags, c.wantHosts, c.wantRest)
			}
		})
	}
}

func TestParsedPassthroughArgsReassemble(t *testing.T) {
	cases := []struct {
		name string
		p    ParsedPassthroughArgs
		want []string
	}{
		{
			name: "tags, skip tags, hosts and rest all present",
			p:    ParsedPassthroughArgs{Tags: "foo,bar", SkipTags: "baz", Hosts: "h1,h2", Rest: []string{"-i", "inv.ini"}},
			want: []string{"--tags", "foo,bar", "--skip-tags", "baz", "--limit", "h1,h2", "-i", "inv.ini"},
		},
		{
			name: "only rest",
			p:    ParsedPassthroughArgs{Rest: []string{"-i", "inv.ini"}},
			want: []string{"-i", "inv.ini"},
		},
		{
			name: "all empty",
			p:    ParsedPassthroughArgs{},
			want: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.p.Reassemble(); !slices.Equal(got, c.want) {
				t.Errorf("Reassemble() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestParsePassthroughArgsReassembleRoundTrip(t *testing.T) {
	args := []string{"--tags", "foo,bar", "--skip-tags", "baz", "--limit", "host1,host2", "-i", "inv.ini", "-e", "x=1"}
	got := ParsePassthroughArgs(args).Reassemble()
	if !slices.Equal(got, args) {
		t.Errorf("round trip = %v, want %v", got, args)
	}
}

func TestArgsToHistoryStringRoundTrip(t *testing.T) {
	cases := [][]string{
		nil,
		{},
		{"-l", "somehost"},
		{"--tags", "foo,bar"},
		{"-l", "host1,host2", "--tags", "foo,bar", "-e", "bla=fasel"},
		{"-e", "msg=hello world"},
		{"-e", `msg=has "quotes" in it`},
		{"-e", `path=C:\some\thing`},
		{""},
	}
	for _, args := range cases {
		s := ArgsToHistoryString(args)
		got := HistoryStringToArgs(s)
		want := args
		if len(want) == 0 {
			want = nil
		}
		if !slices.Equal(got, want) {
			t.Errorf("round trip of %v via %q = %v, want %v", args, s, got, want)
		}
	}
}

func TestArgsToHistoryStringExactForm(t *testing.T) {
	if got := ArgsToHistoryString(nil); got != "" {
		t.Errorf("argsToHistoryString(nil) = %q, want empty string", got)
	}
	if got := ArgsToHistoryString([]string{"-l", "somehost"}); got != "-l somehost" {
		t.Errorf("argsToHistoryString([-l somehost]) = %q, want %q", got, "-l somehost")
	}
}

func TestHistoryStringToArgsEmptyString(t *testing.T) {
	if got := HistoryStringToArgs(""); got != nil {
		t.Errorf("historyStringToArgs(\"\") = %v, want nil", got)
	}
}

func TestExtractStartAtPlay(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantPlay string
		wantRest []string
	}{
		{"absent", []string{"-i", "localhost,"}, "", []string{"-i", "localhost,"}},
		{"two-token form", []string{"-i", "localhost,", "--start-at-play", "second play", "-v"}, "second play", []string{"-i", "localhost,", "-v"}},
		{"equals form", []string{"--start-at-play=second play", "-v"}, "second play", []string{"-v"}},
		{"last occurrence wins", []string{"--start-at-play", "first", "--start-at-play", "second"}, "second", nil},
		{"dangling flag left untouched", []string{"-v", "--start-at-play"}, "", []string{"-v", "--start-at-play"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPlay, gotRest := ExtractStartAtPlay(tt.args)
			if gotPlay != tt.wantPlay {
				t.Errorf("play = %q, want %q", gotPlay, tt.wantPlay)
			}
			if !slices.Equal(gotRest, tt.wantRest) {
				t.Errorf("rest = %v, want %v", gotRest, tt.wantRest)
			}
		})
	}
}

func TestExtractStartAtPlay_NoneFoundReturnsSameSlice(t *testing.T) {
	args := []string{"-i", "localhost,"}
	_, rest := ExtractStartAtPlay(args)
	if &rest[0] != &args[0] {
		t.Error("rest should be the same underlying slice as args when nothing was found")
	}
}

func TestHasCheckFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"absent", []string{"-i", "localhost,"}, false},
		{"long form", []string{"-i", "localhost,", "--check"}, true},
		{"short form", []string{"-i", "localhost,", "-C"}, true},
		{"empty args", nil, false},
		{"attached bundled short form not recognized", []string{"-vC"}, false},
		{"value-bearing form not recognized", []string{"--check=true"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasCheckFlag(tt.args); got != tt.want {
				t.Errorf("HasCheckFlag(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}
