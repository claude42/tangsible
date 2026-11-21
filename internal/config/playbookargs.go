package config

import "strings"

// ansiblePlaybookValueFlags are the ansible-playbook options that take one
// value, both spellings of each (design-docs/ImplicitRun.md, "Options before
// the playbook"). Every other flag is treated as a switch, including one not
// listed here. TestAnsiblePlaybookFlagTable compares this with the installed
// `ansible-playbook --help`.
var ansiblePlaybookValueFlags = map[string]bool{
	"--become-password-file": true, "--become-pass-file": true,
	"--connection-password-file": true, "--conn-pass-file": true,
	"--skip-tags": true, "--start-at-task": true, "--vault-id": true,
	"--vault-password-file": true, "--vault-pass-file": true,
	"--module-path": true, "--extra-vars": true, "--forks": true,
	"--inventory": true, "--inventory-file": true, "--limit": true,
	"--tags": true, "--private-key": true, "--key-file": true,
	"--scp-extra-args": true, "--sftp-extra-args": true,
	"--ssh-common-args": true, "--ssh-extra-args": true,
	"--timeout": true, "--connection": true, "--user": true,
	"--become-method": true, "--become-user": true,
	// Tangsible's own synthetic flags that take a value.
	"--ee": true, "--start-at-play": true,
}

// ansiblePlaybookValueShorts are the short spellings of the value-taking
// flags above (-M -e -f -i -l -t -T -c -u).
const ansiblePlaybookValueShorts = "MefiltTcu"

// SplitPlaybookArgsAmongOptions is SplitPlaybookArgs for the verbs that
// spawn ansible-playbook (run, rerun, role, debug): the playbook is the
// first argument that is neither a flag nor a flag's value, so options may
// come before it (`--ee img site.yml`, `-i hosts site.yml`). "--" ends the
// options and is dropped. rest keeps every other argument in the typed
// order, minus the playbook. No such argument means no playbook given:
// explicit is false and rest is args (minus a trailing "--").
func SplitPlaybookArgsAmongOptions(args []string) (playbook string, rest []string, explicit bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			if i+1 >= len(args) {
				return "", args[:i:i], false
			}
			return args[i+1], join(args[:i], args[i+2:]), true
		case strings.HasPrefix(a, "--"):
			if !strings.Contains(a, "=") && ansiblePlaybookValueFlags[a] {
				i++
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			if shortClusterTakesNext(a[1:]) {
				i++
			}
		default:
			return a, join(args[:i], args[i+1:]), true
		}
	}
	return "", args, false
}

// shortClusterTakesNext reports whether the letters after a single '-' end in
// a value-taking flag with nothing after it, so the next argument is its
// value. Letters after a value-taking one are that value (-ifile).
func shortClusterTakesNext(letters string) bool {
	for i := 0; i < len(letters); i++ {
		if strings.IndexByte(ansiblePlaybookValueShorts, letters[i]) >= 0 {
			return i == len(letters)-1
		}
	}
	return false
}

func join(a, b []string) []string {
	if len(a)+len(b) == 0 {
		return nil
	}
	out := make([]string, 0, len(a)+len(b))
	return append(append(out, a...), b...)
}
