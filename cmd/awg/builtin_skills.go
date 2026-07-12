// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const skillManifestName = ".sensei-managed.json"

type builtinSkill struct {
	Name      string
	Version   string
	SourceDir string
	Targets   []string
}

type skillManifest struct {
	ManagedBy string            `json:"managed_by"`
	Skill     string            `json:"skill"`
	Version   string            `json:"version"`
	SourceDir string            `json:"source_dir"`
	Files     map[string]string `json:"files"`
}

var builtinSkills = []builtinSkill{
	{
		Name:      "sensei-architect",
		Version:   "2026.07.12",
		SourceDir: "templates/skills/sensei-architect",
		Targets: []string{
			filepath.Join(".sensei", "skills", "sensei-architect"),
			filepath.Join(".agents", "skills", "sensei-architect"),
			filepath.Join(".claude", "skills", "sensei-architect"),
		},
	},
	{
		Name:      "sensei-import",
		Version:   "2026.07.12",
		SourceDir: "templates/skills/sensei-import",
		Targets: []string{
			filepath.Join(".sensei", "skills", "sensei-import"),
			filepath.Join(".agents", "skills", "sensei-import"),
			filepath.Join(".claude", "skills", "sensei-import"),
		},
	},
}

func scaffoldBuiltinSkills(root string, force bool) ([]string, []string, error) {
	var created []string
	var notices []string
	for _, skill := range builtinSkills {
		files, err := bundledSkillFiles(skill)
		if err != nil {
			return nil, nil, err
		}
		for _, target := range skill.Targets {
			wrote, notice, err := syncBuiltinSkill(root, skill, target, files, force)
			if err != nil {
				return nil, nil, err
			}
			created = append(created, wrote...)
			if notice != "" {
				notices = append(notices, notice)
			}
		}
	}
	sort.Strings(created)
	sort.Strings(notices)
	return created, notices, nil
}

func bundledSkillFiles(skill builtinSkill) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(templates, skill.SourceDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := templates.ReadFile(path)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(path, skill.SourceDir+"/")
		files[rel] = data
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read bundled skill %s: %w", skill.Name, err)
	}
	if _, ok := files["SKILL.md"]; !ok {
		return nil, fmt.Errorf("bundled skill %s is missing SKILL.md", skill.Name)
	}
	return files, nil
}

func syncBuiltinSkill(root string, skill builtinSkill, targetRel string, files map[string][]byte, force bool) ([]string, string, error) {
	target := filepath.Join(root, targetRel)
	manifestPath := filepath.Join(target, skillManifestName)
	manifest, manifestFound, manifestValid, err := readSkillManifest(manifestPath)
	if err != nil {
		return nil, "", err
	}

	if !manifestFound && dirHasUserContent(target) {
		if !force {
			return nil, fmt.Sprintf("%s already exists without a Sensei manifest; left unchanged (use --skills-force to replace)", targetRel), nil
		}
	}
	if manifestFound && !manifestValid && !force {
		return nil, fmt.Sprintf("%s has an unreadable Sensei manifest; left unchanged (use --skills-force to replace)", targetRel), nil
	}
	if manifestFound && manifestValid && !force {
		if modified, detail := managedSkillModified(target, manifest); modified {
			return nil, fmt.Sprintf("%s was modified locally (%s); left unchanged (use --skills-force to replace)", targetRel, detail), nil
		}
	}

	next := buildSkillManifest(skill, files)
	if manifestFound && manifestValid && !force && sameSkillManifest(manifest, next) {
		return nil, "", nil
	}

	if err := os.MkdirAll(target, 0o755); err != nil {
		return nil, "", err
	}

	var wrote []string
	for rel, data := range files {
		dst := filepath.Join(target, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, "", err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return nil, "", err
		}
		wrote = append(wrote, dst)
	}
	if manifestFound && manifestValid {
		for rel := range manifest.Files {
			if _, stillManaged := files[rel]; stillManaged {
				continue
			}
			_ = os.Remove(filepath.Join(target, filepath.FromSlash(rel)))
		}
	}

	manifestData, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return nil, "", err
	}
	if err := os.WriteFile(manifestPath, append(manifestData, '\n'), 0o644); err != nil {
		return nil, "", err
	}
	wrote = append(wrote, manifestPath)
	sort.Strings(wrote)
	return wrote, "", nil
}

func readSkillManifest(path string) (skillManifest, bool, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return skillManifest{}, false, false, nil
		}
		return skillManifest{}, false, false, err
	}
	var manifest skillManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return skillManifest{}, true, false, nil
	}
	if manifest.ManagedBy != "sensei init" || manifest.Skill == "" || len(manifest.Files) == 0 {
		return skillManifest{}, true, false, nil
	}
	return manifest, true, true, nil
}

func buildSkillManifest(skill builtinSkill, files map[string][]byte) skillManifest {
	digests := map[string]string{}
	for rel, data := range files {
		digests[rel] = sha256Hex(data)
	}
	return skillManifest{
		ManagedBy: "sensei init",
		Skill:     skill.Name,
		Version:   skill.Version,
		SourceDir: skill.SourceDir,
		Files:     digests,
	}
}

func sameSkillManifest(a, b skillManifest) bool {
	if a.Skill != b.Skill || a.Version != b.Version || a.SourceDir != b.SourceDir || len(a.Files) != len(b.Files) {
		return false
	}
	for rel, digest := range a.Files {
		if b.Files[rel] != digest {
			return false
		}
	}
	return true
}

func managedSkillModified(target string, manifest skillManifest) (bool, string) {
	for rel, digest := range manifest.Files {
		data, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(rel)))
		if err != nil {
			return true, rel + " missing"
		}
		if sha256Hex(data) != digest {
			return true, rel + " changed"
		}
	}
	return false, ""
}

func dirHasUserContent(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Name() == skillManifestName {
			continue
		}
		return true
	}
	return false
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
