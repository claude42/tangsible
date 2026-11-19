package eepush

import (
	"fmt"
	"strings"

	"code.aw.net/claude/tangsible/internal/eebuild"
)

// Usage is the synopsis printed with a usage error.
const Usage = "usage: tangsible ee push [-f file] [-y | --yes]"

// Options is the parsed command line of `tangsible ee push`.
type Options struct {
	File string
	// Yes skips the confirmation.
	Yes bool
}

// ParseArgs reads the arguments after "push": -f/--file and -y/--yes.
func ParseArgs(args []string) (Options, error) {
	o := Options{File: eebuild.DefaultFile}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-f" || a == "--file":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return o, fmt.Errorf("%s needs a value", a)
			}
			i++
			o.File = args[i]
		case strings.HasPrefix(a, "--file="):
			if o.File = strings.TrimPrefix(a, "--file="); o.File == "" {
				return o, fmt.Errorf("--file needs a value")
			}
		case a == "-y" || a == "--yes":
			o.Yes = true
		case strings.HasPrefix(a, "-"):
			return o, fmt.Errorf("unknown option %s", a)
		default:
			return o, fmt.Errorf("unexpected argument %s", a)
		}
	}
	return o, nil
}
