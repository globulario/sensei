// SPDX-License-Identifier: Apache-2.0

package skillimport

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type implementationPatternYAML struct {
	ID                 string              `yaml:"id"`
	Class              string              `yaml:"class"`
	Label              string              `yaml:"label"`
	Status             string              `yaml:"status"`
	Confidence         string              `yaml:"confidence,omitempty"`
	WhenToUse          []string            `yaml:"when_to_use,omitempty"`
	ReferenceFiles     []referenceFileYAML `yaml:"reference_files,omitempty"`
	MustFollow         []string            `yaml:"must_follow,omitempty"`
	ForbiddenShortcuts []string            `yaml:"forbidden_shortcuts,omitempty"`
	Rationale          string              `yaml:"rationale,omitempty"`
	SourceFiles        []string            `yaml:"source_files,omitempty"`
}

type referenceFileYAML struct {
	Path string `yaml:"path"`
	Role string `yaml:"role"`
}

func RenderCandidate(candidate SkillCandidate) ([]byte, error) {
	candidate.Status = defaultStatus
	doc := implementationPatternYAML{
		ID:                 candidate.ID,
		Class:              string(CandidateImplementationPattern),
		Label:              candidate.Label,
		Status:             defaultStatus,
		Confidence:         confidenceOrDefault(candidate.Confidence),
		WhenToUse:          candidate.WhenToUse,
		MustFollow:         candidate.MustFollow,
		ForbiddenShortcuts: candidate.ForbiddenShortcuts,
		Rationale:          candidate.Rationale,
		SourceFiles:        sourceFiles(candidate),
	}
	for _, ref := range candidate.ReferenceFiles {
		if strings.TrimSpace(ref.Path) == "" {
			continue
		}
		role := strings.TrimSpace(ref.Role)
		if role == "" {
			role = "reference"
		}
		doc.ReferenceFiles = append(doc.ReferenceFiles, referenceFileYAML{
			Path: strings.TrimSpace(ref.Path),
			Role: role,
		})
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func WriteCandidates(candidates []SkillCandidate, opts ImportOptions) (WriteResult, error) {
	var result WriteResult
	if strings.TrimSpace(opts.OutputDir) == "" {
		return result, errors.New("output directory is required")
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return result, err
	}

	sorted := append([]SkillCandidate(nil), candidates...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ID < sorted[j].ID
	})

	for _, candidate := range sorted {
		data, err := RenderCandidate(candidate)
		if err != nil {
			return result, err
		}
		path := filepath.Join(opts.OutputDir, CandidateFileName(candidate))
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return result, err
		}
		result.Paths = append(result.Paths, path)
	}
	if err := ValidateGeneratedCandidates(result.Paths); err != nil {
		return result, err
	}
	return result, nil
}

func CandidateFileName(candidate SkillCandidate) string {
	name := strings.ReplaceAll(candidate.ID, ".", "_")
	name = slug(name)
	if name == "" {
		name = "imported_skill"
	}
	return name + ".yaml"
}

func ValidateGeneratedCandidates(paths []string) error {
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("%s: read: %w", path, err)
		}
		if err := ValidateCandidateYAML(data); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return nil
}

func ValidateCandidateYAML(data []byte) error {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("yaml parse: %w", err)
	}
	if strings.TrimSpace(asString(raw["id"])) == "" {
		return errors.New("id is required")
	}
	if asString(raw["class"]) != string(CandidateImplementationPattern) {
		return fmt.Errorf("class must be %s", CandidateImplementationPattern)
	}
	if asString(raw["status"]) != defaultStatus {
		return errors.New("status must be candidate")
	}
	if !hasNonEmptyList(raw["must_follow"]) &&
		!hasNonEmptyList(raw["when_to_use"]) &&
		!hasNonEmptyList(raw["forbidden_shortcuts"]) {
		return errors.New("at least one of must_follow, when_to_use, or forbidden_shortcuts is required")
	}
	if !referenceFilesContainSkill(raw["reference_files"]) {
		return errors.New("reference_files must contain the source SKILL.md")
	}
	return nil
}

func sourceFiles(candidate SkillCandidate) []string {
	if strings.TrimSpace(candidate.SourcePath) != "" {
		return []string{strings.TrimSpace(candidate.SourcePath)}
	}
	for _, ref := range candidate.ReferenceFiles {
		if strings.TrimSpace(ref.Path) != "" {
			return []string{strings.TrimSpace(ref.Path)}
		}
	}
	return nil
}

func asString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func hasNonEmptyList(v any) bool {
	items, ok := v.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if strings.TrimSpace(asString(item)) != "" {
			return true
		}
	}
	return false
}

func referenceFilesContainSkill(v any) bool {
	items, ok := v.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if strings.HasSuffix(asString(m["path"]), "SKILL.md") {
			return true
		}
	}
	return false
}
