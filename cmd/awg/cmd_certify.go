// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type certifyResult struct {
	EventID                   string                    `json:"event_id,omitempty"`
	Task                      string                    `json:"task,omitempty"`
	Score                     int                       `json:"score,omitempty"`
	LegacyCertificationStatus string                    `json:"legacy_certification_status,omitempty"`
	RepairClaim               certifyRepairClaim        `json:"repair_claim"`
	ProofMapping              certifyProofMapping       `json:"proof_mapping,omitempty"`
	EvidenceArtifacts         []certifyEvidenceArtifact `json:"evidence_artifacts,omitempty"`
	DetectedForbiddenMoves    []certifyForbiddenMove    `json:"detected_forbidden_moves,omitempty"`
	GovernanceCertification   certifyGovernanceResult   `json:"governance_certification"`
}

type certifyRepairClaim struct {
	ID                  string   `json:"id,omitempty"`
	Summary             string   `json:"summary,omitempty"`
	ContractIDs         []string `json:"contract_ids,omitempty"`
	ScopeFiles          []string `json:"scope_files,omitempty"`
	AuthoritySurfaceIDs []string `json:"authority_surface_ids,omitempty"`
	ProofObligationIDs  []string `json:"proof_obligation_ids,omitempty"`
	ForbiddenMoveIDs    []string `json:"forbidden_move_ids,omitempty"`
}

type certifyProofMapping struct {
	Static    []string `json:"static,omitempty"`
	Tests     []string `json:"tests,omitempty"`
	Runtime   []string `json:"runtime,omitempty"`
	Artifacts []string `json:"artifacts,omitempty"`
}

type certifyEvidenceArtifact struct {
	ID                         string                   `json:"id,omitempty"`
	Kind                       string                   `json:"kind,omitempty"`
	Path                       string                   `json:"path,omitempty"`
	Satisfies                  []certifyArtifactSatisfy `json:"satisfies,omitempty"`
	RelatedAuthoritySurfaceIDs []string                 `json:"related_authority_surface_ids,omitempty"`
	RelatedProofObligationIDs  []string                 `json:"related_proof_obligation_ids,omitempty"`
}

type certifyArtifactSatisfy struct {
	ProofObligationID string `json:"proof_obligation_id,omitempty"`
	Slot              string `json:"slot,omitempty"`
}

type certifyForbiddenMove struct {
	ID           string `json:"id,omitempty"`
	Reason       string `json:"reason,omitempty"`
	EvidenceKind string `json:"evidence_kind,omitempty"`
	EvidencePath string `json:"evidence_path,omitempty"`
}

type certifyGovernanceResult struct {
	RepairClaimID               string              `json:"repair_claim_id,omitempty"`
	CertificationRequirementIDs []string            `json:"certification_requirement_ids,omitempty"`
	Obligations                 []certifyObligation `json:"obligations,omitempty"`
	Lanes                       []certifyLaneResult `json:"lanes,omitempty"`
	Verdict                     string              `json:"verdict"`
	Promotion                   string              `json:"promotion"`
	ScoreUsedForCertification   bool                `json:"score_used_for_certification"`
	MissingEvidence             []string            `json:"missing_evidence,omitempty"`
	BlockedByForbiddenMoveIDs   []string            `json:"blocked_by_forbidden_move_ids,omitempty"`
	Notes                       []string            `json:"notes,omitempty"`
}

type certifyLaneResult struct {
	Lane    string   `json:"lane"`
	Status  string   `json:"status"`
	Reasons []string `json:"reasons,omitempty"`
}

type certifyObligation struct {
	ID             string              `json:"id"`
	Status         string              `json:"status"`
	EvidenceLane   string              `json:"evidence_lane,omitempty"`
	RequiredSlots  []string            `json:"required_slots,omitempty"`
	SatisfiedSlots []string            `json:"satisfied_slots,omitempty"`
	MissingSlots   []string            `json:"missing_slots,omitempty"`
	SlotResults    []certifySlotResult `json:"slot_results,omitempty"`
}

type certifySlotResult struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind"`
	Required         bool     `json:"required"`
	AvailableSources []string `json:"available_sources,omitempty"`
	MappedEvidence   []string `json:"mapped_evidence,omitempty"`
	MappingSource    string   `json:"mapping_source,omitempty"`
	ArtifactID       string   `json:"artifact_id,omitempty"`
	Status           string   `json:"status"`
}

func runCertify(args []string) int {
	fs := flag.NewFlagSet("awg certify", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	eventPath := fs.String("event", "", "learning event YAML/JSON file")
	proofPath := fs.String("proof-obligations", "", "proof obligations YAML (default: docs/awareness/generated/proof_obligations.yaml)")
	format := fs.String("format", "text", "output format: text | json")
	asJSON := fs.Bool("json", false, "output as JSON (deprecated: same as --format json)")
	field := fs.String("field", "", "print only one field: verdict | promotion | repair_claim_id | legacy_certification_status")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg certify [flags]

Evaluate repair-governance certification from an authored benchmark learning
event. Score may be reported, but it is never allowed to decide promotion.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *asJSON {
		*format = "json"
	}
	if strings.TrimSpace(*eventPath) == "" {
		fmt.Fprintln(os.Stderr, "awg certify: --event is required")
		return 2
	}
	doc, err := loadBenchmarkRetryDoc(*eventPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg certify: load event: %v\n", err)
		return 1
	}
	proofDoc, err := loadProofObligationsForCertify(*proofPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg certify: load proof obligations: %v\n", err)
		return 1
	}
	res := buildCertifyResult(benchmarkRetryUnwrapEvent(doc), proofDoc)
	if strings.TrimSpace(*field) != "" {
		switch strings.TrimSpace(*field) {
		case "verdict":
			fmt.Println(res.GovernanceCertification.Verdict)
		case "promotion":
			fmt.Println(res.GovernanceCertification.Promotion)
		case "repair_claim_id":
			fmt.Println(res.GovernanceCertification.RepairClaimID)
		case "legacy_certification_status":
			fmt.Println(res.LegacyCertificationStatus)
		default:
			fmt.Fprintf(os.Stderr, "awg certify: unknown --field %q\n", *field)
			return 2
		}
		return 0
	}

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
	default:
		fmt.Print(renderCertifyText(res))
	}
	return 0
}

func buildCertifyResult(event map[string]any, proofDoc proofObligationsDoc) certifyResult {
	claim := parseRepairClaim(event)
	proof := parseProofMapping(event)
	artifacts := parseEvidenceArtifacts(event)
	forbiddenMoves := parseDetectedForbiddenMoves(event)
	governance := evaluateGovernance(event, claim, proof, artifacts, forbiddenMoves, proofDoc)
	return certifyResult{
		EventID:                   benchmarkPresentString(event["id"]),
		Task:                      benchmarkPresentString(event["task"]),
		Score:                     benchmarkInt(benchmarkMap(event["current"])["score"]),
		LegacyCertificationStatus: firstNonEmpty(benchmarkPresentString(event["certification_status"]), benchmarkPresentString(benchmarkMap(event["certification"])["certification_status"])),
		RepairClaim:               claim,
		ProofMapping:              proof,
		EvidenceArtifacts:         artifacts,
		DetectedForbiddenMoves:    forbiddenMoves,
		GovernanceCertification:   governance,
	}
}

func parseRepairClaim(event map[string]any) certifyRepairClaim {
	raw := benchmarkMap(event["repair_claim"])
	claim := certifyRepairClaim{
		ID:                  benchmarkPresentString(raw["id"]),
		Summary:             benchmarkPresentString(raw["summary"]),
		ContractIDs:         benchmarkStringList(raw["contract_ids"]),
		ScopeFiles:          benchmarkStringList(raw["scope_files"]),
		AuthoritySurfaceIDs: benchmarkStringList(raw["authority_surface_ids"]),
		ProofObligationIDs:  benchmarkStringList(raw["proof_obligation_ids"]),
		ForbiddenMoveIDs:    benchmarkStringList(raw["forbidden_move_ids"]),
	}
	if len(claim.ContractIDs) == 0 {
		if contractID := firstNonEmpty(benchmarkPresentString(event["governing_contract_id"]), benchmarkPresentString(benchmarkMap(event["certification"])["governing_contract_id"])); contractID != "" {
			claim.ContractIDs = []string{contractID}
		}
	}
	if claim.ID == "" && len(claim.ContractIDs) > 0 {
		claim.ID = "claim." + strings.ReplaceAll(claim.ContractIDs[0], "contract.", "")
	}
	if claim.Summary == "" && len(claim.ContractIDs) > 0 {
		claim.Summary = "Repair claim governed by " + claim.ContractIDs[0]
	}
	return claim
}

func parseProofMapping(event map[string]any) certifyProofMapping {
	raw := benchmarkMap(event["proof_mapping"])
	return certifyProofMapping{
		Static:    benchmarkRefList(raw["static"]),
		Tests:     benchmarkRefList(raw["tests"]),
		Runtime:   benchmarkRefList(raw["runtime"]),
		Artifacts: benchmarkRefList(raw["artifacts"]),
	}
}

func parseEvidenceArtifacts(event map[string]any) []certifyEvidenceArtifact {
	items, ok := event["evidence_artifacts"].([]any)
	if !ok {
		return nil
	}
	var out []certifyEvidenceArtifact
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		artifact := certifyEvidenceArtifact{
			ID:                         benchmarkPresentString(m["id"]),
			Kind:                       benchmarkPresentString(m["kind"]),
			Path:                       benchmarkPresentString(m["path"]),
			RelatedAuthoritySurfaceIDs: benchmarkStringList(m["related_authority_surface_ids"]),
			RelatedProofObligationIDs:  benchmarkStringList(m["related_proof_obligation_ids"]),
		}
		if sats, ok := m["satisfies"].([]any); ok {
			for _, sat := range sats {
				sm, ok := sat.(map[string]any)
				if !ok {
					continue
				}
				artifact.Satisfies = append(artifact.Satisfies, certifyArtifactSatisfy{
					ProofObligationID: benchmarkPresentString(sm["proof_obligation_id"]),
					Slot:              benchmarkPresentString(sm["slot"]),
				})
			}
		}
		if artifact.ID == "" && artifact.Path != "" {
			artifact.ID = artifact.Path
		}
		out = append(out, artifact)
	}
	return out
}

func parseDetectedForbiddenMoves(event map[string]any) []certifyForbiddenMove {
	items, ok := event["detected_forbidden_moves"].([]any)
	if !ok {
		return nil
	}
	var out []certifyForbiddenMove
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		move := certifyForbiddenMove{
			ID:     benchmarkPresentString(m["id"]),
			Reason: benchmarkPresentString(m["reason"]),
		}
		if evidence := benchmarkMap(m["evidence"]); len(evidence) > 0 {
			move.EvidenceKind = benchmarkPresentString(evidence["kind"])
			move.EvidencePath = firstNonEmpty(benchmarkPresentString(evidence["path"]), benchmarkPresentString(evidence["ref"]))
		}
		if move.ID != "" {
			out = append(out, move)
		}
	}
	return out
}

func evaluateGovernance(event map[string]any, claim certifyRepairClaim, proof certifyProofMapping, artifacts []certifyEvidenceArtifact, forbiddenMoves []certifyForbiddenMove, proofDoc proofObligationsDoc) certifyGovernanceResult {
	cert := benchmarkMap(event["certification"])
	legacyStatus := firstNonEmpty(benchmarkString(event["certification_status"]), benchmarkString(cert["certification_status"]))
	missingEvidence := certifyDedupeStrings(append(benchmarkStringList(event["missing_evidence"]), benchmarkStringList(cert["missing_evidence"])...))
	blockedByForbidden := certifyDedupeStrings(append(claim.ForbiddenMoveIDs, benchmarkStringList(benchmarkMap(event["governance_certification"])["blocked_by_forbidden_move_ids"])...))
	for _, move := range forbiddenMoves {
		blockedByForbidden = append(blockedByForbidden, move.ID)
	}
	blockedByForbidden = certifyDedupeStrings(blockedByForbidden)
	reasons := benchmarkStringSet(event["contract_clean_reasons"])
	primaryFailure := benchmarkString(benchmarkMap(event["diagnosis"])["primary_failure_mode"])
	obligations := matchProofObligations(claim, proofDoc)
	obligationResults := evaluateObligations(obligations, proof, artifacts)

	scope := certifyLaneResult{Lane: "scope", Status: "pass"}
	if !benchmarkTruthyDefault(cert["scope_valid"], true) || benchmarkString(benchmarkMap(event["current"])["contract_scope_status"]) == "underconstrained" || reasons["scope_underconstrained"] || reasons["out_of_scope_edit"] || reasons["edited_file_out_of_scope"] {
		scope.Status = "blocked"
		scope.Reasons = certifyDedupeStrings(append(scope.Reasons,
			append(reasonIf(!benchmarkTruthyDefault(cert["scope_valid"], true), "scope_invalid"),
				append(reasonIf(benchmarkString(benchmarkMap(event["current"])["contract_scope_status"]) == "underconstrained", "scope_underconstrained"),
					append(reasonIf(reasons["out_of_scope_edit"], "out_of_scope_edit"),
						reasonIf(reasons["edited_file_out_of_scope"], "edited_file_out_of_scope")...)...)...)...))
	} else if len(claim.ScopeFiles) == 0 && len(claim.ContractIDs) == 0 {
		scope.Status = "unknown"
		scope.Reasons = []string{"repair_claim_scope_missing"}
	}

	authority := certifyLaneResult{Lane: "authority", Status: "pass"}
	govContract := firstNonEmpty(benchmarkPresentString(event["governing_contract_id"]), benchmarkPresentString(cert["governing_contract_id"]))
	frozenPresent := benchmarkTruthyDefault(cert["frozen_contract_present"], false)
	blockValid := benchmarkTruthyDefault(cert["contract_block_valid"], false)
	blockMapped := benchmarkTruthyDefault(cert["contract_block_maps_to_frozen_contract"], false)
	if govContract == "" || !frozenPresent {
		authority.Status = "blocked"
		authority.Reasons = certifyDedupeStrings([]string{"governing_contract_missing", "frozen_contract_missing"})
		if govContract != "" {
			authority.Reasons = []string{"frozen_contract_missing"}
		}
	} else if !blockValid || !blockMapped {
		authority.Status = "blocked"
		authority.Reasons = certifyDedupeStrings(append(authority.Reasons,
			append(reasonIf(!blockValid, "contract_block_invalid"),
				reasonIf(!blockMapped, "contract_block_mapping_missing")...)...))
	}

	proofLane := certifyLaneResult{Lane: "proof", Status: "pass"}
	requiredPathsSatisfied := benchmarkTruthyDefault(cert["required_paths_satisfied"], true)
	hasProofEvidence := len(proof.Static)+len(proof.Tests)+len(proof.Runtime)+len(proof.Artifacts) > 0
	if !requiredPathsSatisfied || reasons["test_proof_incomplete"] || reasons["missing_required_test_path"] || reasons["missing_required_test_symbol"] {
		proofLane.Status = "blocked"
		proofLane.Reasons = certifyDedupeStrings(append(proofLane.Reasons,
			append(reasonIf(!requiredPathsSatisfied, "required_paths_unsatisfied"),
				append(reasonIf(reasons["test_proof_incomplete"], "test_proof_incomplete"),
					append(reasonIf(reasons["missing_required_test_path"], "missing_required_test_path"),
						reasonIf(reasons["missing_required_test_symbol"], "missing_required_test_symbol")...)...)...)...))
	} else if len(obligationResults) > 0 {
		missingSlots := collectObligationProofReasons(obligationResults)
		if len(missingSlots) > 0 {
			proofLane.Status = "blocked"
			proofLane.Reasons = certifyDedupeStrings(append(proofLane.Reasons, missingSlots...))
		}
	} else if len(claim.ProofObligationIDs) > 0 && !hasProofEvidence {
		proofLane.Status = "blocked"
		proofLane.Reasons = []string{"proof_mapping_missing"}
	}

	evidence := certifyLaneResult{Lane: "evidence", Status: "pass"}
	evidenceSufficient := benchmarkTruthyDefault(cert["evidence_sufficient"], len(missingEvidence) == 0)
	if !evidenceSufficient || len(missingEvidence) > 0 || reasons["verification_impossible_required_path"] {
		evidence.Status = "blocked"
		evidence.Reasons = certifyDedupeStrings(append(evidence.Reasons, missingEvidence...))
		if reasons["verification_impossible_required_path"] {
			evidence.Reasons = append(evidence.Reasons, "verification_impossible_required_path")
		}
		if !evidenceSufficient && len(evidence.Reasons) == 0 {
			evidence.Reasons = []string{"evidence_insufficient"}
		}
	}
	if obligationEvidenceReasons := collectObligationEvidenceReasons(obligationResults); len(obligationEvidenceReasons) > 0 {
		evidence.Status = "blocked"
		evidence.Reasons = certifyDedupeStrings(append(evidence.Reasons, obligationEvidenceReasons...))
	}

	lanes := []certifyLaneResult{scope, authority, proofLane, evidence}
	notes := []string{}
	if primaryFailure != "" {
		notes = append(notes, "primary_failure_mode:"+primaryFailure)
	}
	if legacyStatus != "" {
		notes = append(notes, "legacy_certification_status:"+legacyStatus)
	}

	verdict, promotion := certifyVerdictAndPromotion(event, legacyStatus, lanes, missingEvidence, blockedByForbidden, primaryFailure, obligationResults)
	if benchmarkTruthyDefault(benchmarkMap(event["governance_certification"])["score_used_for_certification"], false) {
		notes = append(notes, "governance_input_claimed_score_use")
	}

	govRaw := benchmarkMap(event["governance_certification"])
	return certifyGovernanceResult{
		RepairClaimID:               firstNonEmpty(benchmarkPresentString(govRaw["repair_claim_id"]), claim.ID),
		CertificationRequirementIDs: benchmarkStringList(govRaw["certification_requirement_ids"]),
		Obligations:                 obligationResults,
		Lanes:                       lanes,
		Verdict:                     verdict,
		Promotion:                   promotion,
		ScoreUsedForCertification:   false,
		MissingEvidence:             missingEvidence,
		BlockedByForbiddenMoveIDs:   blockedByForbidden,
		Notes:                       certifyDedupeStrings(notes),
	}
}

func certifyVerdictAndPromotion(event map[string]any, legacyStatus string, lanes []certifyLaneResult, missingEvidence, blockedByForbidden []string, primaryFailure string, obligations []certifyObligation) (string, string) {
	byName := map[string]certifyLaneResult{}
	for _, lane := range lanes {
		byName[lane.Lane] = lane
	}
	humanReviewRequired := benchmarkTruthyDefault(event["human_review_required"], benchmarkTruthyDefault(benchmarkMap(event["certification"])["human_review_required"], false))
	contaminated := benchmarkTruthyDefault(benchmarkMap(event["diagnosis"])["contaminated"], false)
	if len(blockedByForbidden) > 0 {
		return "forbidden_move_detected", "blocked"
	}
	if contaminated {
		return "uncertified_score_only", "blocked"
	}
	if byName["scope"].Status == "blocked" {
		return "scope_unbounded", "blocked"
	}
	if byName["authority"].Status == "blocked" {
		return "authority_unknown", "blocked"
	}
	if runtimeEvidenceMissing(missingEvidence) || obligationsHaveRuntimeLaneMissingSource(obligations) {
		return "runtime_evidence_missing", "blocked"
	}
	if obligationsHaveUnmappedEvidence(obligations) || byName["evidence"].Status == "blocked" {
		return "artifact_mapping_missing", "blocked"
	}
	if obligationsHaveMissingRequiredSlots(obligations) || byName["proof"].Status == "blocked" {
		return "proof_missing", "blocked"
	}
	if primaryFailure == "verification_impossible" || legacyStatus == "verification_impossible" {
		return "verification_impossible", "blocked"
	}
	if humanReviewRequired {
		return "certified_partial_repair", "review_required"
	}
	if legacyStatus == "certified_clean_repair" || benchmarkTruthyDefault(event["promotion_allowed"], benchmarkTruthyDefault(benchmarkMap(event["certification"])["promotion_allowed"], false)) {
		return "certified_clean_repair", "allowed"
	}
	return "uncertified_score_only", "review_required"
}

func loadProofObligationsForCertify(path string) (proofObligationsDoc, error) {
	if strings.TrimSpace(path) == "" {
		path = filepath.Join("docs", "awareness", "generated", "proof_obligations.yaml")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return proofObligationsDoc{}, nil
		}
		return proofObligationsDoc{}, err
	}
	var doc proofObligationsDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return proofObligationsDoc{}, err
	}
	return doc, nil
}

func matchProofObligations(claim certifyRepairClaim, doc proofObligationsDoc) []generatedProofObligation {
	if len(doc.ProofObligations) == 0 {
		return nil
	}
	want := map[string]bool{}
	for _, id := range claim.ProofObligationIDs {
		if id = strings.TrimSpace(id); id != "" {
			want[id] = true
		}
	}
	for _, id := range claim.AuthoritySurfaceIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		want["proof."+strings.TrimPrefix(id, "candidate.")] = true
	}
	var out []generatedProofObligation
	for _, ob := range doc.ProofObligations {
		if want[ob.ID] {
			out = append(out, ob)
			continue
		}
		for _, ref := range ob.AppliesToAuthoritySurfaces {
			if want[strings.TrimSpace(ref)] {
				out = append(out, ob)
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func evaluateObligations(obligations []generatedProofObligation, proof certifyProofMapping, artifacts []certifyEvidenceArtifact) []certifyObligation {
	var out []certifyObligation
	for _, ob := range obligations {
		res := certifyObligation{
			ID:           ob.ID,
			Status:       "satisfied",
			EvidenceLane: ob.EvidenceLane,
		}
		for _, slot := range ob.RequiredSlots {
			slotRes := evaluateProofSlot(ob, slot, proof, artifacts)
			if slot.Required {
				res.RequiredSlots = append(res.RequiredSlots, slot.Kind)
			}
			if slotRes.Status == "satisfied" {
				res.SatisfiedSlots = append(res.SatisfiedSlots, slot.Kind)
			} else {
				res.MissingSlots = append(res.MissingSlots, slot.Kind)
			}
			res.SlotResults = append(res.SlotResults, slotRes)
		}
		if len(res.MissingSlots) > 0 {
			res.Status = "missing_required_slots"
		}
		out = append(out, res)
	}
	return out
}

func evaluateProofSlot(ob generatedProofObligation, slot generatedProofSlot, proof certifyProofMapping, artifacts []certifyEvidenceArtifact) certifySlotResult {
	legacySources := proofSourcesForSlot(slot.Kind, proof)
	availableSources := append([]string{}, legacySources...)
	availableArtifacts := availableArtifactsForSlot(ob, slot, artifacts)
	compatibleArtifacts := compatibleArtifactsForSlot(ob, slot, artifacts)
	for _, art := range availableArtifacts {
		availableSources = append(availableSources, art.ID)
	}
	availableSources = certifyDedupeStrings(availableSources)
	mappedEvidence, mappingSource, artifactID := mappedEvidenceForSlot(ob, slot, legacySources, compatibleArtifacts)
	status := "satisfied"
	switch {
	case !slot.Required:
		status = "optional"
	case len(mappedEvidence) > 0:
		status = "satisfied"
	case len(availableSources) > 0:
		status = "available_unmapped"
	default:
		status = "missing_source"
	}
	return certifySlotResult{
		ID:               slot.ID,
		Kind:             slot.Kind,
		Required:         slot.Required,
		AvailableSources: availableSources,
		MappedEvidence:   mappedEvidence,
		MappingSource:    mappingSource,
		ArtifactID:       artifactID,
		Status:           status,
	}
}

func proofSourcesForSlot(kind string, proof certifyProofMapping) []string {
	switch kind {
	case "static_guard", "scope_mapping", "input_validation":
		return certifyDedupeStrings(append(append([]string{}, proof.Static...), proof.Tests...))
	case "before_after", "artifact":
		return certifyDedupeStrings(proof.Artifacts)
	case "runtime", "process_artifact", "log_artifact", "failure_evidence":
		return certifyDedupeStrings(append(append([]string{}, proof.Runtime...), proof.Artifacts...))
	case "test_or_runtime":
		return certifyDedupeStrings(append(append([]string{}, proof.Tests...), proof.Runtime...))
	case "negative_contract":
		return certifyDedupeStrings(append(append(append([]string{}, proof.Static...), proof.Tests...), proof.Artifacts...))
	default:
		return certifyDedupeStrings(append(append(append([]string{}, proof.Static...), proof.Tests...), append(proof.Runtime, proof.Artifacts...)...))
	}
}

func mappedEvidenceForSlot(ob generatedProofObligation, slot generatedProofSlot, legacySources []string, artifacts []certifyEvidenceArtifact) ([]string, string, string) {
	for _, art := range artifacts {
		for _, sat := range art.Satisfies {
			if strings.TrimSpace(sat.ProofObligationID) == ob.ID && strings.TrimSpace(sat.Slot) == slot.Kind {
				ref := firstNonEmpty(art.ID, art.Path)
				if ref == "" {
					ref = slot.Kind
				}
				return []string{ref}, "explicit", art.ID
			}
		}
	}
	for _, art := range artifacts {
		if deterministicArtifactMatch(ob, slot, art) {
			ref := firstNonEmpty(art.ID, art.Path)
			if ref == "" {
				ref = slot.Kind
			}
			return []string{ref}, "inferred", art.ID
		}
	}
	var out []string
	slotTokens := []string{
		strings.ToLower(slot.ID),
		strings.ToLower(slot.Kind),
		strings.ToLower(ob.ID),
		strings.ToLower(strings.TrimPrefix(ob.ID, "proof.")),
	}
	for _, src := range legacySources {
		lower := strings.ToLower(src)
		for _, tok := range slotTokens {
			if tok != "" && strings.Contains(lower, tok) {
				out = append(out, src)
				break
			}
		}
	}
	out = certifyDedupeStrings(out)
	if len(out) > 0 {
		return out, "inferred", ""
	}
	return nil, "", ""
}

func collectObligationEvidenceReasons(obligations []certifyObligation) []string {
	var out []string
	for _, ob := range obligations {
		for _, slot := range ob.SlotResults {
			if slotSide(slot.Kind) == "proof" {
				continue
			}
			switch slot.Status {
			case "missing_source":
				out = append(out, "missing_source:"+ob.ID+":"+slot.Kind)
			case "available_unmapped":
				out = append(out, "unmapped_evidence:"+ob.ID+":"+slot.Kind)
			}
		}
	}
	return certifyDedupeStrings(out)
}

func collectObligationProofReasons(obligations []certifyObligation) []string {
	var out []string
	for _, ob := range obligations {
		for _, slot := range ob.SlotResults {
			if slotSide(slot.Kind) != "proof" {
				continue
			}
			if slot.Status != "satisfied" && slot.Status != "optional" {
				out = append(out, ob.ID+":"+slot.Kind)
			}
		}
	}
	return certifyDedupeStrings(out)
}

func obligationsHaveMissingRequiredSlots(obligations []certifyObligation) bool {
	for _, ob := range obligations {
		if len(ob.MissingSlots) > 0 {
			return true
		}
	}
	return false
}

func obligationsHaveRuntimeLaneMissingSource(obligations []certifyObligation) bool {
	for _, ob := range obligations {
		if !obligationRequiresRuntimeLaneEvidence(ob) {
			continue
		}
		for _, slot := range ob.SlotResults {
			if slot.Status != "missing_source" {
				continue
			}
			if slotSide(slot.Kind) != "evidence" {
				continue
			}
			if slotKindNeedsRuntimeLane(slot.Kind) {
				return true
			}
		}
	}
	return false
}

func obligationsHaveUnmappedEvidence(obligations []certifyObligation) bool {
	for _, ob := range obligations {
		for _, slot := range ob.SlotResults {
			if slot.Status == "available_unmapped" {
				return true
			}
		}
	}
	return false
}

func runtimeEvidenceMissing(missingEvidence []string) bool {
	for _, item := range missingEvidence {
		item = strings.TrimSpace(item)
		if strings.Contains(item, "runtime") || strings.Contains(item, "log_artifact") || strings.Contains(item, "process_artifact") {
			return true
		}
	}
	return false
}

func obligationRequiresRuntimeLaneEvidence(ob certifyObligation) bool {
	switch strings.TrimSpace(ob.EvidenceLane) {
	case "runtime_required":
		return true
	case "hybrid":
		for _, slot := range ob.SlotResults {
			if slotKindNeedsRuntimeLane(slot.Kind) {
				return true
			}
		}
	}
	return false
}

func slotKindNeedsRuntimeLane(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "runtime", "process_artifact", "log_artifact", "failure_evidence", "test_or_runtime":
		return true
	default:
		return false
	}
}

func slotSide(kind string) string {
	switch strings.TrimSpace(kind) {
	case "static_guard", "scope_mapping", "input_validation", "negative_contract":
		return "proof"
	case "artifact", "before_after", "runtime", "process_artifact", "log_artifact", "failure_evidence", "test_or_runtime":
		return "evidence"
	default:
		return "proof"
	}
}

func benchmarkTruthyDefault(v any, fallback bool) bool {
	switch t := v.(type) {
	case nil:
		return fallback
	case bool:
		return t
	case string:
		s := strings.TrimSpace(strings.ToLower(t))
		if s == "" {
			return fallback
		}
		return s == "true"
	default:
		return fallback
	}
}

func benchmarkStringList(v any) []string {
	switch items := v.(type) {
	case []string:
		return certifyDedupeStrings(items)
	case []any:
		var out []string
		for _, item := range items {
			s := benchmarkString(item)
			if s != "" && s != "<nil>" {
				out = append(out, s)
			}
		}
		return certifyDedupeStrings(out)
	default:
		s := benchmarkPresentString(v)
		if s == "" {
			return nil
		}
		return []string{s}
	}
}

func benchmarkRefList(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return benchmarkStringList(v)
	}
	var out []string
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			s := benchmarkPresentString(m["ref"])
			if s != "" {
				out = append(out, s)
			}
			continue
		}
		s := benchmarkPresentString(item)
		if s != "" {
			out = append(out, s)
		}
	}
	return certifyDedupeStrings(out)
}

func benchmarkPresentString(v any) string {
	s := benchmarkString(v)
	if s == "<nil>" {
		return ""
	}
	return s
}

func renderCertifyText(res certifyResult) string {
	var b strings.Builder
	if res.EventID != "" {
		fmt.Fprintf(&b, "Event id: %s\n", res.EventID)
	}
	if res.Task != "" {
		fmt.Fprintf(&b, "Task: %s\n", res.Task)
	}
	fmt.Fprintf(&b, "Certification: %s\n", res.GovernanceCertification.Verdict)
	fmt.Fprintf(&b, "Promotion: %s\n", res.GovernanceCertification.Promotion)
	if res.Score != 0 {
		fmt.Fprintf(&b, "Score: %d\n", res.Score)
	}
	if res.RepairClaim.ID != "" {
		fmt.Fprintf(&b, "Repair claim id: %s\n", res.RepairClaim.ID)
	}
	if res.RepairClaim.Summary != "" {
		fmt.Fprintf(&b, "Repair claim: %s\n", res.RepairClaim.Summary)
	}
	if res.LegacyCertificationStatus != "" {
		fmt.Fprintf(&b, "Legacy certification status: %s\n", res.LegacyCertificationStatus)
	}
	fmt.Fprintf(&b, "Score used for certification: false\n")
	if len(res.RepairClaim.ContractIDs) > 0 {
		fmt.Fprintf(&b, "\nContracts:\n")
		for _, item := range res.RepairClaim.ContractIDs {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if len(res.GovernanceCertification.Obligations) > 0 {
		fmt.Fprintf(&b, "\nMatched obligations:\n")
		for _, ob := range res.GovernanceCertification.Obligations {
			fmt.Fprintf(&b, "  - %s (%s)\n", ob.ID, ob.Status)
			if ob.EvidenceLane != "" {
				fmt.Fprintf(&b, "    evidence_lane: %s\n", ob.EvidenceLane)
			}
			if len(ob.RequiredSlots) > 0 {
				fmt.Fprintf(&b, "    required_slots: %s\n", strings.Join(ob.RequiredSlots, ", "))
			}
			if len(ob.SatisfiedSlots) > 0 {
				fmt.Fprintf(&b, "    satisfied_slots: %s\n", strings.Join(ob.SatisfiedSlots, ", "))
			}
			if len(ob.MissingSlots) > 0 {
				fmt.Fprintf(&b, "    missing_slots: %s\n", strings.Join(ob.MissingSlots, ", "))
			}
			for _, slot := range ob.SlotResults {
				if slot.Status == "optional" {
					continue
				}
				fmt.Fprintf(&b, "    slot %s: %s", slot.Kind, slot.Status)
				if slot.MappingSource != "" {
					fmt.Fprintf(&b, " (%s", slot.MappingSource)
					if slot.ArtifactID != "" {
						fmt.Fprintf(&b, ", %s", slot.ArtifactID)
					}
					fmt.Fprintf(&b, ")")
				}
				fmt.Fprintf(&b, "\n")
			}
		}
	}
	if len(res.GovernanceCertification.Lanes) > 0 {
		fmt.Fprintf(&b, "\nLanes:\n")
		for _, lane := range res.GovernanceCertification.Lanes {
			fmt.Fprintf(&b, "  - %s: %s\n", lane.Lane, lane.Status)
			for _, reason := range lane.Reasons {
				fmt.Fprintf(&b, "    reason: %s\n", reason)
			}
		}
	}
	if len(res.GovernanceCertification.MissingEvidence) > 0 {
		fmt.Fprintf(&b, "\nMissing evidence:\n")
		for _, item := range res.GovernanceCertification.MissingEvidence {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if len(res.GovernanceCertification.BlockedByForbiddenMoveIDs) > 0 {
		fmt.Fprintf(&b, "\nBlocked by forbidden moves:\n")
		for _, item := range res.GovernanceCertification.BlockedByForbiddenMoveIDs {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if len(res.DetectedForbiddenMoves) > 0 {
		fmt.Fprintf(&b, "\nDetected forbidden moves:\n")
		for _, item := range res.DetectedForbiddenMoves {
			fmt.Fprintf(&b, "  - %s", item.ID)
			if item.Reason != "" {
				fmt.Fprintf(&b, ": %s", item.Reason)
			}
			if item.EvidenceKind != "" || item.EvidencePath != "" {
				fmt.Fprintf(&b, " [")
				if item.EvidenceKind != "" {
					fmt.Fprintf(&b, "%s", item.EvidenceKind)
				}
				if item.EvidencePath != "" {
					if item.EvidenceKind != "" {
						fmt.Fprintf(&b, " ")
					}
					fmt.Fprintf(&b, "%s", item.EvidencePath)
				}
				fmt.Fprintf(&b, "]")
			}
			fmt.Fprintf(&b, "\n")
		}
	}
	return b.String()
}

func availableArtifactsForSlot(ob generatedProofObligation, slot generatedProofSlot, artifacts []certifyEvidenceArtifact) []certifyEvidenceArtifact {
	var out []certifyEvidenceArtifact
	for _, art := range artifacts {
		if artifactExplicitlyTargetsSlot(art, ob.ID, slot.Kind) || artifactRelatedToObligation(ob, art) {
			out = append(out, art)
		}
	}
	return out
}

func compatibleArtifactsForSlot(ob generatedProofObligation, slot generatedProofSlot, artifacts []certifyEvidenceArtifact) []certifyEvidenceArtifact {
	var out []certifyEvidenceArtifact
	for _, art := range artifacts {
		if !artifactExplicitlyTargetsSlot(art, ob.ID, slot.Kind) && !artifactRelatedToObligation(ob, art) {
			continue
		}
		if !artifactKindCompatibleWithSlot(art.Kind, slot.Kind) {
			continue
		}
		out = append(out, art)
	}
	return out
}

func deterministicArtifactMatch(ob generatedProofObligation, slot generatedProofSlot, art certifyEvidenceArtifact) bool {
	if !artifactKindCompatibleWithSlot(art.Kind, slot.Kind) {
		return false
	}
	if artifactExplicitlyTargetsSlot(art, ob.ID, slot.Kind) {
		return true
	}
	return artifactRelatedToObligation(ob, art)
}

func artifactExplicitlyTargetsSlot(art certifyEvidenceArtifact, obligationID, slotKind string) bool {
	for _, sat := range art.Satisfies {
		if strings.TrimSpace(sat.ProofObligationID) == obligationID && strings.TrimSpace(sat.Slot) == slotKind {
			return true
		}
	}
	return false
}

func artifactRelatedToObligation(ob generatedProofObligation, art certifyEvidenceArtifact) bool {
	for _, ref := range art.RelatedProofObligationIDs {
		if strings.TrimSpace(ref) == ob.ID {
			return true
		}
	}
	for _, ref := range art.RelatedAuthoritySurfaceIDs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		for _, want := range ob.AppliesToAuthoritySurfaces {
			if ref == strings.TrimSpace(want) {
				return true
			}
		}
		if ref == strings.TrimSpace(ob.DerivedFromAuthoritySurface) {
			return true
		}
	}
	return false
}

func artifactKindCompatibleWithSlot(kind, slot string) bool {
	kind = strings.TrimSpace(kind)
	slot = strings.TrimSpace(slot)
	switch slot {
	case "log_artifact":
		return kind == "runtime_log"
	case "process_artifact":
		return kind == "process_snapshot"
	case "runtime":
		return kind == "runtime_log" || kind == "process_snapshot" || kind == "command_output"
	case "before_after":
		return kind == "before_after_artifact" || kind == "config_snapshot"
	case "failure_evidence":
		return kind == "failure_evidence"
	case "artifact":
		return kind == "patch" || kind == "config_snapshot" || kind == "before_after_artifact" || kind == "command_output"
	case "test_or_runtime":
		return kind == "test_output" || kind == "runtime_log" || kind == "process_snapshot"
	case "static_guard", "scope_mapping", "input_validation":
		return kind == "test_output" || kind == "command_output"
	case "negative_contract":
		return kind == "test_output" || kind == "command_output" || kind == "patch"
	default:
		return false
	}
}

func reasonIf(ok bool, reason string) []string {
	if !ok {
		return nil
	}
	return []string{reason}
}

func certifyDedupeStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}
