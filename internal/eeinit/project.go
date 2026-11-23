package eeinit

import (
	"os"
	"path/filepath"

	"code.aw.net/claude/tangsible/internal/source"
)

// Project is everything the scan finds a playbook (and the roles and
// templates it reaches) uses; `ee init` drafts from it and `ee verify` checks
// the built image against it.
type Project struct {
	// Abs is the playbook's absolute path.
	Abs      string
	Uses     []ModuleUse
	Todos    []Todo
	Commands []Command
	Lookups  []LookupUse
	// TemplatedLookups counts lookup() calls whose plugin name isn't a literal.
	TemplatedLookups int
	// Roles are the roles that came from collections.
	Roles []RoleUse
}

// ScanProject scans playbook: every YAML file it can reach (ReachableYAML)
// and the Jinja templates under its directory. Locations are shown relative
// to cwd where they lie under it. The definition and requirements files are
// not part of the project. Unreadable files are skipped.
func ScanProject(playbook, cwd string) (*Project, error) {
	abs, err := filepath.Abs(playbook)
	if err != nil {
		return nil, err
	}
	p := &Project{Abs: abs}
	seenRole := map[RoleUse]bool{}
	for _, f := range source.ReachableYAML(abs) {
		name := filepath.Base(f.Path)
		if name == "requirements.yml" || name == "requirements.yaml" || name == OutputFile {
			continue
		}
		data, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		var own []string
		if f.Collection != "" {
			own = []string{f.Collection}
			if r := (RoleUse{f.Role, f.Collection}); !seenRole[r] {
				seenRole[r] = true
				p.Roles = append(p.Roles, r)
			}
		}
		scan := ScanYAML(displayPath(f.Path, cwd), data, own)
		p.Uses = append(p.Uses, scan.Uses...)
		p.Todos = append(p.Todos, scan.Todos...)
		p.Commands = append(p.Commands, scan.Commands...)
		lu, lt := ScanLookups(displayPath(f.Path, cwd), data, own)
		p.Lookups = append(p.Lookups, lu...)
		p.TemplatedLookups += lt
	}
	for _, path := range TemplateFiles(filepath.Dir(abs)) {
		if data, err := os.ReadFile(path); err == nil {
			lu, lt := ScanTemplate(displayPath(path, cwd), string(data))
			p.Lookups = append(p.Lookups, lu...)
			p.TemplatedLookups += lt
		}
	}
	// A pipe lookup runs its command in a shell on the controller, like a
	// shell task there.
	for _, u := range p.Lookups {
		for _, c := range u.Cmds {
			p.Commands = append(p.Commands, Command{Loc: u.Loc, Shell: true, Text: c})
		}
	}
	return p, nil
}
