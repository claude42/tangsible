package eebuild

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Tags returns the definition's options.tags (none when there is no options
// section or no tags). A tags value that is not a list of strings is an error:
// ansible-builder would reject it too, later and with a less pointed message.
func Tags(definition []byte) ([]string, error) {
	var doc struct {
		Options struct {
			Tags any `yaml:"tags"`
		} `yaml:"options"`
	}
	if err := yaml.Unmarshal(definition, &doc); err != nil {
		return nil, err
	}
	switch v := doc.Options.Tags.(type) {
	case nil:
		return nil, nil
	case []any:
		tags := make([]string, 0, len(v))
		for i, e := range v {
			s, ok := e.(string)
			if !ok || s == "" {
				return nil, fmt.Errorf("options.tags: entry %d is not a name", i+1)
			}
			tags = append(tags, s)
		}
		return tags, nil
	default:
		return nil, fmt.Errorf("options.tags must be a list of names")
	}
}
