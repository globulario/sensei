// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
	"fmt"
	"os"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// RelationTargetsFile is the authored declaration that permits governed
// relations to name non-repository targets. Absence means no external target
// is trusted: absolute paths and sibling-repository references remain
// malformed until the repository explicitly declares their domain.
const RelationTargetsFile = AwarenessDir + "/relation_targets.yaml"

type relationTargetsDocument struct {
	RelationTargets struct {
		RuntimeRoots        []string `yaml:"runtime_roots"`
		SiblingRepositories []string `yaml:"sibling_repositories"`
	} `yaml:"relation_targets"`
}

type relationTargetPolicy struct {
	runtimeRoots map[string]bool
	siblings     map[string]bool
}

type relationTargetDisposition uint8

const (
	relationTargetInvalid relationTargetDisposition = iota
	relationTargetLocal
	relationTargetExternal
)

func loadRelationTargetPolicy(repoRoot string) (relationTargetPolicy, []string) {
	policy := relationTargetPolicy{
		runtimeRoots: map[string]bool{},
		siblings:     map[string]bool{},
	}
	filePath := joinRepo(repoRoot, RelationTargetsFile)
	raw, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return policy, nil
		}
		return policy, []string{fmt.Sprintf("%s: unreadable: %v", RelationTargetsFile, err)}
	}

	var doc relationTargetsDocument
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return policy, []string{fmt.Sprintf("%s: %v", RelationTargetsFile, err)}
	}

	var malformed []string
	for _, declared := range doc.RelationTargets.RuntimeRoots {
		root := filepathToSlash(strings.TrimSpace(declared))
		if root == "" || !strings.HasPrefix(root, "/") || strings.HasPrefix(root, "//") {
			malformed = append(malformed, fmt.Sprintf("%s: runtime root %q must be an absolute POSIX path", RelationTargetsFile, declared))
			continue
		}
		clean := path.Clean(root)
		if clean == "/" {
			malformed = append(malformed, fmt.Sprintf("%s: runtime root %q is too broad", RelationTargetsFile, declared))
			continue
		}
		policy.runtimeRoots[clean] = true
	}
	for _, declared := range doc.RelationTargets.SiblingRepositories {
		repo := strings.TrimSpace(declared)
		if !validSiblingRepositoryName(repo) {
			malformed = append(malformed, fmt.Sprintf("%s: sibling repository %q must be one path segment", RelationTargetsFile, declared))
			continue
		}
		policy.siblings[repo] = true
	}
	return policy, malformed
}

func validSiblingRepositoryName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

// classify keeps local protection semantics strict while recognizing two
// explicitly governed external domains:
//   - absolute runtime paths under a declared runtime root;
//   - ../<declared-sibling>/... references.
//
// External targets are valid documentation/traceability anchors, but they do
// not become local ProtectedPath entries because this checkout cannot govern
// edits outside its repository boundary.
func (p relationTargetPolicy) classify(target string) (string, relationTargetDisposition) {
	raw := strings.TrimSpace(target)
	if norm, ok := NormalizePath(raw); ok {
		return norm, relationTargetLocal
	}

	slash := filepathToSlash(raw)
	if strings.HasPrefix(slash, "/") && !strings.HasPrefix(slash, "//") {
		clean := path.Clean(slash)
		for root := range p.runtimeRoots {
			if clean == root || strings.HasPrefix(clean, root+"/") {
				return "", relationTargetExternal
			}
		}
		return "", relationTargetInvalid
	}

	if strings.HasPrefix(slash, "../") {
		rest := strings.TrimPrefix(slash, "../")
		clean := path.Clean(rest)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
			return "", relationTargetInvalid
		}
		repo := strings.SplitN(clean, "/", 2)[0]
		if p.siblings[repo] {
			return "", relationTargetExternal
		}
	}
	return "", relationTargetInvalid
}
