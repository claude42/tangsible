// Copyright 2026 Klaus Wissmann
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package uikit

import (
	"bytes"
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// RenderVariablesYAML renders a JSON object of variables as block-style YAML
// sorted by name, each top-level key followed by a comment naming where its
// value was defined (labels, from origin.Labels). It is the body of the host
// verb's Variables tab and of the Variables tab in debug mode's drill-down.
// note is printed above the YAML; empty is returned after it when there are
// no variables at all. Keys are highlighted like every other YAML tab.
func RenderVariablesYAML(vars json.RawMessage, labels map[string]string, note, empty string) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(vars))
	dec.UseNumber()
	var parsed map[string]interface{}
	if err := dec.Decode(&parsed); err != nil {
		return "", err
	}
	if len(parsed) == 0 {
		return note + empty, nil
	}
	sorted, err := json.Marshal(parsed) // sorts every level's keys
	if err != nil {
		return "", err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(sorted, &doc); err != nil || len(doc.Content) == 0 {
		return "", fmt.Errorf("could not render the variables")
	}
	root := doc.Content[0]
	ClearYAMLStyle(root)
	for i := 0; i+1 < len(root.Content); i += 2 {
		if label := labels[root.Content[i].Value]; label != "" {
			root.Content[i].LineComment = label
		}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return "", err
	}
	_ = enc.Close()
	return note + HighlightYAMLKeys(buf.String()), nil
}

// ClearYAMLStyle drops the flow/quoted styles a node parsed from JSON
// carries, so the encoder falls back to block style and quotes only where a
// string needs it.
func ClearYAMLStyle(n *yaml.Node) {
	n.Style = 0
	for _, c := range n.Content {
		ClearYAMLStyle(c)
	}
}
