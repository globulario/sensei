// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// parseCandidateEntries accepts both candidate document shapes used by
// Sensei producers:
//
//	candidates: [...]                         (direct canonical list)
//	generator_name: { candidates: [...] }     (metadata/envelope wrapper)
//
// Mixing both shapes in one document is rejected because silently merging two
// authorities would make candidate identity depend on map traversal and author
// intent ambiguous.
func parseCandidateEntries(raw []byte) ([]candidateEntry, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("candidate document must be a mapping")
	}

	var direct []candidateEntry
	var wrapped []candidateEntry
	directSeen, wrappedSeen := false, false
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		if key.Value == "candidates" {
			directSeen = true
			if err := value.Decode(&direct); err != nil {
				return nil, fmt.Errorf("decode direct candidates: %w", err)
			}
			continue
		}
		if value.Kind != yaml.MappingNode || !yamlMappingHasKey(value, "candidates") {
			continue
		}
		wrappedSeen = true
		var section struct {
			Candidates []candidateEntry `yaml:"candidates"`
		}
		if err := value.Decode(&section); err != nil {
			return nil, fmt.Errorf("decode candidate wrapper %q: %w", key.Value, err)
		}
		wrapped = append(wrapped, section.Candidates...)
	}
	if directSeen && wrappedSeen {
		return nil, fmt.Errorf("candidate document mixes direct and wrapped candidates")
	}
	if directSeen {
		return direct, nil
	}
	return wrapped, nil
}

func yamlMappingHasKey(node *yaml.Node, wanted string) bool {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == wanted {
			return true
		}
	}
	return false
}
