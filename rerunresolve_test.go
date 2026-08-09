package main

import (
	"slices"
	"testing"
)

func cfgWithHistory(lastPlaybook string, entries ...playbookHistory) playbookConfig {
	var cfg playbookConfig
	cfg.General.LastPlaybook = lastPlaybook
	cfg.History = entries
	return cfg
}

func TestResolveRerun(t *testing.T) {
	t.Run("no playbook given, resolves from LastPlaybook", func(t *testing.T) {
		cfg := cfgWithHistory("site.yml", playbookHistory{
			Playbook:    "site.yml",
			Invocations: []string{"-l somehost --tags foo,bar"},
		})
		res, ok := resolveRerun(nil, cfg)
		if !ok {
			t.Fatal("resolveRerun() ok = false, want true")
		}
		if res.Playbook != "site.yml" || res.Tags != "foo,bar" || res.Hosts != "somehost" {
			t.Errorf("resolveRerun() = %+v, want Playbook=site.yml Tags=foo,bar Hosts=somehost", res)
		}
	})

	t.Run("no playbook given and no history at all - not ok", func(t *testing.T) {
		_, ok := resolveRerun(nil, playbookConfig{})
		if ok {
			t.Error("resolveRerun() ok = true, want false when nothing has ever been invoked")
		}
	})

	t.Run("explicit playbook wins over LastPlaybook", func(t *testing.T) {
		cfg := cfgWithHistory("other.yml", playbookHistory{
			Playbook:    "site.yml",
			Invocations: []string{"-l somehost"},
		})
		res, ok := resolveRerun([]string{"site.yml"}, cfg)
		if !ok || res.Playbook != "site.yml" || res.Hosts != "somehost" {
			t.Errorf("resolveRerun([site.yml]) = %+v, ok=%v, want Playbook=site.yml Hosts=somehost", res, ok)
		}
	})

	t.Run("explicit playbook never run before - zero-value prefill", func(t *testing.T) {
		cfg := cfgWithHistory("other.yml")
		res, ok := resolveRerun([]string{"new.yml"}, cfg)
		if !ok {
			t.Fatal("resolveRerun() ok = false, want true")
		}
		if res.Playbook != "new.yml" || res.Tags != "" || res.Hosts != "" || res.Rest != nil {
			t.Errorf("resolveRerun([new.yml]) = %+v, want zero-value Tags/Hosts/Rest", res)
		}
	})

	t.Run("CLI --tags/-l override history's own values", func(t *testing.T) {
		cfg := cfgWithHistory("site.yml", playbookHistory{
			Playbook:    "site.yml",
			Invocations: []string{"-l fromhistory --tags fromhistory"},
		})
		res, ok := resolveRerun([]string{"-l", "clihost", "--tags", "clitag"}, cfg)
		if !ok || res.Tags != "clitag" || res.Hosts != "clihost" {
			t.Errorf("resolveRerun with CLI overrides = %+v, ok=%v, want Tags=clitag Hosts=clihost", res, ok)
		}
	})

	t.Run("CLI extra rest args replace history's Rest outright", func(t *testing.T) {
		cfg := cfgWithHistory("site.yml", playbookHistory{
			Playbook:    "site.yml",
			Invocations: []string{"-i old-inventory.ini -l somehost"},
		})
		res, ok := resolveRerun([]string{"-i", "new-inventory.ini"}, cfg)
		if !ok {
			t.Fatal("resolveRerun() ok = false, want true")
		}
		if !slices.Equal(res.Rest, []string{"-i", "new-inventory.ini"}) {
			t.Errorf("res.Rest = %v, want CLI's own rest args to fully replace history's", res.Rest)
		}
		// Hosts wasn't given on the CLI this time, so it still falls back
		// to history - Rest replacement is independent of Tags/Hosts
		// precedence.
		if res.Hosts != "somehost" {
			t.Errorf("res.Hosts = %q, want %q (falls back to history)", res.Hosts, "somehost")
		}
	})

	t.Run("no CLI rest args at all - history's Rest carries forward", func(t *testing.T) {
		cfg := cfgWithHistory("site.yml", playbookHistory{
			Playbook:    "site.yml",
			Invocations: []string{"-i inventory.ini -e x=1"},
		})
		res, ok := resolveRerun(nil, cfg)
		if !ok {
			t.Fatal("resolveRerun() ok = false, want true")
		}
		if !slices.Equal(res.Rest, []string{"-i", "inventory.ini", "-e", "x=1"}) {
			t.Errorf("res.Rest = %v, want history's own rest args carried forward", res.Rest)
		}
	})
}

func TestLastPlaybook(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		if _, ok := lastPlaybook(playbookConfig{}); ok {
			t.Error("lastPlaybook() ok = true, want false for a zero-value config")
		}
	})
	t.Run("set", func(t *testing.T) {
		cfg := cfgWithHistory("site.yml")
		got, ok := lastPlaybook(cfg)
		if !ok || got != "site.yml" {
			t.Errorf("lastPlaybook() = (%q, %v), want (\"site.yml\", true)", got, ok)
		}
	})
	t.Run("unset but exactly one playbook in history - falls back to it", func(t *testing.T) {
		// Covers a .tangsible written entirely by a build predating
		// LastPlaybook: [[history]] entries exist, general.last_playbook
		// doesn't - real case, not hypothetical.
		var cfg playbookConfig
		cfg.History = []playbookHistory{{Playbook: "site.yml", Invocations: []string{"--tags foo"}}}
		got, ok := lastPlaybook(cfg)
		if !ok || got != "site.yml" {
			t.Errorf("lastPlaybook() = (%q, %v), want (\"site.yml\", true)", got, ok)
		}
	})
	t.Run("unset and two or more playbooks in history - still not ok", func(t *testing.T) {
		// Genuinely ambiguous: History's order is first-seen, not recency.
		var cfg playbookConfig
		cfg.History = []playbookHistory{
			{Playbook: "site.yml", Invocations: []string{"--tags foo"}},
			{Playbook: "other.yml", Invocations: []string{"-l host"}},
		}
		if _, ok := lastPlaybook(cfg); ok {
			t.Error("lastPlaybook() ok = true, want false when multiple playbooks exist with no LastPlaybook to disambiguate")
		}
	})
}
