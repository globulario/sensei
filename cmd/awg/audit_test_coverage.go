// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

type repositoryTestCoverage struct {
	CriticalHigh int
	Covered      int
	Missing      []string
}

type auditInvariantTestDeclaration struct {
	ID                      string   `yaml:"id"`
	Severity                string   `yaml:"severity"`
	RequiredTests           []string `yaml:"required_tests"`
	TestNotApplicableReason string   `yaml:"test_not_applicable_reason"`
}

type auditRequiredTestDeclaration struct {
	ID       string `yaml:"id"`
	Protects struct {
		Invariants []string `yaml:"invariants"`
	} `yaml:"protects"`
}

// assessRepositoryTestCoverage keeps invariants.yaml as the declared audit
// scope, but resolves coverage through both supported bindings:
//  1. inline invariant.required_tests;
//  2. the canonical docs/awareness/required_tests*.yaml registry.
//
// A registry entry counts only when it explicitly protects the exact invariant
// ID. Merely finding a similarly named test file is never treated as proof.
func assessRepositoryTestCoverage(repoRoot string) (repositoryTestCoverage, error) {
	invPath := filepath.Join(repoRoot, "docs", "awareness", "invariants.yaml")
	raw, err := os.ReadFile(invPath)
	if err != nil {
		return repositoryTestCoverage{}, fmt.Errorf("read invariants.yaml: %w", err)
	}
	var invDoc struct {
		Invariants []auditInvariantTestDeclaration `yaml:"invariants"`
	}
	if err := yaml.Unmarshal(raw, &invDoc); err != nil {
		return repositoryTestCoverage{}, fmt.Errorf("parse invariants.yaml: %w", err)
	}

	registryBindings := map[string][]string{}
	registryFiles, err := filepath.Glob(filepath.Join(repoRoot, "docs", "awareness", "required_tests*.yaml"))
	if err != nil {
		return repositoryTestCoverage{}, fmt.Errorf("glob required-test registries: %w", err)
	}
	sort.Strings(registryFiles)
	for _, registryPath := range registryFiles {
		registryRaw, err := os.ReadFile(registryPath)
		if err != nil {
			return repositoryTestCoverage{}, fmt.Errorf("read %s: %w", filepath.Base(registryPath), err)
		}
		var registryDoc struct {
			RequiredTests []auditRequiredTestDeclaration `yaml:"required_tests"`
		}
		if err := yaml.Unmarshal(registryRaw, &registryDoc); err != nil {
			return repositoryTestCoverage{}, fmt.Errorf("parse %s: %w", filepath.Base(registryPath), err)
		}
		for _, test := range registryDoc.RequiredTests {
			if test.ID == "" {
				continue
			}
			for _, invariantID := range test.Protects.Invariants {
				registryBindings[invariantID] = append(registryBindings[invariantID], test.ID)
			}
		}
	}

	result := repositoryTestCoverage{}
	for _, inv := range invDoc.Invariants {
		if inv.Severity != "critical" && inv.Severity != "high" {
			continue
		}
		result.CriticalHigh++
		covered := len(inv.RequiredTests) > 0 || inv.TestNotApplicableReason != "" || len(registryBindings[inv.ID]) > 0
		if covered {
			result.Covered++
			continue
		}
		result.Missing = append(result.Missing, fmt.Sprintf("[%s] %s", inv.Severity, inv.ID))
	}
	sort.Strings(result.Missing)
	return result, nil
}
