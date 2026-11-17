package ee

import (
	"fmt"
	"strings"
)

// ownedFlags are `podman run` flags the builder sets itself, or whose use
// would break how tangsible talks to the container (its stdin/stdout,
// the event FIFO, the mapped user). Each maps to what to use instead.
var ownedFlags = map[string]string{
	"--userns":       "tangsible runs the container as your user (keep-id)",
	"--user":         "tangsible runs the container as your user (keep-id)",
	"--passwd-entry": "tangsible sets the passwd entry and the home directory",
	"--volume":       "use extra_mounts",
	"--mount":        "use extra_mounts",
	"--workdir":      "the working directory is the project's",
	"--name":         "tangsible names the container to be able to stop it",
	"--network":      "use the network key",
	"--net":          "use the network key",
	"--env":          "use pass_env or set_env (values stay off the command line)",
	"--entrypoint":   "tangsible runs ansible-playbook itself",
	"--interactive":  "tangsible owns the container's stdin",
	"--tty":          "the output is streamed, a terminal would corrupt it",
	"--detach":       "tangsible waits for the container",
	"--attach":       "tangsible owns the container's output",
	"--security-opt": "", // only a label= option is refused, see CheckContainerOptions
	"--tmpfs":        "", // only the home directory is refused
}

// shortOwned are the one-letter forms of the above: a flag that takes a value
// (the value may be attached, -v/a:/b) and the bundled switches -i, -t, -d.
var shortOwned = map[byte]string{
	'v': "--volume", 'w': "--workdir", 'e': "--env", 'u': "--user", 'a': "--attach",
	'i': "--interactive", 't': "--tty", 'd': "--detach",
}

// CheckContainerOptions refuses container_options entries that would fight
// the builder's own flags, naming the setting to use instead. The rest go to
// `podman run` unchanged, so anything else podman accepts (capabilities,
// devices, resource limits, --env-file, ...) works.
func CheckContainerOptions(opts []string) error {
	for i, o := range opts {
		if !strings.HasPrefix(o, "-") || o == "-" || o == "--" {
			continue
		}
		name, val, hasVal := strings.Cut(o, "=")
		if !strings.HasPrefix(o, "--") {
			if long, ok := shortOwned[o[1]]; ok {
				return optionError(o, long)
			}
			// A bundle of switches such as -it.
			if len(o) > 2 && isLetters(o[1:]) {
				for j := 2; j < len(o); j++ {
					if long, ok := shortOwned[o[j]]; ok && strings.IndexByte("itd", o[j]) >= 0 {
						return optionError(o, long)
					}
				}
			}
			continue
		}
		_, owned := ownedFlags[name]
		if !owned {
			continue
		}
		if !hasVal && i+1 < len(opts) {
			val = opts[i+1]
		}
		switch name {
		case "--security-opt":
			if strings.HasPrefix(val, "label") {
				return fmt.Errorf("container_options: %s %s: SELinux labels are set by the selinux key", name, val)
			}
		case "--tmpfs":
			if p, _, _ := strings.Cut(val, ":"); p == ContainerHome {
				return fmt.Errorf("container_options: %s %s: %s is tangsible's home directory", name, val, ContainerHome)
			}
		default:
			return optionError(o, name)
		}
	}
	return nil
}

func optionError(entry, long string) error {
	return fmt.Errorf("container_options: %s is not allowed: %s", entry, ownedFlags[long])
}

func isLetters(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}
