// SPDX-License-Identifier: Apache-2.0

package proofrequirements

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type authoritySurfaceInputDoc struct {
	AuthoritySurfaceCandidates struct {
		Candidates []authoritySurfaceCandidate `yaml:"candidates"`
	} `yaml:"authority_surface_candidates"`
	AuthoritySurfaces []authoritySurfaceCandidate `yaml:"authority_surfaces"`
}

type proofObligationsDoc struct {
	ProofObligations []generatedProofObligation `yaml:"proof_obligations"`
}

type generatedProofObligation struct {
	ID                          string               `yaml:"id"`
	Label                       string               `yaml:"label"`
	Status                      string               `yaml:"status"`
	DerivedFromStatus           string               `yaml:"derived_from_status"`
	DerivedFromAuthoritySurface string               `yaml:"derived_from_authority_surface"`
	AppliesToAuthoritySurfaces  []string             `yaml:"applies_to_authority_surfaces"`
	EvidenceLane                string               `yaml:"evidence_lane"`
	TemplateKind                string               `yaml:"template_kind"`
	RequiredSlots               []generatedProofSlot `yaml:"required_slots"`
	Notes                       string               `yaml:"notes,omitempty"`
}

type generatedProofSlot struct {
	ID          string `yaml:"id"`
	Kind        string `yaml:"kind"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
}

type proofTemplate struct {
	templateKind string
	evidenceLane string
	slots        []generatedProofSlot
	notes        string
}

func loadAuthoritySurfaces(path string) ([]authoritySurfaceCandidate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc authoritySurfaceInputDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if len(doc.AuthoritySurfaceCandidates.Candidates) > 0 {
		return doc.AuthoritySurfaceCandidates.Candidates, nil
	}
	return doc.AuthoritySurfaces, nil
}

func buildProofObligations(surfaces []authoritySurfaceCandidate) proofObligationsDoc {
	var out proofObligationsDoc
	for _, surface := range surfaces {
		tpl := templateForAuthoritySurface(surface)
		if tpl.templateKind == "" {
			continue
		}
		ob := generatedProofObligation{
			ID:                          "proof." + strings.TrimPrefix(surface.ID, "candidate."),
			Label:                       "Proof obligation for " + surface.ID,
			Status:                      proofStatusFromSurface(surface.Status),
			DerivedFromStatus:           strings.TrimSpace(surface.Status),
			DerivedFromAuthoritySurface: surface.ID,
			AppliesToAuthoritySurfaces:  []string{surface.ID},
			EvidenceLane:                tpl.evidenceLane,
			TemplateKind:                tpl.templateKind,
			RequiredSlots:               proofSlotsForSurface(surface.ID, tpl.slots),
			Notes:                       tpl.notes,
		}
		out.ProofObligations = append(out.ProofObligations, ob)
	}
	sort.SliceStable(out.ProofObligations, func(i, j int) bool {
		return out.ProofObligations[i].ID < out.ProofObligations[j].ID
	})
	return out
}

func renderProofObligations(doc proofObligationsDoc) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("# GENERATED proof obligations by `sensei extract-proof-obligations`.\n")
	buf.WriteString("# These obligations are derived from authority surfaces. They define\n")
	buf.WriteString("# what proof slots a repair must satisfy before certification can pass.\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	_ = enc.Close()
	return buf.Bytes(), nil
}

func renderProofObligationSummary(doc proofObligationsDoc, target string, check bool) string {
	var b strings.Builder
	if check {
		fmt.Fprintf(&b, "Proof obligations: %d\n", len(doc.ProofObligations))
		fmt.Fprintf(&b, "Check target: %s\n", target)
		return b.String()
	}
	fmt.Fprintf(&b, "extract-proof-obligations: wrote %d obligation(s) to %s\n", len(doc.ProofObligations), target)
	counts := map[string]int{}
	for _, ob := range doc.ProofObligations {
		counts[ob.TemplateKind]++
	}
	var keys []string
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "  %s=%d\n", k, counts[k])
	}
	return b.String()
}

func templateForAuthoritySurface(surface authoritySurfaceCandidate) proofTemplate {
	switch {
	case containsAny(surface.RequiredAuthority, "config_authority"):
		return proofTemplate{
			templateKind: "config_mutation",
			evidenceLane: "hybrid",
			slots: []generatedProofSlot{
				{Kind: "static_guard", Description: "Evidence that the config mutation path remains guarded or intentionally unguarded with explicit authority."},
				{Kind: "scope_mapping", Description: "Evidence that the mutation scope is bounded to the governed config surface."},
				{Kind: "before_after", Description: "Before/after config artifact proving the intended persisted change."},
				{Kind: "test_or_runtime", Description: "Test or runtime evidence showing the config change takes effect without collateral breakage."},
			},
			notes: "Derived from config authority: certification must explain config mutations by guard, scope, artifact, and behavior.",
		}
	case containsAny(surface.RequiredAuthority, "certificate_authority"):
		return proofTemplate{
			templateKind: "cert_or_key_operation",
			evidenceLane: "hybrid",
			slots: []generatedProofSlot{
				{Kind: "static_guard", Description: "Evidence that certificate/key operations remain behind the intended guard or trust boundary."},
				{Kind: "artifact", Description: "Artifact evidence for the produced certificate, CSR, or key material path."},
				{Kind: "input_validation", Description: "Evidence that CSR or input validation still governs the signing path."},
				{Kind: "negative_contract", Description: "Evidence that the repair does not leak key material or bypass the signing contract."},
			},
			notes: "Certificate and key operations require both behavioral and negative-contract proof.",
		}
	case surface.Kind == "lifecycle_control" || containsAny(surface.RequiredAuthority, "service_lifecycle_authority"):
		return proofTemplate{
			templateKind: "service_lifecycle",
			evidenceLane: "runtime_required",
			slots: []generatedProofSlot{
				{Kind: "runtime", Description: "Runtime evidence that the lifecycle transition occurred as intended."},
				{Kind: "process_artifact", Description: "Process or supervisor artifact showing the controlled service state."},
				{Kind: "log_artifact", Description: "Log or runtime artifact confirming the lifecycle action and outcome."},
				{Kind: "failure_evidence", Description: "Evidence of rollback, stop, or failure handling when the lifecycle action does not complete cleanly."},
			},
			notes: "Lifecycle authority is runtime-governed: score alone must never certify it.",
		}
	case containsAny(surface.RequiredAuthority, "network_authority"):
		return proofTemplate{
			templateKind: "peer_or_dns_mutation",
			evidenceLane: "hybrid",
			slots: []generatedProofSlot{
				{Kind: "static_guard", Description: "Evidence that peer or DNS mutation remains behind the intended authority path."},
				{Kind: "artifact", Description: "Artifact evidence for the modified peer, DNS, or registration state."},
				{Kind: "runtime", Description: "Runtime evidence that the peer/DNS mutation is visible at the intended observation path."},
				{Kind: "negative_contract", Description: "Evidence that the repair does not bypass ownership or trust boundaries."},
			},
			notes: "Peer/DNS mutations need both durable artifacts and live observation.",
		}
	case containsAny(surface.RequiredAuthority, "identity_authority"):
		return proofTemplate{
			templateKind: "auth_or_token_gate",
			evidenceLane: "static_only",
			slots: []generatedProofSlot{
				{Kind: "static_guard", Description: "Evidence that the identity or token gate is still enforced before mutation."},
				{Kind: "scope_mapping", Description: "Evidence that the claim is bounded to the identity surface rather than adjacent helpers."},
				{Kind: "negative_contract", Description: "Evidence that the repair does not bypass auth or widen issuance authority."},
			},
			notes: "Identity surfaces are governance-critical even when runtime artifacts are minimal.",
		}
	case containsAny(surface.RequiredAuthority, "filesystem_authority"):
		return proofTemplate{
			templateKind: "filesystem_mutation",
			evidenceLane: "hybrid",
			slots: []generatedProofSlot{
				{Kind: "scope_mapping", Description: "Evidence that the filesystem mutation is bounded to the governed path."},
				{Kind: "artifact", Description: "Artifact evidence for the created, removed, or rewritten file content."},
				{Kind: "negative_contract", Description: "Evidence that the repair does not mutate an ungoverned file path."},
			},
			notes: "Filesystem mutations require path-bounded artifacts before certification can pass.",
		}
	default:
		return proofTemplate{}
	}
}

func proofStatusFromSurface(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return "generated"
	}
	if status == "candidate" {
		return "candidate"
	}
	return "active"
}

func proofSlotsForSurface(surfaceID string, slots []generatedProofSlot) []generatedProofSlot {
	out := make([]generatedProofSlot, 0, len(slots))
	base := "slot." + strings.TrimPrefix(surfaceID, "candidate.")
	for _, slot := range slots {
		out = append(out, generatedProofSlot{
			ID:          base + "." + slot.Kind,
			Kind:        slot.Kind,
			Description: slot.Description,
			Required:    true,
		})
	}
	return out
}

func containsAny(items []string, want ...string) bool {
	set := map[string]bool{}
	for _, item := range items {
		set[strings.TrimSpace(item)] = true
	}
	for _, w := range want {
		if set[w] {
			return true
		}
	}
	return false
}
