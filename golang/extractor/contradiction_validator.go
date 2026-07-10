// SPDX-License-Identifier: AGPL-3.0-only

// Contradiction and supersession validator (Phase 2E).
//
// A large graph becomes dangerous when old truths and new truths coexist
// without relationships. This validator walks the authored corpus and detects
// structural contradictions CI can fail on:
//
//   - superseded_active        : a node is active/accepted yet carries supersededBy
//   - authority_owner_conflict : two AuthorityDomains own the same state object
//     with different owner services
//   - repair_plan_unguarded_safety : a repair plan binds must_not_violate
//     invariants but, at a data_loss/security blast
//     radius, declares no approval gate
//   - duplicate_active_id      : the same node id is active in more than one file
//
// A node may carry `exception: <reason>` to document a deliberate deviation;
// when present, that node's contradictions are suppressed (the deviation is
// authored, not accidental).
package extractor

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Contradiction is one detected conflict.
type Contradiction struct {
	Rule   string
	Detail string
	Nodes  []string // node ids / file paths involved
}

func (c Contradiction) String() string {
	return fmt.Sprintf("[%s] %s (%s)", c.Rule, c.Detail, strings.Join(c.Nodes, ", "))
}

// corpusNode is the generic view the validator needs across node kinds.
type corpusNode struct {
	ID              string
	Class           string
	Path            string
	Status          string
	PromotionStatus string
	SupersededBy    string
	Exception       string
	// authority-domain fields
	OwnerService string
	OwnsState    []string
	// repair-plan fields
	BlastRadius    string
	ApprovalGate   string
	MustNotViolate []string
}

// ValidateContradictions walks the directories and returns detected
// contradictions, sorted for deterministic output.
func ValidateContradictions(dirs ...string) ([]Contradiction, error) {
	var nodes []corpusNode
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "candidates" {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".yaml") && !strings.HasSuffix(d.Name(), ".yml") {
				return nil
			}
			ns, verr := collectCorpusNodes(path)
			if verr != nil {
				return verr
			}
			nodes = append(nodes, ns...)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	out := runContradictionChecks(nodes)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Rule != out[j].Rule {
			return out[i].Rule < out[j].Rule
		}
		return out[i].Detail < out[j].Detail
	})
	return out, nil
}

// collectCorpusNodes extracts the generic node view(s) from one YAML file.
func collectCorpusNodes(path string) ([]corpusNode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, nil // not a single-doc map
	}

	// List-schema files (authority_domains, runtime_evidence) carry a top-level list.
	if lst, ok := raw["authority_domains"].([]any); ok {
		return listNodes(path, "AuthorityDomain", lst), nil
	}
	if lst, ok := raw["runtime_evidence"].([]any); ok {
		return listNodes(path, "RuntimeEvidence", lst), nil
	}

	// Single-entity file (id-keyed).
	if _, hasID := raw["id"]; !hasID {
		return nil, nil
	}
	return []corpusNode{nodeFromMap(path, raw)}, nil
}

func listNodes(path, class string, lst []any) []corpusNode {
	var out []corpusNode
	for _, item := range lst {
		if m, ok := item.(map[string]any); ok {
			n := nodeFromMap(path, m)
			if n.Class == "" {
				n.Class = class
			}
			if n.ID != "" {
				out = append(out, n)
			}
		}
	}
	return out
}

func nodeFromMap(path string, m map[string]any) corpusNode {
	n := corpusNode{
		ID:              str(m["id"]),
		Class:           str(m["class"]),
		Path:            path,
		Status:          str(m["status"]),
		PromotionStatus: str(m["promotion_status"]),
		SupersededBy:    str(m["superseded_by"]),
		Exception:       str(m["exception"]),
		OwnerService:    str(m["owner_service"]),
		OwnsState:       strList(m["owns_state"]),
		BlastRadius:     str(m["blast_radius"]),
		ApprovalGate:    str(m["approval_gate"]),
		MustNotViolate:  strList(m["must_not_violate_invariants"]),
	}
	return n
}

func runContradictionChecks(nodes []corpusNode) []Contradiction {
	var out []Contradiction

	// 1. superseded but still active.
	for _, n := range nodes {
		if n.Exception != "" {
			continue
		}
		if n.SupersededBy != "" && isActiveStatus(effectiveNodeStatus(n)) {
			out = append(out, Contradiction{
				Rule:   "superseded_active",
				Detail: fmt.Sprintf("node %q is %s but is superseded by %q", n.ID, effectiveNodeStatus(n), n.SupersededBy),
				Nodes:  []string{n.ID},
			})
		}
	}

	// 2. authority owner conflict: same owned state, different owner.
	ownerByState := map[string]corpusNode{}
	for _, n := range nodes {
		if n.Class != "AuthorityDomain" || n.Exception != "" {
			continue
		}
		for _, st := range n.OwnsState {
			key := strings.ToLower(strings.TrimSpace(st))
			if key == "" {
				continue
			}
			if prev, ok := ownerByState[key]; ok && !sameOwner(prev.OwnerService, n.OwnerService) {
				out = append(out, Contradiction{
					Rule:   "authority_owner_conflict",
					Detail: fmt.Sprintf("state %q claimed by %q (%s) and %q (%s)", st, prev.ID, prev.OwnerService, n.ID, n.OwnerService),
					Nodes:  []string{prev.ID, n.ID},
				})
			} else if !ok {
				ownerByState[key] = n
			}
		}
	}

	// 3. repair plan binds safety invariants but is ungated at a dangerous radius.
	for _, n := range nodes {
		if n.Class != "RepairPlan" || n.Exception != "" {
			continue
		}
		if len(n.MustNotViolate) == 0 {
			continue
		}
		if isDangerousRadius(n.BlastRadius) && isUngated(n.ApprovalGate) {
			out = append(out, Contradiction{
				Rule:   "repair_plan_unguarded_safety",
				Detail: fmt.Sprintf("repair plan %q binds must_not_violate invariants at blast_radius=%s but declares no approval gate", n.ID, n.BlastRadius),
				Nodes:  []string{n.ID},
			})
		}
	}

	// 4. duplicate active ids across files.
	activeByID := map[string]corpusNode{}
	for _, n := range nodes {
		if n.ID == "" || n.Exception != "" || !isActiveStatus(effectiveNodeStatus(n)) {
			continue
		}
		if prev, ok := activeByID[n.ID]; ok && prev.Path != n.Path {
			out = append(out, Contradiction{
				Rule:   "duplicate_active_id",
				Detail: fmt.Sprintf("id %q is active in two files: %s and %s", n.ID, prev.Path, n.Path),
				Nodes:  []string{n.ID},
			})
		} else if !ok {
			activeByID[n.ID] = n
		}
	}

	return out
}

func effectiveNodeStatus(n corpusNode) string {
	if n.PromotionStatus != "" {
		return strings.ToLower(strings.TrimSpace(n.PromotionStatus))
	}
	return strings.ToLower(strings.TrimSpace(n.Status))
}

func isActiveStatus(s string) bool { return s == "active" || s == "accepted" }

func isDangerousRadius(br string) bool {
	switch strings.ToLower(strings.TrimSpace(br)) {
	case "data_loss", "security", "external":
		return true
	}
	return false
}

func isUngated(gate string) bool {
	g := strings.ToLower(strings.TrimSpace(gate))
	return g == "" || g == "none"
}

func sameOwner(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func strList(v any) []string {
	lst, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, item := range lst {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
