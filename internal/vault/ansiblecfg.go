package vault

import (
	"os"
	"path/filepath"
	"strings"
)

// locateAnsibleConfig finds ansible.cfg the same way ansible itself does:
// $ANSIBLE_CONFIG, then ./ansible.cfg, then ~/.ansible.cfg, then
// /etc/ansible/ansible.cfg, first one that actually exists wins. This is
// deliberately a small subset of ansible's own real resolution (which
// also considers the playbook's own directory in some contexts) - good
// enough for finding a project's vault_password_file setting, not a
// general ansible.cfg resolver.
func locateAnsibleConfig() (string, bool) {
	if v := os.Getenv("ANSIBLE_CONFIG"); v != "" {
		if fileExists(v) {
			return v, true
		}
	}
	if fileExists("ansible.cfg") {
		return "ansible.cfg", true
	}
	if home, err := os.UserHomeDir(); err == nil {
		if p := filepath.Join(home, ".ansible.cfg"); fileExists(p) {
			return p, true
		}
	}
	if fileExists("/etc/ansible/ansible.cfg") {
		return "/etc/ansible/ansible.cfg", true
	}
	return "", false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// ansibleCfgVaultPasswordFile reads [defaults] vault_password_file from
// whatever ansible.cfg locateAnsibleConfig finds - "" if there's no
// ansible.cfg, no [defaults] section, or no such key. This is the
// standard, most common place ansible users already configure their
// vault password, so it has to be a real source here even though it's
// not something tangsible itself owns or writes - a plain, best-effort
// read, silently absent rather than an error, matching this codebase's
// existing convention for optional config sources
// (internal/config.ReadTOMLFile's own "missing file, no warning" rule).
func ansibleCfgVaultPasswordFile() string {
	path, ok := locateAnsibleConfig()
	if !ok {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return iniValue(string(data), "defaults", "vault_password_file")
}

// iniValue is a minimal INI reader, not a general one: it finds the
// first "key = value" line (or "key: value" - ansible.cfg's own parser,
// Python's configparser, accepts both) within the named section,
// case-insensitive on the section name (matching configparser's own
// default behavior), and returns its trimmed value. "#"/";" line
// comments and blank lines are skipped; a section header is any
// "[name]" line. Good enough for reading one well-known key out of a
// well-formed ansible.cfg - not a validating parser, and not chased
// further than that, same "documented heuristic" spirit as this
// project's other minimal parsers (e.g. internal/config/rerunargs.go's
// tokenizer).
func iniValue(content, section, key string) string {
	inSection := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inSection = strings.EqualFold(strings.TrimSpace(trimmed[1:len(trimmed)-1]), section)
			continue
		}
		if !inSection {
			continue
		}
		k, v, ok := strings.Cut(trimmed, "=")
		if !ok {
			k, v, ok = strings.Cut(trimmed, ":")
		}
		if ok && strings.EqualFold(strings.TrimSpace(k), key) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
