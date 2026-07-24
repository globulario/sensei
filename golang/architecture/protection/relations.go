// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
//
// malformed lists every file that could not be read, or that failed to
// parse as EITHER an invariants.yaml or failure_modes.yaml shape (a real
// YAML syntax error, not merely "this file doesn't declare that top-level
// key"). Per contract §6 correction, a non-empty malformed list must never
// be silently absorbed into a clean-looking result — callers MUST treat it
// as a gap forcing at least PARTIAL coverage.
func GovernedRelationReasons(repoRoot string) (reasons map[string][]ProtectionReason, malformed []string, err error) {
	files, err := GovernedSourceFiles(repoRoot)
	if err != nil {
		return nil, nil, err
	}
	out := map[string][]ProtectionReason{}
	add := func(target, kind, source, knowledgeRef string) {
		norm, ok := NormalizePath(target)
		if !ok {
			// contract §4/§6 correction: an invalid governed-relation
			// target (empty, or escaping the repository) must never be
			// silently dropped — it is a gap forcing at least PARTIAL
			// coverage, not a clean no-op.
			malformed = append(malformed, fmt.Sprintf("%s: invalid target %q for %s (id=%s)", source, target, kind, knowledgeRef))
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
		// Governed sources include non-YAML authored files (design docs,
		// generated baselines) that are unconditionally protected as
		// governed_source but were never meant to be parsed as
		// invariants.yaml/failure_modes.yaml — attempting to would report
		// every such file as "malformed YAML" when it simply isn't YAML.
		ext := strings.ToLower(filepath.Ext(f))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		raw, readErr := os.ReadFile(joinRepo(repoRoot, f))
		if readErr != nil {
			malformed = append(malformed, fmt.Sprintf("%s: unreadable: %v", f, readErr))
			continue
		}
		var inv relationInvariantsFile
		invErr := yaml.Unmarshal(raw, &inv)
		if invErr == nil {
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
		fmErr := yaml.Unmarshal(raw, &fm)
		if fmErr == nil {
			for _, m := range fm.FailureModes {
				for _, target := range m.Protects.Files {
					add(target, "protects.files", f, m.ID)
				}
				for _, testID := range m.RequiredTests {
					add(splitTestID(testID), "required_test", f, m.ID)
				}
			}
		}
		// A genuine YAML syntax error fails BOTH lenient unmarshal attempts —
		// a file that simply doesn't declare `invariants:`/`failure_modes:`
		// (the normal case for most governed sources) fails neither, since
		// unmatched top-level keys are not an error under yaml.v3.
		if invErr != nil && fmErr != nil {
			malformed = append(malformed, fmt.Sprintf("%s: %v", f, invErr))
		}
	}
	return out, malformed, nil
}
