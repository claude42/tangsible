package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIniValue(t *testing.T) {
	content := `# a comment
[defaults]
; another comment style
vault_password_file = ./.vault_pass
inventory=./inventory.ini

[galaxy]
vault_password_file = wrong_section.txt
`
	cases := []struct {
		name    string
		section string
		key     string
		want    string
	}{
		{"basic lookup", "defaults", "vault_password_file", "./.vault_pass"},
		{"colon-separated form", "defaults", "inventory", "./inventory.ini"},
		{"section is case-insensitive", "DEFAULTS", "vault_password_file", "./.vault_pass"},
		{"same key in a different section doesn't match", "galaxy", "missing_key", ""},
		{"missing key", "defaults", "no_such_key", ""},
		{"missing section", "no_such_section", "vault_password_file", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := iniValue(content, c.section, c.key)
			if got != c.want {
				t.Errorf("iniValue(%q, %q) = %q, want %q", c.section, c.key, got, c.want)
			}
		})
	}
}

func TestIniValue_SectionScoping(t *testing.T) {
	// vault_password_file in [galaxy] must not leak into a [defaults] lookup.
	content := "[galaxy]\nvault_password_file = wrong.txt\n[defaults]\nother_key = fine\n"
	if got := iniValue(content, "defaults", "vault_password_file"); got != "" {
		t.Errorf("iniValue leaked a value from the wrong section: %q", got)
	}
}

// TestAnsibleCfgVaultPasswordFile_FindsProjectAnsibleCfg is an end-to-end
// check for the actual reported bug: a project with a real ansible.cfg
// setting vault_password_file must have it picked up automatically,
// without any tangsible-specific flag or env var.
func TestAnsibleCfgVaultPasswordFile_FindsProjectAnsibleCfg(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("ANSIBLE_CONFIG", "") // don't let a real env var from outside the test leak in

	if err := os.WriteFile(filepath.Join(dir, "ansible.cfg"), []byte("[defaults]\nvault_password_file = ./.vault_pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := ansibleCfgVaultPasswordFile()
	if got != "./.vault_pass" {
		t.Errorf("ansibleCfgVaultPasswordFile() = %q, want \"./.vault_pass\"", got)
	}
}

func TestAnsibleCfgVaultPasswordFile_NoAnsibleCfg(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("ANSIBLE_CONFIG", "")
	// Also isolate $HOME so a real ~/.ansible.cfg on the machine running
	// this test can't make it flaky.
	t.Setenv("HOME", dir)

	if got := ansibleCfgVaultPasswordFile(); got != "" {
		t.Errorf("ansibleCfgVaultPasswordFile() = %q, want empty when there's no ansible.cfg at all", got)
	}
}
