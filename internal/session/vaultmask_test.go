package session

import (
	"encoding/json"
	"testing"
)

func TestMaskSnapshot(t *testing.T) {
	vars := json.RawMessage(`{"region":"us-east-1","db_password":"Hunter2pass","url":"pg://admin:hunter2PASS@db","cfg":{"tok":"x","list":["a","b"]},"port":5432,"pin":"1","note":"1 of 1"}`)
	mask := vaultMask{
		Vaulted: [][]any{{"db_password"}, {"cfg", "list", float64(1)}, {"gone", "x"}},
		Secrets: []string{"Hunter2pass", "1"},
	}
	var got map[string]any
	if err := json.Unmarshal(maskSnapshot(vars, mask), &got); err != nil {
		t.Fatal(err)
	}
	if got["db_password"] != "<vaulted>" {
		t.Errorf("vaulted path not masked: %v", got["db_password"])
	}
	if got["url"] != "pg://admin:<vaulted>@db" {
		t.Errorf("derived value: %v", got["url"])
	}
	if got["region"] != "us-east-1" || got["port"] != float64(5432) {
		t.Errorf("unrelated values changed: %v", got)
	}
	list := got["cfg"].(map[string]any)["list"].([]any)
	if list[0] != "a" || list[1] != "<vaulted>" {
		t.Errorf("index path: %v", list)
	}
	if got["note"] != "1 of 1" {
		t.Errorf("secret below the length floor must not be searched for: %v", got["note"])
	}
}

func TestMaskSnapshotEmptyMaskUnchanged(t *testing.T) {
	vars := json.RawMessage(`{"a":"<b>"}`)
	if got := maskSnapshot(vars, vaultMask{}); string(got) != string(vars) {
		t.Errorf("got %s", got)
	}
}

func TestMaskSnapshotKeepsBigNumbersAndHTML(t *testing.T) {
	got := string(maskSnapshot(json.RawMessage(`{"n":12345678901234567890,"s":"<x>","p":"secret1"}`), vaultMask{Vaulted: [][]any{{"p"}}}))
	if got != `{"n":12345678901234567890,"p":"<vaulted>","s":"<x>"}` {
		t.Errorf("got %s", got)
	}
}

func TestParseSnapshotQuery(t *testing.T) {
	vars, mask, origins, ok := parseSnapshotQuery(json.RawMessage(`{"vars":{"a":1},"vaulted":[["a"]],"secrets":["s3cret"],"origins":{"a":{"path":"/p/x.yml","line":3}}}`))
	if !ok || string(vars) != `{"a":1}` || len(mask.Vaulted) != 1 || mask.Secrets[0] != "s3cret" || origins["a"].Line != 3 {
		t.Errorf("got %s %+v %v %v", vars, mask, origins, ok)
	}
	if _, _, _, ok := parseSnapshotQuery(json.RawMessage(`{"a":1}`)); ok {
		t.Error("a bare task_vars object must not parse as a helper reply")
	}
}
