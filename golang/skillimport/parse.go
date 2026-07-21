// SPDX-License-Identifier: AGPL-3.0-only

package skillimport

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type skillFrontMatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func DiscoverSkills(root string, includeDeprecated bool) ([]Skill, error) {
	result, err := DiscoverSkillsWithReport(root, includeDeprecated)
	if err != nil {
		return nil, err
	}
	return result.Skills, nil
}

func DiscoverSkillsWithReport(root string, includeDeprecated bool) (DiscoverResult, error) {
	var result DiscoverResult
	info, err := os.Stat(root)
	if err != nil {
		return result, err
	}
	if !info.IsDir() {
		return result, fmt.Errorf("%s is not a directory", root)
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return result, err
	}

	err = filepath.WalkDir(rootAbs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel := relSlash(rootAbs, path)
		if d.IsDir() {
			if shouldSkipDir(rel, d.Name(), includeDeprecated) {
				result.Skipped = append(result.Skipped, rel+"/")
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "SKILL.md" {
			return nil
		}
		skill, err := ParseSkillFile(path)
		if err != nil {
			result.Invalid = append(result.Invalid, FileIssue{Path: rel, Err: err})
			return nil
		}
		skill.SourcePath = rel
		skill.Category = inferCategory(rel)
		result.Skills = append(result.Skills, skill)
		return nil
	})
	if err != nil {
		return result, err
	}

	sort.Slice(result.Skills, func(i, j int) bool {
		return result.Skills[i].SourcePath < result.Skills[j].SourcePath
	})
	sort.Strings(result.Skipped)
	return result, nil
}

func ParseSkillFile(path string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	text := string(data)
	if !strings.HasPrefix(text, "---") {
		return Skill{}, errors.New("missing YAML front matter")
	}

	bodyStart := -1
	lines := strings.SplitAfter(text, "\n")
	offset := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if i > 0 && trimmed == "---" {
			bodyStart = offset + len(line)
			break
		}
		offset += len(line)
	}
	if bodyStart < 0 {
		return Skill{}, errors.New("unterminated YAML front matter")
	}

	header := text[len("---"):bodyStart]
	end := strings.LastIndex(header, "---")
	if end >= 0 {
		header = header[:end]
	}

	var fm skillFrontMatter
	if err := yaml.Unmarshal([]byte(header), &fm); err != nil {
		return Skill{}, fmt.Errorf("front matter: %w", err)
	}
	fm.Name = strings.TrimSpace(fm.Name)
	fm.Description = strings.TrimSpace(fm.Description)
	if fm.Name == "" {
		return Skill{}, errors.New("front matter missing name")
	}
	if fm.Description == "" {
		return Skill{}, errors.New("front matter missing description")
	}

	sourcePath := filepath.ToSlash(filepath.Clean(path))
	return Skill{
		Name:        fm.Name,
		Description: fm.Description,
		SourcePath:  sourcePath,
		Body:        strings.TrimLeft(text[bodyStart:], "\r\n"),
		Category:    inferCategory(sourcePath),
	}, nil
}

func shouldSkipDir(rel, name string, includeDeprecated bool) bool {
	switch name {
	case ".git", "node_modules", ".changeset", ".claude-plugin", "in-progress":
		return true
	case "deprecated":
		return !includeDeprecated
	}
	return false
}

func inferCategory(sourcePath string) string {
	parts := strings.Split(filepath.ToSlash(sourcePath), "/")
	for i, part := range parts {
		if part == "skills" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	if len(parts) > 2 {
		return parts[len(parts)-3]
	}
	return "general"
}

func relSlash(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	if rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}
