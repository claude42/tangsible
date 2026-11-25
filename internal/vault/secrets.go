package vault

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/vaultcrypto"
	"code.aw.net/claude/tangsible/internal/vaultfile"
)

// Args is what the vault verb takes: the target file plus the options that
// say where the passwords come from, mirroring ansible-vault's own
// (design-docs/Vault.md, "Password source" and "Vault ids").
type Args struct {
	Filename     string
	PasswordFile string   // --vault-password-file (--vault-pass-file)
	AskPass      bool     // --ask-vault-pass (--ask-vault-password)
	VaultIDs     []string // each --vault-id, "label@source" or just a source
	EncryptID    string   // --encrypt-vault-id
}

// ParseVaultArgs splits args (everything after the "vault" verb) into the
// target filename and the password options. Hand-rolled rather than the
// stdlib flag package (used nowhere else in this codebase), matching
// internal/config/rerunargs.go's ParsePassthroughArgs style. The two
// "one password" options are mutually exclusive, as in ansible-vault; a
// --vault-id or --encrypt-vault-id with no value is an error (silently
// ignoring a vault id would fall back to a prompt the user did not expect).
func ParseVaultArgs(args []string) (Args, error) {
	var out Args
	var rest []string

	// take reports the value of a flag written "--flag value" or
	// "--flag=value"; consumed is how many arguments it used.
	take := func(flag string, i int) (value string, consumed int, ok bool) {
		a := args[i]
		if a == flag {
			if i+1 < len(args) {
				return args[i+1], 2, true
			}
			return "", 0, false // dangling flag with no value
		}
		if v, found := strings.CutPrefix(a, flag+"="); found {
			return v, 1, true
		}
		return "", 0, false
	}

	for i := 0; i < len(args); {
		matched := false
		for _, flag := range []string{"--vault-password-file", "--vault-pass-file"} {
			if v, n, ok := take(flag, i); ok {
				out.PasswordFile = v
				i += n
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		if v, n, ok := take("--vault-id", i); ok {
			out.VaultIDs = append(out.VaultIDs, v)
			i += n
			continue
		}
		if v, n, ok := take("--encrypt-vault-id", i); ok {
			out.EncryptID = v
			i += n
			continue
		}
		switch args[i] {
		case "--vault-id", "--encrypt-vault-id":
			return Args{}, fmt.Errorf("%s needs a value", args[i])
		case "--ask-vault-pass", "--ask-vault-password":
			out.AskPass = true
			i++
			continue
		}
		rest = append(rest, args[i])
		i++
	}

	filename, _, ok := config.SplitPlaybookArgsAmongOptions(rest)
	if !ok {
		return Args{}, errors.New("a target file is required")
	}
	out.Filename = filename

	if out.PasswordFile != "" && out.AskPass {
		return Args{}, errors.New("--vault-password-file and --ask-vault-pass can't be used together")
	}
	return out, nil
}

// secretSpec is one password to obtain: its vault id and where it comes
// from. Source is a file or script path, or promptSource.
type secretSpec struct {
	ID     string
	Source string
}

const (
	promptSource = "prompt"
	// askSource is --ask-vault-pass's own prompt, which keeps the plain
	// "Vault password: " wording (ansible's too).
	askSource = "prompt_ask_vault_pass"
)

// parseVaultID splits "label@source" the way ansible does; a bare source has
// the label "default".
func parseVaultID(s string) secretSpec {
	if id, src, ok := strings.Cut(s, "@"); ok {
		return secretSpec{ID: id, Source: src}
	}
	return secretSpec{ID: vaultcrypto.DefaultID, Source: s}
}

// passwordSources is what the environment and ansible.cfg contribute to the
// password options.
type passwordSources struct {
	IdentityList []string // ANSIBLE_VAULT_IDENTITY_LIST, else [defaults] vault_identity_list
	EncryptID    string   // ANSIBLE_VAULT_ENCRYPT_IDENTITY, else [defaults] vault_encrypt_identity
	EnvFile      string   // ANSIBLE_VAULT_PASSWORD_FILE
	CfgFile      string   // [defaults] vault_password_file
}

func readPasswordSources() passwordSources {
	return passwordSources{
		IdentityList: ansibleCfgIdentityList(),
		EncryptID:    ansibleCfgEncryptIdentity(),
		EnvFile:      os.Getenv("ANSIBLE_VAULT_PASSWORD_FILE"),
		CfgFile:      ansibleCfgVaultPasswordFile(),
	}
}

// secretSpecs is the pure decision behind the password list, in ansible's
// order: the configured identity list, then every --vault-id, then the "one
// password" of the older options with their own precedence (design-docs/
// Vault.md: --vault-password-file, --ask-vault-pass, $ANSIBLE_VAULT_
// PASSWORD_FILE, ansible.cfg's vault_password_file - the first set wins), as
// the vault id "default". When that yields nothing at all there is a prompt
// for the default password: a deliberate divergence from ansible-vault,
// which errors, because this verb is interactive by nature.
func secretSpecs(args Args, src passwordSources) []secretSpec {
	var specs []secretSpec
	for _, id := range src.IdentityList {
		specs = append(specs, parseVaultID(id))
	}
	for _, id := range args.VaultIDs {
		specs = append(specs, parseVaultID(id))
	}
	switch {
	case args.PasswordFile != "":
		specs = append(specs, secretSpec{vaultcrypto.DefaultID, args.PasswordFile})
	case args.AskPass:
		specs = append(specs, secretSpec{vaultcrypto.DefaultID, askSource})
	case src.EnvFile != "":
		specs = append(specs, secretSpec{vaultcrypto.DefaultID, src.EnvFile})
	case src.CfgFile != "":
		specs = append(specs, secretSpec{vaultcrypto.DefaultID, src.CfgFile})
	}
	if len(specs) == 0 {
		specs = append(specs, secretSpec{vaultcrypto.DefaultID, askSource})
	}
	return specs
}

// promptFunc asks for a password; ask is true for the plain --ask-vault-pass
// wording, otherwise the vault id is named.
type promptFunc func(id string, ask bool) (string, error)

// loadSecrets obtains every password of specs, in order.
func loadSecrets(specs []secretSpec, prompt promptFunc) ([]vaultcrypto.Secret, error) {
	secrets := make([]vaultcrypto.Secret, 0, len(specs))
	for _, spec := range specs {
		var password string
		var err error
		switch spec.Source {
		case promptSource, askSource:
			password, err = prompt(spec.ID, spec.Source == askSource)
		default:
			password, err = readPasswordSource(spec.ID, spec.Source)
		}
		if err != nil {
			return nil, err
		}
		secrets = append(secrets, vaultcrypto.Secret{ID: spec.ID, Password: password})
	}
	return secrets, nil
}

// readPasswordSource reads a password from a file, or from the output of an
// executable one (a script). A script whose name ends in "-client" (before
// any extension) gets "--vault-id <id>", as with ansible.
func readPasswordSource(id, path string) (string, error) {
	path = expandHome(path)
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("couldn't read vault password file %q: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("the vault password file %q is a directory", path)
	}
	if info.Mode()&0o111 == 0 {
		return readPasswordFile(path)
	}
	var cmdArgs []string
	if strings.HasSuffix(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), "-client") {
		cmdArgs = []string{"--vault-id", id}
	}
	cmd := exec.Command(path, cmdArgs...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("the vault password script %q failed: %w", path, err)
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

func readPasswordFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("couldn't read vault password file %q: %w", path, err)
	}
	// Matches ansible-vault's own tolerance of a trailing newline in a
	// password file - a password file is conventionally one line, and a
	// trailing newline from `echo password > file` shouldn't become part
	// of the actual password.
	return strings.TrimRight(string(data), "\r\n"), nil
}

func promptPassword(id string, ask bool) (string, error) {
	if ask {
		fmt.Fprint(os.Stderr, "Vault password: ")
	} else {
		fmt.Fprintf(os.Stderr, "Vault password (%s): ", id)
	}
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("couldn't read password: %w", err)
	}
	return string(b), nil
}

// chooseEncryptSecret picks the password a brand-new value is encrypted
// with. An explicit vault id (--encrypt-vault-id, else the configured
// vault_encrypt_identity) must name a password that was given. Without one
// ansible uses the first password; here, when every value already in the
// file is under one vault id and a password with that id was given, that is
// used instead, so a new secret in a "prod" file does not end up under
// whichever password happened to be listed first.
func chooseEncryptSecret(view vaultfile.DecryptedView, secrets []vaultcrypto.Secret, encryptID string) (*vaultcrypto.Secret, error) {
	if encryptID != "" {
		for i := range secrets {
			if secrets[i].ID == encryptID {
				return &secrets[i], nil
			}
		}
		return nil, fmt.Errorf("no password was given for the vault id %q (--encrypt-vault-id)", encryptID)
	}
	if len(secrets) == 0 {
		return nil, nil
	}
	ids := map[string]bool{}
	label := func(l string) string {
		if l == "" {
			return vaultcrypto.DefaultID
		}
		return l
	}
	for _, u := range view.UsedBefore {
		ids[label(u.Label)] = true
	}
	for _, l := range view.LockedBefore {
		ids[label(l)] = true
	}
	if len(ids) == 1 {
		for id := range ids {
			for i := range secrets {
				if secrets[i].ID == id {
					return &secrets[i], nil
				}
			}
		}
	}
	return &secrets[0], nil
}
