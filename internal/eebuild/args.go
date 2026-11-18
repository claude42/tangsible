package eebuild

import (
	"errors"
	"fmt"
	"strings"
)

// Usage is the synopsis printed with a usage error.
const Usage = "usage: tangsible ee build [-f file] [-t tag ...] [--no-cache] [--show-output]"

// DefaultFile is the definition built when -f is not given.
const DefaultFile = "execution-environment.yml"

// Options is the parsed command line of `tangsible ee build`.
type Options struct {
	File string
	// Tags are the image names; they replace the definition's options.tags.
	Tags    []string
	NoCache bool
	// ShowOutput streams the build's output instead of the status line.
	ShowOutput bool
}

// ParseArgs reads the arguments after "build": -f/--file, -t/--tag
// (repeatable), --no-cache and --show-output.
func ParseArgs(args []string) (Options, error) {
	o := Options{File: DefaultFile}
	value := func(i *int, name string) (string, error) {
		if *i+1 >= len(args) || strings.HasPrefix(args[*i+1], "-") {
			return "", fmt.Errorf("%s needs a value", name)
		}
		*i++
		return args[*i], nil
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-f" || a == "--file":
			v, err := value(&i, a)
			if err != nil {
				return o, err
			}
			o.File = v
		case strings.HasPrefix(a, "--file="):
			if o.File = strings.TrimPrefix(a, "--file="); o.File == "" {
				return o, errors.New("--file needs a value")
			}
		case a == "-t" || a == "--tag":
			v, err := value(&i, a)
			if err != nil {
				return o, err
			}
			o.Tags = append(o.Tags, v)
		case strings.HasPrefix(a, "--tag="):
			v := strings.TrimPrefix(a, "--tag=")
			if v == "" {
				return o, errors.New("--tag needs a value")
			}
			o.Tags = append(o.Tags, v)
		case a == "--no-cache":
			o.NoCache = true
		case a == "--show-output":
			o.ShowOutput = true
		case strings.HasPrefix(a, "-"):
			return o, fmt.Errorf("unknown option %s", a)
		default:
			return o, fmt.Errorf("unexpected argument %s", a)
		}
	}
	return o, nil
}
