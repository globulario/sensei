#!/usr/bin/env python3
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def replace_once(path: str, old: str, new: str) -> None:
    file_path = ROOT / path
    text = file_path.read_text()
    if old not in text:
        raise SystemExit(f"expected patch anchor not found in {path}")
    file_path.write_text(text.replace(old, new, 1))


def replace_go_function(path: str, signature: str, replacement: str) -> None:
    file_path = ROOT / path
    text = file_path.read_text()
    start = text.find(signature)
    if start < 0:
        raise SystemExit(f"function anchor not found in {path}: {signature}")
    brace = text.find("{", start)
    if brace < 0:
        raise SystemExit(f"opening brace not found in {path}: {signature}")
    depth = 0
    in_string = False
    in_raw = False
    escaped = False
    i = brace
    while i < len(text):
        ch = text[i]
        if in_raw:
            if ch == "`":
                in_raw = False
        elif in_string:
            if escaped:
                escaped = False
            elif ch == "\\":
                escaped = True
            elif ch == '"':
                in_string = False
        else:
            if ch == '"':
                in_string = True
            elif ch == "`":
                in_raw = True
            elif ch == "{":
                depth += 1
            elif ch == "}":
                depth -= 1
                if depth == 0:
                    end = i + 1
                    file_path.write_text(text[:start] + replacement.rstrip() + "\n" + text[end:])
                    return
        i += 1
    raise SystemExit(f"matching brace not found in {path}: {signature}")


def append_once(path: str, marker: str, addition: str) -> None:
    file_path = ROOT / path
    text = file_path.read_text()
    if marker in text:
        return
    file_path.write_text(text.rstrip() + "\n\n" + addition.strip() + "\n")


(ROOT / "golang/architecture/protection/relation_targets.go").write_text(r'''// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
    "fmt"
    "os"
    "path"
    "path/filepath"
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
''')

replace_once(
    "golang/architecture/protection/relations.go",
    '''\tout := map[string][]ProtectionReason{}\n\tadd := func(target, kind, source, knowledgeRef string) {\n\t\tnorm, ok := NormalizePath(target)\n\t\tif !ok {\n\t\t\t// contract §4/§6 correction: an invalid governed-relation\n\t\t\t// target (empty, or escaping the repository) must never be\n\t\t\t// silently dropped — it is a gap forcing at least PARTIAL\n\t\t\t// coverage, not a clean no-op.\n\t\t\tmalformed = append(malformed, fmt.Sprintf("%s: invalid target %q for %s (id=%s)", source, target, kind, knowledgeRef))\n\t\t\treturn\n\t\t}\n\t\tout[norm] = append(out[norm], ProtectionReason{\n\t\t\tOrigin:       OriginGovernedRelation,\n\t\t\tKind:         kind,\n\t\t\tSource:       source,\n\t\t\tKnowledgeRef: knowledgeRef,\n\t\t})\n\t}\n''',
    '''\tout := map[string][]ProtectionReason{}\n\ttargetPolicy, policyMalformed := loadRelationTargetPolicy(repoRoot)\n\tmalformed = append(malformed, policyMalformed...)\n\tadd := func(target, kind, source, knowledgeRef string) {\n\t\tnorm, disposition := targetPolicy.classify(target)\n\t\tswitch disposition {\n\t\tcase relationTargetExternal:\n\t\t\t// A declared runtime/sibling target is a valid relation anchor,\n\t\t\t// but it is outside this checkout's editable protection domain.\n\t\t\treturn\n\t\tcase relationTargetLocal:\n\t\t\tout[norm] = append(out[norm], ProtectionReason{\n\t\t\t\tOrigin:       OriginGovernedRelation,\n\t\t\t\tKind:         kind,\n\t\t\t\tSource:       source,\n\t\t\t\tKnowledgeRef: knowledgeRef,\n\t\t\t})\n\t\tdefault:\n\t\t\t// Undeclared absolute paths and repository escapes stay fail-closed.\n\t\t\tmalformed = append(malformed, fmt.Sprintf("%s: invalid or undeclared external target %q for %s (id=%s)", source, target, kind, knowledgeRef))\n\t\t}\n\t}\n''',
)

append_once(
    "golang/architecture/protection/relations_test.go",
    "TestGovernedRelationReasons_DeclaredExternalTargetsAreAccepted",
    r'''func TestGovernedRelationReasons_DeclaredExternalTargetsAreAccepted(t *testing.T) {
    root := t.TempDir()
    writeFile(t, root, RelationTargetsFile, `
relation_targets:
  runtime_roots:
    - /var/lib/globular/etcd
  sibling_repositories:
    - globular-installer
    - packages
`)
    writeFile(t, root, "docs/awareness/invariants.yaml", `
invariants:
  - id: test.external.targets
    title: External targets are explicit
    severity: high
    protects:
      files:
        - src/local.go
        - /var/lib/globular/etcd/member/snap/db
        - ../packages/registry.yaml
        - ../globular-installer/scripts/install-day0.sh
    required_tests:
      - ../globular-installer:make check-specs
`)

    reasons, malformed, err := GovernedRelationReasons(root)
    if err != nil {
        t.Fatal(err)
    }
    if len(malformed) != 0 {
        t.Fatalf("declared external targets must be accepted, got %v", malformed)
    }
    if len(reasons["src/local.go"]) == 0 {
        t.Fatal("local target must still produce local protection")
    }
    if _, exists := reasons["/var/lib/globular/etcd/member/snap/db"]; exists {
        t.Fatal("runtime target must not become a local ProtectedPath")
    }

    cov, err := Derive(root)
    if err != nil {
        t.Fatal(err)
    }
    if cov.Status != CoverageComplete {
        t.Fatalf("declared external targets must not degrade coverage, got %s: %v", cov.Status, cov.Gaps)
    }
}

func TestGovernedRelationReasons_UndeclaredRuntimeTargetRemainsMalformed(t *testing.T) {
    root := t.TempDir()
    writeFile(t, root, "docs/awareness/invariants.yaml", `
invariants:
  - id: test.external.undeclared
    title: Undeclared runtime target
    severity: high
    protects:
      files:
        - /var/lib/globular/etcd/member/snap/db
`)
    _, malformed, err := GovernedRelationReasons(root)
    if err != nil {
        t.Fatal(err)
    }
    if len(malformed) != 1 || !strings.Contains(malformed[0], "/var/lib/globular/etcd") {
        t.Fatalf("undeclared runtime path must remain malformed, got %v", malformed)
    }
}

func TestGovernedRelationReasons_MalformedExternalPolicyIsVisible(t *testing.T) {
    root := t.TempDir()
    writeFile(t, root, RelationTargetsFile, `
relation_targets:
  runtime_roots:
    - relative/runtime
    - /
  sibling_repositories:
    - nested/repo
`)
    writeFile(t, root, "docs/awareness/invariants.yaml", testInvariantsYAML)
    _, malformed, err := GovernedRelationReasons(root)
    if err != nil {
        t.Fatal(err)
    }
    if len(malformed) != 3 {
        t.Fatalf("expected three visible policy defects, got %v", malformed)
    }
}
''',
)

(ROOT / "golang/architecture/protection/candidate_document.go").write_text(r'''// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
    "fmt"

    "gopkg.in/yaml.v3"
)

// parseCandidateEntries accepts both candidate document shapes used by
// Sensei producers:
//
//   candidates: [...]                         (direct canonical list)
//   generator_name: { candidates: [...] }     (metadata/envelope wrapper)
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
''')

candidate_function = r'''func CandidateSignalReasons(repoRoot string) (reasons map[string][]ProtectionReason, malformed []string, err error) {
    dir := joinRepo(repoRoot, candidatesDir)
    entries, err := os.ReadDir(dir)
    if err != nil {
        if os.IsNotExist(err) {
            return map[string][]ProtectionReason{}, nil, nil
        }
        return nil, nil, err
    }
    out := map[string][]ProtectionReason{}
    for _, e := range entries {
        if e.IsDir() {
            continue
        }
        name := e.Name()
        if filepath.Ext(name) != ".yaml" && filepath.Ext(name) != ".yml" {
            continue
        }
        relSource, ok := NormalizePath(candidatesDir + "/" + name)
        if !ok {
            continue
        }
        raw, readErr := os.ReadFile(filepath.Join(dir, name))
        if readErr != nil {
            malformed = append(malformed, fmt.Sprintf("%s: unreadable: %v", relSource, readErr))
            continue
        }
        candidates, parseErr := parseCandidateEntries(raw)
        if parseErr != nil {
            malformed = append(malformed, fmt.Sprintf("%s: %v", relSource, parseErr))
            continue
        }
        for _, c := range candidates {
            targets := append([]string(nil), c.SourceFiles...)
            targets = append(targets, c.Files...)
            for _, target := range targets {
                norm, ok := NormalizePath(target)
                if !ok {
                    // Candidate targets are always files in this repository.
                    // External relation declarations do not grant candidates
                    // provisional authority outside the checkout.
                    malformed = append(malformed, fmt.Sprintf("%s: candidate %q has invalid target %q", relSource, c.ID, target))
                    continue
                }
                out[norm] = append(out[norm], ProtectionReason{
                    Origin:       OriginCandidateSignal,
                    Kind:         "candidate_source_file",
                    Source:       relSource,
                    KnowledgeRef: c.ID,
                    Provisional:  true,
                })
            }
        }
    }
    return out, malformed, nil
}'''
replace_go_function(
    "golang/architecture/protection/structural.go",
    "func CandidateSignalReasons(repoRoot string)",
    candidate_function,
)
replace_once(
    "golang/architecture/protection/structural.go",
    "// docs/awareness/candidates/*.yaml file: exactly one top-level key whose\n// value has a `candidates:` list, each entry carrying an `id` and either\n// `source_files` or `files`.",
    "// docs/awareness/candidates/*.yaml entry. Candidate documents may use a\n// direct top-level `candidates:` list or a generator metadata wrapper containing\n// that list; parseCandidateEntries owns the two accepted shapes.",
)

append_once(
    "golang/architecture/protection/structural_test.go",
    "TestCandidateSignalReasons_DirectTopLevelCandidates",
    r'''func TestCandidateSignalReasons_DirectTopLevelCandidates(t *testing.T) {
    root := t.TempDir()
    writeFile(t, root, "docs/awareness/candidates/session_discovered_invariants.yaml", `
candidates:
  - id: candidate.invariant.direct
    source_files:
      - src/direct.go
`)
    reasons, malformed, err := CandidateSignalReasons(root)
    if err != nil {
        t.Fatal(err)
    }
    if len(malformed) != 0 {
        t.Fatalf("direct candidate list must parse cleanly, got %v", malformed)
    }
    if len(reasons["src/direct.go"]) != 1 {
        t.Fatalf("direct candidate entry was not consumed: %v", reasons)
    }
}

func TestCandidateSignalReasons_MixedDirectAndWrappedIsMalformed(t *testing.T) {
    root := t.TempDir()
    writeFile(t, root, "docs/awareness/candidates/mixed.yaml", `
candidates:
  - id: candidate.direct
    source_files: [src/direct.go]
wrapped:
  candidates:
    - id: candidate.wrapped
      source_files: [src/wrapped.go]
`)
    reasons, malformed, err := CandidateSignalReasons(root)
    if err != nil {
        t.Fatal(err)
    }
    if len(reasons) != 0 || len(malformed) != 1 {
        t.Fatalf("mixed candidate authorities must fail closed, reasons=%v malformed=%v", reasons, malformed)
    }
}
''',
)

(ROOT / "cmd/awg/audit_test_coverage.go").write_text(r'''// SPDX-License-Identifier: AGPL-3.0-only

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
//   1. inline invariant.required_tests;
//   2. the canonical docs/awareness/required_tests*.yaml registry.
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
''')

(ROOT / "cmd/awg/audit_test_coverage_test.go").write_text(r'''// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
    "os"
    "path/filepath"
    "testing"
)

func writeAuditFixture(t *testing.T, root, rel, content string) {
    t.Helper()
    path := filepath.Join(root, filepath.FromSlash(rel))
    if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
        t.Fatal(err)
    }
}

func TestAssessRepositoryTestCoverage_InlineRegistryAndMissing(t *testing.T) {
    root := t.TempDir()
    writeAuditFixture(t, root, "docs/awareness/invariants.yaml", `
invariants:
  - id: inv.inline
    severity: high
    required_tests: [pkg/x_test.go:TestX]
  - id: inv.registry
    severity: critical
  - id: inv.missing
    severity: high
  - id: inv.warning
    severity: warning
`)
    writeAuditFixture(t, root, "docs/awareness/required_tests_dogfood.yaml", `
required_tests:
  - id: test.registry
    protects:
      invariants: [inv.registry]
`)
    got, err := assessRepositoryTestCoverage(root)
    if err != nil {
        t.Fatal(err)
    }
    if got.CriticalHigh != 3 || got.Covered != 2 || len(got.Missing) != 1 || got.Missing[0] != "[high] inv.missing" {
        t.Fatalf("unexpected assessment: %+v", got)
    }
}

func TestAssessRepositoryTestCoverage_RequiresExactRegistryBinding(t *testing.T) {
    root := t.TempDir()
    writeAuditFixture(t, root, "docs/awareness/invariants.yaml", `
invariants:
  - id: inv.exact
    severity: high
`)
    writeAuditFixture(t, root, "docs/awareness/required_tests.yaml", `
required_tests:
  - id: test.nearby
    protects:
      invariants: [inv.exact.but_different]
`)
    got, err := assessRepositoryTestCoverage(root)
    if err != nil {
        t.Fatal(err)
    }
    if len(got.Missing) != 1 {
        t.Fatalf("nearby ID must not manufacture coverage: %+v", got)
    }
}

func TestAssessRepositoryTestCoverage_MalformedRegistryIsVisible(t *testing.T) {
    root := t.TempDir()
    writeAuditFixture(t, root, "docs/awareness/invariants.yaml", "invariants: []\n")
    writeAuditFixture(t, root, "docs/awareness/required_tests.yaml", "required_tests: [\n")
    if _, err := assessRepositoryTestCoverage(root); err == nil {
        t.Fatal("malformed required-test registry must be reported")
    }
}
''')

new_check_test_coverage = r'''func checkTestCoverage(svcRepo string) auditResult {
    assessment, err := assessRepositoryTestCoverage(svcRepo)
    if err != nil {
        return auditResult{name: "test-coverage", level: auditWARN, summary: err.Error()}
    }
    if len(assessment.Missing) == 0 {
        return auditResult{name: "test-coverage", level: auditPASS,
            summary: fmt.Sprintf("all %d critical/high invariants covered", assessment.CriticalHigh)}
    }
    return auditResult{name: "test-coverage", level: auditWARN,
        summary: fmt.Sprintf("%d/%d critical/high invariants missing tests", len(assessment.Missing), assessment.CriticalHigh),
        details: assessment.Missing}
}'''
replace_go_function("cmd/awg/cmd_audit.go", "func checkTestCoverage(svcRepo string)", new_check_test_coverage)

(ROOT / "docs/design/governed-external-relation-targets.md").write_text(r'''# Governed external relation targets

## Status

Accepted implementation contract.

## Problem

A governed invariant may legitimately mention two kinds of target that are not
files inside the current checkout:

1. runtime state, such as `/var/lib/globular/etcd/...`;
2. a file or repository-level verification command in a sibling repository,
   such as `../globular-installer/scripts/install-day0.sh` or
   `../globular-installer:make check-specs`.

Treating every such target as an ordinary local file is false. Silently
accepting every absolute or `../` path is worse: a typo or repository escape
would disappear behind an apparently COMPLETE protection result.

## Contract

`docs/awareness/relation_targets.yaml` is the optional authored allowlist for
external relation domains.

```yaml
relation_targets:
  runtime_roots:
    - /var/lib/example/runtime
  sibling_repositories:
    - installer
```

A governed relation target is classified as exactly one of:

- **local**: a normalized repository-relative path; it creates a local
  `ProtectedPath` reason;
- **declared external**: an absolute path under a declared runtime root, or a
  `../<declared-sibling>/...` reference; it is accepted as traceability but
  creates no local edit-protection claim;
- **invalid/undeclared**: it remains a malformed source and forces PARTIAL
  coverage.

The allowlist is itself an authored governed source and therefore participates
in protection generation identity. Runtime root `/` is forbidden because it
would erase the boundary. Candidate files do not inherit this external-target
permission: candidates remain provisional signals about files in their own
checkout only.
''')

# The runner is one-shot. Its deletion is committed with the implementation.
Path(__file__).unlink()
