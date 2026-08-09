package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDecodeHostResult(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want hostResult
	}{
		{
			name: "changed",
			raw:  `{"changed":true,"msg":"did a thing"}`,
			want: hostResult{Changed: true, Msg: "did a thing"},
		},
		{
			name: "failed",
			raw:  `{"failed":true,"msg":"nope"}`,
			want: hostResult{Failed: true, Msg: "nope"},
		},
		{
			name: "unreachable",
			raw:  `{"unreachable":true}`,
			want: hostResult{Unreachable: true},
		},
		{
			name: "skipped",
			raw:  `{"skipped":true}`,
			want: hostResult{Skipped: true},
		},
		{
			// A decode error must be treated as "no flags set," not
			// surfaced - the caller still wants to record the raw
			// payload even if it can't be classified.
			name: "malformed JSON",
			raw:  `not json at all`,
			want: hostResult{},
		},
		{
			name: "valid JSON but not an object",
			raw:  `[1,2,3]`,
			want: hostResult{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := decodeHostResult(json.RawMessage(c.raw))
			if got != c.want {
				t.Errorf("decodeHostResult(%s) = %+v, want %+v", c.raw, got, c.want)
			}
		})
	}
}

func TestRawEvent_Timestamp(t *testing.T) {
	cases := []struct {
		name string
		text string
		want time.Time
	}{
		{
			name: "RFC3339 with fractional seconds",
			text: "2026-08-06T12:41:02.439015Z",
			want: time.Date(2026, 8, 6, 12, 41, 2, 439015000, time.UTC),
		},
		{
			name: "empty string",
			text: "",
			want: time.Time{},
		},
		{
			name: "garbage string",
			text: "not-a-timestamp",
			want: time.Time{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev := rawEvent{TimestampText: c.text}
			got := ev.Timestamp()
			if !got.Equal(c.want) {
				t.Errorf("Timestamp() = %v, want %v", got, c.want)
			}
		})
	}
}
