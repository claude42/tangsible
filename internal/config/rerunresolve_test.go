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

func cfgWithHistory(last string, entries ...PlaybookHistory) StateConfig {
	var cfg StateConfig
	cfg.General.Last = last
	cfg.History = entries
	return cfg
}

func TestResolveRerun(t *testing.T) {
	t.Run("no playbook given, resolves from General.Last", func(t *testing.T) {
		cfg := cfgWithHistory("site.yml", PlaybookHistory{
			Playbook:    "site.yml",
			Invocations: []InvocationRecord{{Args: "-l somehost --tags foo,bar --skip-tags skip1,skip2"}},
		})
		res, ok := ResolveRerun(nil, cfg)
		if !ok {
			t.Fatal("resolveRerun() ok = false, want true")
		}
		if res.Playbook != "site.yml" || res.Tags != "foo,bar" || res.SkipTags != "skip1,skip2" || res.Hosts != "somehost" {
			t.Errorf("resolveRerun() = %+v, want Playbook=site.yml Tags=foo,bar SkipTags=skip1,skip2 Hosts=somehost", res)
		}
	})

	t.Run("no playbook given and no history at all - not ok", func(t *testing.T) {
		_, ok := ResolveRerun(nil, StateConfig{})
		if ok {
			t.Error("resolveRerun() ok = true, want false when nothing has ever been invoked")
		}
	})

	t.Run("explicit playbook wins over General.Last", func(t *testing.T) {
		cfg := cfgWithHistory("other.yml", PlaybookHistory{
			Playbook:    "site.yml",
			Invocations: []InvocationRecord{{Args: "-l somehost"}},
		})
		res, ok := ResolveRerun([]string{"site.yml"}, cfg)
		if !ok || res.Playbook != "site.yml" || res.Hosts != "somehost" {
			t.Errorf("resolveRerun([site.yml]) = %+v, ok=%v, want Playbook=site.yml Hosts=somehost", res, ok)
		}
	})

	t.Run("explicit playbook never run before - zero-value prefill", func(t *testing.T) {
		cfg := cfgWithHistory("other.yml")
		res, ok := ResolveRerun([]string{"new.yml"}, cfg)
		if !ok {
			t.Fatal("resolveRerun() ok = false, want true")
		}
		if res.Playbook != "new.yml" || res.Tags != "" || res.Hosts != "" || res.Rest != nil {
			t.Errorf("resolveRerun([new.yml]) = %+v, want zero-value Tags/Hosts/Rest", res)
		}
	})

	t.Run("CLI --tags/--skip-tags/-l override history's own values", func(t *testing.T) {
		cfg := cfgWithHistory("site.yml", PlaybookHistory{
			Playbook:    "site.yml",
			Invocations: []InvocationRecord{{Args: "-l fromhistory --tags fromhistory --skip-tags fromhistory"}},
		})
		res, ok := ResolveRerun([]string{"-l", "clihost", "--tags", "clitag", "--skip-tags", "cliskip"}, cfg)
		if !ok || res.Tags != "clitag" || res.SkipTags != "cliskip" || res.Hosts != "clihost" {
			t.Errorf("resolveRerun with CLI overrides = %+v, ok=%v, want Tags=clitag SkipTags=cliskip Hosts=clihost", res, ok)
		}
	})

	t.Run("CLI extra rest args replace history's Rest outright", func(t *testing.T) {
		cfg := cfgWithHistory("site.yml", PlaybookHistory{
			Playbook:    "site.yml",
			Invocations: []InvocationRecord{{Args: "-i old-inventory.ini -l somehost"}},
		})
		res, ok := ResolveRerun([]string{"-i", "new-inventory.ini"}, cfg)
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
		cfg := cfgWithHistory("site.yml", PlaybookHistory{
			Playbook:    "site.yml",
			Invocations: []InvocationRecord{{Args: "-i inventory.ini -e x=1"}},
		})
		res, ok := ResolveRerun(nil, cfg)
		if !ok {
			t.Fatal("resolveRerun() ok = false, want true")
		}
		if !slices.Equal(res.Rest, []string{"-i", "inventory.ini", "-e", "x=1"}) {
			t.Errorf("res.Rest = %v, want history's own rest args carried forward", res.Rest)
		}
	})

	t.Run("no playbook given, last invocation was a role - resolves to Role, not Playbook", func(t *testing.T) {
		cfg := cfgWithHistory("myrole", PlaybookHistory{
			Role:        "myrole",
			Invocations: []InvocationRecord{{Args: "-l nirvana --tags foo"}},
		})
		res, ok := ResolveRerun(nil, cfg)
		if !ok {
			t.Fatal("resolveRerun() ok = false, want true")
		}
		if res.Role != "myrole" || res.Playbook != "" || res.Tags != "foo" || res.Hosts != "nirvana" {
			t.Errorf("resolveRerun() = %+v, want Role=myrole Playbook=\"\" Tags=foo Hosts=nirvana", res)
		}
	})

	t.Run("an explicit playbook positional always resolves to Playbook, never Role", func(t *testing.T) {
		// Rerun.md/design-docs/Tangsible role.md scope decision: there's
		// no "tangsible rerun <role>" form - a positional argument to
		// rerun is always a playbook path, even if the last invocation on
		// record happened to be a role.
		cfg := cfgWithHistory("myrole", PlaybookHistory{
			Role:        "myrole",
			Invocations: []InvocationRecord{{Args: "-l nirvana"}},
		})
		res, ok := ResolveRerun([]string{"site.yml"}, cfg)
		if !ok || res.Playbook != "site.yml" || res.Role != "" {
			t.Errorf("resolveRerun([site.yml]) = %+v, ok=%v, want Playbook=site.yml Role=\"\"", res, ok)
		}
	})
}

func TestLastTarget(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		if _, ok := LastTarget(StateConfig{}); ok {
			t.Error("lastTarget() ok = true, want false for a zero-value config")
		}
	})
	t.Run("set to a playbook", func(t *testing.T) {
		cfg := cfgWithHistory("site.yml", PlaybookHistory{Playbook: "site.yml"})
		entry, ok := LastTarget(cfg)
		if !ok || entry.Playbook != "site.yml" || entry.Role != "" {
			t.Errorf("lastTarget() = (%+v, %v), want (Playbook=\"site.yml\" Role=\"\", true)", entry, ok)
		}
	})
	t.Run("set to a role", func(t *testing.T) {
		cfg := cfgWithHistory("myrole", PlaybookHistory{Role: "myrole"})
		entry, ok := LastTarget(cfg)
		if !ok || entry.Role != "myrole" || entry.Playbook != "" {
			t.Errorf("lastTarget() = (%+v, %v), want (Role=\"myrole\" Playbook=\"\", true)", entry, ok)
		}
	})
	t.Run("unset but exactly one entry in history - falls back to it", func(t *testing.T) {
		// Covers a .tangsible written entirely by a build predating this
		// field (or its own predecessor, LastPlaybook): [[history]]
		// entries exist, general.last doesn't - real case, not
		// hypothetical.
		var cfg StateConfig
		cfg.History = []PlaybookHistory{{Playbook: "site.yml", Invocations: []InvocationRecord{{Args: "--tags foo"}}}}
		entry, ok := LastTarget(cfg)
		if !ok || entry.Playbook != "site.yml" {
			t.Errorf("lastTarget() = (%+v, %v), want (Playbook=\"site.yml\", true)", entry, ok)
		}
	})
	t.Run("unset and two or more entries in history - still not ok", func(t *testing.T) {
		// Genuinely ambiguous: History's order is first-seen, not recency.
		var cfg StateConfig
		cfg.History = []PlaybookHistory{
			{Playbook: "site.yml", Invocations: []InvocationRecord{{Args: "--tags foo"}}},
			{Role: "myrole", Invocations: []InvocationRecord{{Args: "-l host"}}},
		}
		if _, ok := LastTarget(cfg); ok {
			t.Error("lastTarget() ok = true, want false when multiple entries exist with no General.Last to disambiguate")
		}
	})
}
