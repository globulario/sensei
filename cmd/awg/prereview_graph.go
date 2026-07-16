// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/prereview"
	"gopkg.in/yaml.v3"
)

// localGraphSource collects deterministic architectural context by reading the
// governed sources under docs/awareness/ directly — no gRPC server. It resolves
// the invariants, failure modes, forbidden fixes, and required tests applicable
// to the changed paths, and derives a risk class. Server-only surfaces
// (briefing, impact graph traversal) are reported as unavailable rather than
// approximated.
type localGraphSource struct {
	repoRoot string
}

// awProtects mirrors the `protects:` block of a governing node. Only the file
// lists are needed for path applicability.
type awProtects struct {
	Files           []string `yaml:"files"`
	EnforcesFiles   []string `yaml:"enforces_files"`
	ConfiguresFiles []string `yaml:"configures_files"`
	ObservesFiles   []string `yaml:"observes_files"`
	MayAffectFiles  []string `yaml:"may_affect_files"`
}

func (p awProtects) allFiles() []string {
	out := make([]string, 0, len(p.Files)+len(p.EnforcesFiles)+len(p.ConfiguresFiles)+len(p.ObservesFiles)+len(p.MayAffectFiles))
	out = append(out, p.Files...)
	out = append(out, p.EnforcesFiles...)
	out = append(out, p.ConfiguresFiles...)
	out = append(out, p.ObservesFiles...)
	out = append(out, p.MayAffectFiles...)
	return out
}

type awInvariant struct {
	ID       string     `yaml:"id"`
	Title    string     `yaml:"title"`
	Severity string     `yaml:"severity"`
	Status   string     `yaml:"status"`
	Protects awProtects `yaml:"protects"`
}

type awFailureMode struct {
	ID       string     `yaml:"id"`
	Title    string     `yaml:"title"`
	Severity string     `yaml:"severity"`
	Protects awProtects `yaml:"protects"`
}

type awDetect struct {
	AppliesToPaths []string `yaml:"applies_to_paths"`
}

type awForbiddenFix struct {
	ID                string     `yaml:"id"`
	Title             string     `yaml:"title"`
	Reason            string     `yaml:"reason"`
	RelatedInvariants []string   `yaml:"related_invariants"`
	Protects          awProtects `yaml:"protects"`
	Detect            awDetect   `yaml:"detect"`
}

func (f awForbiddenFix) paths() []string {
	return append(f.Protects.allFiles(), f.Detect.AppliesToPaths...)
}

type awRequiredTest struct {
	ID       string `yaml:"id"`
	Title    string `yaml:"title"`
	Protects struct {
		Files        []string `yaml:"files"`
		Invariants   []string `yaml:"invariants"`
		FailureModes []string `yaml:"failure_modes"`
	} `yaml:"protects"`
}

func (localGraphSource) collect(root string, req prereview.GraphRequest) (prereview.GraphContext, error) {
	docsDir := filepath.Join(root, "docs", "awareness")
	if _, err := os.Stat(docsDir); err != nil {
		return prereview.GraphContext{Degraded: []string{"docs/awareness not found; no governed sources available"}}, nil
	}
	changed := req.ChangedPaths
	gc := prereview.GraphContext{
		Unavailable: []string{"impact_graph_traversal", "briefing"},
	}
	var degraded []string
	available := []string{}

	var invFile struct {
		Invariants []awInvariant `yaml:"invariants"`
	}
	if loaded := loadAwareness(filepath.Join(docsDir, "invariants.yaml"), &invFile, &degraded); loaded {
		available = append(available, "invariants")
	}
	applicableInv := map[string]bool{}
	for _, inv := range invFile.Invariants {
		if strings.EqualFold(inv.Status, "deprecated") || strings.EqualFold(inv.Status, "retired") {
			continue
		}
		if pathsMatch(inv.Protects.allFiles(), changed) {
			applicableInv[inv.ID] = true
			gc.Invariants = append(gc.Invariants, protectionItem(inv.ID, inv.Title, inv.Severity, "invariants.yaml"))
		}
	}

	var fmFile struct {
		FailureModes []awFailureMode `yaml:"failure_modes"`
	}
	if loaded := loadAwareness(filepath.Join(docsDir, "failure_modes.yaml"), &fmFile, &degraded); loaded {
		available = append(available, "failure_modes")
	}
	for _, fm := range fmFile.FailureModes {
		if pathsMatch(fm.Protects.allFiles(), changed) {
			gc.FailureModes = append(gc.FailureModes, protectionItem(fm.ID, fm.Title, fm.Severity, "failure_modes.yaml"))
		}
	}

	var ffFile struct {
		ForbiddenFixes []awForbiddenFix `yaml:"forbidden_fixes"`
	}
	loadedFF := loadAwareness(filepath.Join(docsDir, "forbidden_fixes.yaml"), &ffFile, &degraded)
	var ffArch struct {
		ForbiddenFixes []awForbiddenFix `yaml:"forbidden_fixes"`
	}
	loadedFFArch := loadAwareness(filepath.Join(docsDir, "architecture", "forbidden_fixes.yaml"), &ffArch, &degraded)
	if loadedFF || loadedFFArch {
		available = append(available, "forbidden_fixes")
	}
	for _, ff := range append(ffFile.ForbiddenFixes, ffArch.ForbiddenFixes...) {
		applicable := pathsMatch(ff.paths(), changed)
		if !applicable {
			for _, ri := range ff.RelatedInvariants {
				if applicableInv[strings.TrimSpace(ri)] {
					applicable = true
					break
				}
			}
		}
		if !applicable {
			continue
		}
		gc.ForbiddenFixes = append(gc.ForbiddenFixes, prereview.ProtectionItem{
			ID: ff.ID, Title: ff.Title, Severity: prereview.SeverityHigh,
			Applicability: "covers a changed path", Status: "applicable",
			Epistemic: prereview.EpistemicGoverned, EvidenceRefs: []string{"awareness:forbidden_fixes.yaml#" + ff.ID},
		})
		related := matchedPaths(ff.paths(), changed)
		if len(related) == 0 {
			related = changed
		}
		gc.ReviewerConcerns = append(gc.ReviewerConcerns, prereview.ReviewerAttentionItem{
			ID:                 "concern.forbidden_fix." + ff.ID,
			Category:           prereview.AttentionArchitectQuestion,
			Question:           "Does this change reintroduce the forbidden fix: " + ff.Title + "?",
			WhyItMatters:       ff.Reason,
			Blocking:           false,
			Severity:           prereview.SeverityHigh,
			Epistemic:          prereview.EpistemicGoverned,
			EvidenceRefs:       []string{"awareness:forbidden_fixes.yaml#" + ff.ID},
			RelatedFiles:       related,
			ResolutionPath:     "consult the file briefing and confirm the forbidden shape is not present",
			ArchitecturalReach: 2,
		})
	}

	var rtFile struct {
		RequiredTests []awRequiredTest `yaml:"required_tests"`
	}
	if loaded := loadAwareness(filepath.Join(docsDir, "required_tests.yaml"), &rtFile, &degraded); loaded {
		available = append(available, "required_tests")
	}
	for _, rt := range rtFile.RequiredTests {
		applicable := pathsMatch(rt.Protects.Files, changed)
		if !applicable {
			for _, ri := range rt.Protects.Invariants {
				if applicableInv[strings.TrimSpace(ri)] {
					applicable = true
					break
				}
			}
		}
		if applicable {
			gc.RequiredTests = append(gc.RequiredTests, prereview.ProtectionItem{
				ID: rt.ID, Title: rt.Title, Severity: prereview.SeverityMedium,
				Applicability: "guards a changed path", Status: "required",
				Epistemic: prereview.EpistemicGoverned, EvidenceRefs: []string{"awareness:required_tests.yaml#" + rt.ID},
			})
		}
	}

	if len(gc.Invariants)+len(gc.FailureModes)+len(gc.ForbiddenFixes) > 0 {
		gc.RiskClass = "architecture_sensitive"
	} else {
		gc.RiskClass = "standard"
	}
	gc.Available = available
	gc.Degraded = degraded
	return gc, nil
}

func (g localGraphSource) CollectArchitecturalContext(_ context.Context, req prereview.GraphRequest) (prereview.GraphContext, error) {
	root := strings.TrimSpace(g.repoRoot)
	if root == "" {
		root = "."
	}
	return g.collect(root, req)
}

// loadAwareness reads and decodes a governing YAML file into out. A missing file
// is not an error (the source is simply absent); a malformed file is recorded as
// degraded. It returns whether the file was present and decoded.
func loadAwareness(path string, out any, degraded *[]string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if err := yaml.Unmarshal(raw, out); err != nil {
		*degraded = append(*degraded, fmt.Sprintf("%s: malformed (%v)", filepath.Base(path), err))
		return false
	}
	return true
}

func protectionItem(id, title, severity, sourceFile string) prereview.ProtectionItem {
	return prereview.ProtectionItem{
		ID: id, Title: title, Severity: mapSeverity(severity),
		Applicability: "covers a changed path", Status: "applicable",
		Epistemic: prereview.EpistemicGoverned, EvidenceRefs: []string{"awareness:" + sourceFile + "#" + id},
	}
}

func mapSeverity(s string) prereview.Severity {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical":
		return prereview.SeverityCritical
	case "high":
		return prereview.SeverityHigh
	case "medium":
		return prereview.SeverityMedium
	case "low":
		return prereview.SeverityLow
	case "info", "informational":
		return prereview.SeverityInfo
	default:
		return prereview.SeverityMedium
	}
}

// pathsMatch reports whether any governed file entry covers any changed path. A
// directory entry (trailing slash, or a prefix directory) covers everything
// beneath it; a file entry matches exactly.
func pathsMatch(nodeFiles, changed []string) bool {
	for _, nf := range nodeFiles {
		nf = strings.TrimSpace(nf)
		if nf == "" {
			continue
		}
		for _, cp := range changed {
			if coversPath(nf, cp) {
				return true
			}
		}
	}
	return false
}

// matchedPaths returns the changed paths that any governed file entry covers.
func matchedPaths(nodeFiles, changed []string) []string {
	var out []string
	for _, cp := range changed {
		for _, nf := range nodeFiles {
			if coversPath(strings.TrimSpace(nf), cp) {
				out = append(out, cp)
				break
			}
		}
	}
	return out
}

func coversPath(nodeFile, changed string) bool {
	if nodeFile == changed {
		return true
	}
	if strings.HasSuffix(nodeFile, "/") {
		return strings.HasPrefix(changed, nodeFile)
	}
	// A bare directory entry (no extension) covers everything beneath it.
	if filepath.Ext(nodeFile) == "" {
		return strings.HasPrefix(changed, nodeFile+"/")
	}
	return false
}
