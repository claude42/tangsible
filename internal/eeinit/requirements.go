package eeinit

import (
	"regexp"

	"gopkg.in/yaml.v3"
)

// ReqCollection is one collection entry of a requirements.yml.
type ReqCollection struct {
	// Name is the entry's name (a "namespace.collection", or whatever else
	// the entry names, such as a git URL).
	Name    string
	Version string
	// Plain is true for an entry that is only a name and maybe a version -
	// one tangsible may complete with the host's version. Anything with a
	// source or type is kept exactly as written.
	Plain bool
	// Node is the entry as a mapping.
	Node *yaml.Node
}

// Requirements is a parsed requirements.yml.
type Requirements struct {
	Collections []ReqCollection
	// Roles are the role entries, as written.
	Roles []*yaml.Node
}

var collectionName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\.[A-Za-z_][A-Za-z0-9_]*$`)

// ParseRequirements reads a requirements.yml: the usual mapping with
// collections: and roles:, or the legacy plain list of roles. A scalar
// collection entry ("- community.general") becomes a name mapping.
func ParseRequirements(data []byte) (*Requirements, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	req := &Requirements{}
	if len(doc.Content) == 0 {
		return req, nil
	}
	root := doc.Content[0]
	switch root.Kind {
	case yaml.SequenceNode:
		req.Roles = root.Content
	case yaml.MappingNode:
		if seq := value(root, "roles"); seq != nil && seq.Kind == yaml.SequenceNode {
			req.Roles = seq.Content
		}
		if seq := value(root, "collections"); seq != nil && seq.Kind == yaml.SequenceNode {
			for _, e := range seq.Content {
				if c, ok := reqCollection(e); ok {
					req.Collections = append(req.Collections, c)
				}
			}
		}
	}
	return req, nil
}

func reqCollection(e *yaml.Node) (ReqCollection, bool) {
	if e.Kind == yaml.ScalarNode && e.Value != "" {
		m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "name"},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: e.Value},
		}}
		return ReqCollection{Name: e.Value, Plain: collectionName.MatchString(e.Value), Node: m}, true
	}
	if e.Kind != yaml.MappingNode {
		return ReqCollection{}, false
	}
	name := value(e, "name")
	if name == nil || name.Kind != yaml.ScalarNode {
		return ReqCollection{}, false
	}
	c := ReqCollection{Name: name.Value, Node: e, Plain: collectionName.MatchString(name.Value)}
	if v := value(e, "version"); v != nil {
		c.Version = v.Value
	}
	for i := 0; i+1 < len(e.Content); i += 2 {
		if k := e.Content[i].Value; k != "name" && k != "version" {
			c.Plain = false
		}
	}
	return c, true
}
