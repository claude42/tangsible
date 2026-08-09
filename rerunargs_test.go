package main

import (
	"slices"
	"testing"
)

func TestParsePassthroughArgs(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		wantTags  string
		wantHosts string
		wantRest  []string
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
			name:      "combined and interspersed with rest",
			args:      []string{"-i", "inv.ini", "--tags", "foo,bar", "-e", "x=1", "-l", "host1,host2"},
			wantTags:  "foo,bar",
			wantHosts: "host1,host2",
			wantRest:  []string{"-i", "inv.ini", "-e", "x=1"},
		},
		{
			name:     "repeated --tags occurrences are comma-joined",
			args:     []string{"--tags", "foo", "--tags", "bar"},
			wantTags: "foo,bar",
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
			got := parsePassthroughArgs(c.args)
			if got.Tags != c.wantTags || got.Hosts != c.wantHosts || !slices.Equal(got.Rest, c.wantRest) {
				t.Errorf("parsePassthroughArgs(%v) = %+v, want Tags=%q Hosts=%q Rest=%v",
					c.args, got, c.wantTags, c.wantHosts, c.wantRest)
			}
		})
	}
}

func TestParsedPassthroughArgsReassemble(t *testing.T) {
	cases := []struct {
		name string
		p    parsedPassthroughArgs
		want []string
	}{
		{
			name: "tags, hosts and rest all present",
			p:    parsedPassthroughArgs{Tags: "foo,bar", Hosts: "h1,h2", Rest: []string{"-i", "inv.ini"}},
			want: []string{"--tags", "foo,bar", "--limit", "h1,h2", "-i", "inv.ini"},
		},
		{
			name: "only rest",
			p:    parsedPassthroughArgs{Rest: []string{"-i", "inv.ini"}},
			want: []string{"-i", "inv.ini"},
		},
		{
			name: "all empty",
			p:    parsedPassthroughArgs{},
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
	args := []string{"--tags", "foo,bar", "--limit", "host1,host2", "-i", "inv.ini", "-e", "x=1"}
	got := parsePassthroughArgs(args).Reassemble()
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
		s := argsToHistoryString(args)
		got := historyStringToArgs(s)
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
	if got := argsToHistoryString(nil); got != "" {
		t.Errorf("argsToHistoryString(nil) = %q, want empty string", got)
	}
	if got := argsToHistoryString([]string{"-l", "somehost"}); got != "-l somehost" {
		t.Errorf("argsToHistoryString([-l somehost]) = %q, want %q", got, "-l somehost")
	}
}

func TestHistoryStringToArgsEmptyString(t *testing.T) {
	if got := historyStringToArgs(""); got != nil {
		t.Errorf("historyStringToArgs(\"\") = %v, want nil", got)
	}
}
