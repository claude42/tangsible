package ee

import "strings"

// argKind says how a flag's value refers to host paths.
type argKind int

const (
	kindPath      argKind = iota + 1 // the value is one path
	kindInventory                    // comma-separated paths and/or host patterns
	kindAtFile                       // "@path" is a file, anything else literal
	kindPathList                     // colon-separated paths
	kindVaultID                      // label@source, source being a path or "prompt"
)

var longFlags = map[string]argKind{
	"--private-key":              kindPath,
	"--key-file":                 kindPath,
	"--vault-password-file":      kindPath,
	"--vault-pass-file":          kindPath,
	"--become-password-file":     kindPath,
	"--become-pass-file":         kindPath,
	"--connection-password-file": kindPath,
	"--conn-pass-file":           kindPath,
	"--inventory":                kindInventory,
	"--inventory-file":           kindInventory,
	"--extra-vars":               kindAtFile,
	"--limit":                    kindAtFile,
	"--module-path":              kindPathList,
	"--vault-id":                 kindVaultID,
}

var shortFlags = map[byte]argKind{
	'i': kindInventory,
	'e': kindAtFile,
	'l': kindAtFile,
	'M': kindPathList,
}

// rewriteArgs makes the host paths in a tool's argument list reachable in
// the container (see mapper.translate; the arguments come back unchanged
// because paths are mounted where they are). Only flags known to take
// paths are looked at; any other argument that merely looks like an
// absolute host path gets a warning.
func rewriteArgs(args []string, m *mapper) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return append(out, args[i:]...)
		}
		switch {
		case strings.HasPrefix(a, "--"):
			name, val, hasVal := strings.Cut(a, "=")
			kind, known := longFlags[name]
			if !known {
				if hasVal {
					warnPathLike(m, val)
				}
				out = append(out, a)
				continue
			}
			if hasVal {
				out = append(out, name+"="+rewriteValue(kind, val, m))
			} else if i+1 < len(args) {
				i++
				out = append(out, name, rewriteValue(kind, args[i], m))
			} else {
				out = append(out, a)
			}
		case len(a) >= 2 && a[0] == '-':
			kind, known := shortFlags[a[1]]
			switch {
			case !known:
				out = append(out, a)
			case len(a) > 2:
				out = append(out, a[:2]+rewriteValue(kind, a[2:], m))
			case i+1 < len(args):
				i++
				out = append(out, a, rewriteValue(kind, args[i], m))
			default:
				out = append(out, a)
			}
		default:
			warnPathLike(m, a)
			out = append(out, a)
		}
	}
	return out
}

func warnPathLike(m *mapper, v string) {
	if strings.HasPrefix(v, "/") {
		m.plan.Warnings = append(m.plan.Warnings,
			"argument "+v+" looks like a host path but isn't a known path option; passed through unchanged")
	}
}

func rewriteValue(kind argKind, v string, m *mapper) string {
	switch kind {
	case kindPath:
		return m.translate(v)
	case kindInventory:
		return mapList(v, ",", m)
	case kindPathList:
		return mapList(v, ":", m)
	case kindAtFile:
		if rest, ok := strings.CutPrefix(v, "@"); ok {
			return "@" + m.translate(rest)
		}
		return v
	case kindVaultID:
		label, src := "", v
		if i := strings.Index(v, "@"); i >= 0 {
			label, src = v[:i+1], v[i+1:]
		}
		if src == "prompt" || strings.HasPrefix(src, "prompt_") {
			return v
		}
		return label + m.translateExisting(src)
	}
	return v
}

func mapList(v, sep string, m *mapper) string {
	parts := strings.Split(v, sep)
	for i, p := range parts {
		parts[i] = m.translateExisting(p)
	}
	return strings.Join(parts, sep)
}
