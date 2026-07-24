// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
	"os"

	"gopkg.in/yaml.v3"
)

// relationProtects mirrors the `protects:` block on an authored invariant.
// Only the direct, file-anchored roles are consulted (contract §3.3: "do not
// perform unbounded transitive graph expansion... only explicit direct
// relationships"). may_affect_files is deliberately excluded here exactly as
// it is excluded from the graph's reverse aw:implements edge upstream
// (golang/extractor/yaml_import.go) — it is the weakest, non-direct
// connection and must not manufacture protection.
type relationProtects struct {
	Files           []string `yaml:"files"`
	EnforcesFiles   []string `yaml:"enforces_files"`
	ConfiguresFiles []string `yaml:"configures_files"`
	ObservesFiles   []string `yaml:"observes_files"`
}

type relationInvariant struct {
	ID            string           `yaml:"id"`
	Protects      relationProtects `yaml:"protects"`
	RequiredTests []string         `yaml:"required_tests"`
}

type relationInvariantsFile struct {
	Invariants []relationInvariant `yaml:"invariants"`
}

type relationFailureModeProtects struct {
	Files []string `yaml:"files"`
}

type relationFailureMode struct {
	ID            string                      `yaml:"id"`
	Protects      relationFailureModeProtects `yaml:"protects"`
	RequiredTests []string                    `yaml:"required_tests"`
}

type relationFailureModesFile struct {
	FailureModes []relationFailureMode `yaml:"failure_modes"`
}

// GovernedRelationReasons scans every authored governed source under
// docs/awareness/ (via GovernedSourceFiles) for direct file-anchored
// relationships — protects/enforces/configures/observes and required tests —
// and returns the resulting reasons keyed by normalized protected path.
//
// This reads authored YAML directly: it requires no running graph/MCP
// service, satisfying the offline-capability requirement (contract §5).
func GovernedRelationReasons(repoRoot string) (map[string][]ProtectionReason, error) {
	files, err := GovernedSourceFiles(repoRoot)
	if err != nil {
		return nil, err
	}
	out := map[string][]ProtectionReason{}
	add := func(target, kind, source, knowledgeRef string) {
		norm, ok := NormalizePath(target)
		if !ok {
			return
		}
		out[norm] = append(out[norm], ProtectionReason{
			Origin:       OriginGovernedRelation,
			Kind:         kind,
			Source:       source,
			KnowledgeRef: knowledgeRef,
		})
	}
	for _, f := range files {
		raw, readErr := os.ReadFile(joinRepo(repoRoot, f))
		if readErr != nil {
			continue // GovernedSourceFiles just listed it; a race/removal here is not fatal.
		}
		var inv relationInvariantsFile
		if yaml.Unmarshal(raw, &inv) == nil {
			for _, i := range inv.Invariants {
				for _, target := range i.Protects.Files {
					add(target, "protects.files", f, i.ID)
				}
				for _, target := range i.Protects.EnforcesFiles {
					add(target, "protects.enforces_files", f, i.ID)
				}
				for _, target := range i.Protects.ConfiguresFiles {
					add(target, "protects.configures_files", f, i.ID)
				}
				for _, target := range i.Protects.ObservesFiles {
					add(target, "protects.observes_files", f, i.ID)
				}
				for _, testID := range i.RequiredTests {
					add(splitTestID(testID), "required_test", f, i.ID)
				}
			}
		}
		var fm relationFailureModesFile
		if yaml.Unmarshal(raw, &fm) == nil {
			for _, m := range fm.FailureModes {
				for _, target := range m.Protects.Files {
					add(target, "protects.files", f, m.ID)
				}
				for _, testID := range m.RequiredTests {
					add(splitTestID(testID), "required_test", f, m.ID)
				}
			}
		}
	}
	return out, nil
}
