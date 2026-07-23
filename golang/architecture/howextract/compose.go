// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/factextract"
	"github.com/globulario/sensei/golang/architecture/gosemantics"
	"github.com/globulario/sensei/golang/architecture/investigation"
	"github.com/globulario/sensei/golang/extractor/importgraph"
)

const (
	SchemaVersion             = "investigation.schema.v1"
	GeneratedByIdentity       = "sensei.howextract"
	HowPlanID                 = "plan.how.v1"
	PostProcessingVersion     = "postprocess.v1"
	NondeterminismDeclaration = "deterministic_only"
	ExtractorProfileName      = "profile.how.v1"
)

type InvestigatorDefinition struct {
	ProviderID      string
	ProviderVersion string
	Category        investigation.EvidenceCategory
	Engine          string
}

type executionState string

const (
	executionComplete    executionState = "complete"
	executionPartial     executionState = "partial"
	executionUnavailable executionState = "unavailable"
)

var InvestigatorRegistry = []InvestigatorDefinition{
	{ProviderID: "topology_extractor", ProviderVersion: "1.0", Category: investigation.EvidenceSourceCode, Engine: "semantic"},
	{ProviderID: "flow_extractor", ProviderVersion: "1.0", Category: investigation.EvidenceSourceCode, Engine: "semantic"},
	{ProviderID: "state_extractor", ProviderVersion: "1.0", Category: investigation.EvidenceSourceCode, Engine: "ast"},
	{ProviderID: "boundary_extractor", ProviderVersion: "1.0", Category: investigation.EvidenceSourceCode, Engine: "semantic"},
	{ProviderID: "contract_extractor", ProviderVersion: "1.0", Category: investigation.EvidenceSourceCode, Engine: "semantic"},
	{ProviderID: "data_shape_extractor", ProviderVersion: "1.0", Category: investigation.EvidenceSourceCode, Engine: "semantic"},
	{ProviderID: "test_extractor", ProviderVersion: "1.0", Category: investigation.EvidenceTests, Engine: "semantic"},
}

func investigatorDefinition(id string) (InvestigatorDefinition, bool) {
	for _, definition := range InvestigatorRegistry {
		if definition.ProviderID == id {
			return definition, true
		}
	}
	return InvestigatorDefinition{}, false
}

type SourceSnapshotFile struct {
	Path         string `json:"path"`
	DigestSHA256 string `json:"digest_sha256"`
}

type SourceSnapshotManifestV1 struct {
	SchemaVersion    string               `json:"schema_version"`
	RepositoryDomain string               `json:"repository_domain"`
	Files            []SourceSnapshotFile `json:"files"`
}

type CoverageTargetV1 struct {
	SchemaVersion          string                         `json:"schema_version"`
	Mode                   investigation.Mode             `json:"mode"`
	ProviderID             string                         `json:"provider_id"`
	ProviderVersion        string                         `json:"provider_version"`
	Category               investigation.EvidenceCategory `json:"category"`
	RepositoryDomain       string                         `json:"repository_domain"`
	Scope                  string                         `json:"scope"`
	PlanDigestSHA256       string                         `json:"plan_digest_sha256"`
	ExtractorProfileDigest string                         `json:"extractor_profile_digest"`
}

type ExtractorProfileV1 struct {
	SchemaVersion        string   `json:"schema_version"`
	ProfileName          string   `json:"profile_name"`
	EnabledInvestigators []string `json:"enabled_investigators"`
	SourceSnapshotAlgo   string   `json:"source_snapshot_algo"`
}

func BuildSourceSnapshotManifest(root string, repoDomain string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	selected, err := gosemantics.SemanticInputFiles(absRoot)
	if err != nil {
		return "", err
	}

	var files []SourceSnapshotFile
	for _, path := range selected {
		rel, relErr := filepath.Rel(absRoot, path)
		if relErr != nil {
			return "", fmt.Errorf("relativize searched source file %s: %w", path, relErr)
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", readErr
		}

		files = append(files, SourceSnapshotFile{
			Path:         filepath.ToSlash(rel),
			DigestSHA256: sha256Hex(string(content)),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	manifest := SourceSnapshotManifestV1{
		SchemaVersion:    "manifest.v1",
		RepositoryDomain: repoDomain,
		Files:            files,
	}

	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}

	return sha256Hex(string(manifestData)), nil
}

func CalculatePlanDigest(plan investigation.Plan) (string, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	return sha256Hex(string(data)), nil
}

func CalculateProfileDigest(profile ExtractorProfileV1) (string, error) {
	data, err := json.Marshal(profile)
	if err != nil {
		return "", err
	}
	return sha256Hex(string(data)), nil
}

func CalculateTargetDigest(target CoverageTargetV1) (string, error) {
	data, err := json.Marshal(target)
	if err != nil {
		return "", err
	}
	return sha256Hex(string(data)), nil
}

func extractAll(root string, opts Options) (investigation.Document, error) {
	var limitations []architecture.Limitation

	repoDomain := opts.Repository.RepositoryDomain
	if repoDomain == "" {
		identity := factextract.ResolveRepositoryIdentity(root)
		repoDomain = identity.Domain
		opts.Repository.RepositoryDomain = repoDomain
	}
	if repoDomain == "" {
		return investigation.Document{}, fmt.Errorf("resolve repository identity: domain is unavailable")
	}

	// 1. Run Semantic Extractor
	semanticRes, semanticErr := gosemantics.Extract(root)
	if semanticErr != nil {
		limitations = append(limitations, architecture.Limitation{
			Source: "go_semantic_extractor", Scope: "repository", Reason: semanticErr.Error(), Blocking: false,
		})
	} else {
		for _, lim := range semanticRes.Limitations {
			limitations = append(limitations, architecture.Limitation{
				Source: "go_semantic_extractor", Scope: lim.Scope, Reason: lim.Reason, Blocking: false,
			})
		}
	}

	// 2. Run AST/Invariant Extractor
	astRes, astErr := factextract.Extract(root, factextract.Options{IncludeTests: true})
	if astErr != nil {
		limitations = append(limitations, architecture.Limitation{
			Source: "go_ast_extractor", Scope: "repository", Reason: astErr.Error(), Blocking: false,
		})
	} else {
		for _, lim := range astRes.Limitations {
			limitations = append(limitations, lim)
		}
	}

	// Composed observations
	var facts []architecture.Fact
	if semanticErr == nil {
		facts = append(facts, extractTopology(semanticRes.Observations)...)
		facts = append(facts, extractFlow(semanticRes.Observations)...)
		facts = append(facts, extractBoundaries(semanticRes.Observations)...)
		facts = append(facts, extractContracts(semanticRes.Observations)...)
		facts = append(facts, extractTests(semanticRes.Observations)...)
		facts = append(facts, extractDataShapes(semanticRes.Observations)...)
	}
	if astErr == nil {
		facts = append(facts, extractState(astRes.Facts)...)
	}

	// Ensure all facts are scoped to the bound repository domain
	for i := range facts {
		facts[i].Scope.Repository = repoDomain
	}

	// Normalize facts
	normalizedFacts, normErr := architecture.NormalizeFacts(root, facts)
	if normErr != nil {
		return investigation.Document{}, normErr
	}

	return composeReceiptsAndCoverage(root, normalizedFacts, repoDomain, opts, limitations, semanticErr, astErr)
}

func composeReceiptsAndCoverage(
	root string,
	normalizedFacts []architecture.Fact,
	repoDomain string,
	opts Options,
	initialLimitations []architecture.Limitation,
	semanticErr error,
	astErr error,
) (investigation.Document, error) {
	limitations := initialLimitations
	var rawEvidence []investigation.EvidenceReceipt
	discoveredByProvider := map[string]int{}
	captureFailuresByProvider := map[string]int{}

	for _, f := range normalizedFacts {
		if f.Evidence.SourceFile == "" {
			continue
		}
		definition, ok := investigatorDefinition(f.Extractor)
		if !ok {
			return investigation.Document{}, fmt.Errorf("unregistered HOW extractor %q", f.Extractor)
		}
		discoveredByProvider[definition.ProviderID]++

		fileSHA, err := architecture.SourceDigestSHA256(root, f.Evidence.SourceFile)
		if err != nil {
			captureFailuresByProvider[definition.ProviderID]++
			limitations = append(limitations, architecture.Limitation{Source: f.Extractor, Scope: f.Evidence.SourceFile, Reason: "source digest unavailable: " + err.Error(), Blocking: false})
			continue
		}

		lineStart := f.Evidence.LineStart
		lineEnd := f.Evidence.LineEnd
		if lineStart <= 0 {
			lineStart = 1
		}
		if lineEnd <= 0 {
			lineEnd = lineStart
		}

		capturedText, readErr := readCapturedLines(filepath.Join(root, f.Evidence.SourceFile), lineStart, lineEnd)
		if readErr != nil {
			captureFailuresByProvider[definition.ProviderID]++
			limitations = append(limitations, architecture.Limitation{Source: f.Extractor, Scope: f.Evidence.SourceFile, Reason: "source capture unavailable: " + readErr.Error(), Blocking: false})
			continue
		}

		contentSHA := sha256Hex(capturedText)
		receiptID := "evidence_" + sha256Hex(f.ID)[:16]
		component, _ := importgraph.ComponentForFile(f.Evidence.SourceFile)

		receipt := investigation.EvidenceReceipt{
			ID:                  receiptID,
			Category:            definition.Category,
			Provider:            investigation.ProviderBinding{ID: definition.ProviderID, Version: definition.ProviderVersion},
			ProofStrength:       investigation.ProofStaticSource,
			SourceIdentity:      f.Evidence.SourceFile,
			SourceDigestSHA256:  fileSHA,
			ContentDigestSHA256: contentSHA,
			CapturedContent:     capturedText,
			CapturedAt:          opts.CapturedAt,
			Scope: architecture.ClaimScope{
				Repository: repoDomain,
				Files:      []string{f.Evidence.SourceFile},
				Symbols:    f.Scope.Symbols,
				Components: []string{component},
			},
		}
		rawEvidence = append(rawEvidence, receipt)
	}

	dedupReceipts, err := deduplicateReceipts(rawEvidence)
	if err != nil {
		return investigation.Document{}, err
	}

	// 1. Plan Digest
	plan := investigation.Plan{
		ID:          HowPlanID,
		Description: "Phase 10.2 deterministic HOW extraction plan",
		Queries: []string{
			"topology_extractor",
			"flow_extractor",
			"state_extractor",
			"boundary_extractor",
			"contract_extractor",
			"data_shape_extractor",
			"test_extractor",
		},
	}
	planDigest, err := CalculatePlanDigest(plan)
	if err != nil {
		return investigation.Document{}, err
	}

	// 2. Extractor Profile Digest
	profile := ExtractorProfileV1{
		SchemaVersion: "profile.schema.v1",
		ProfileName:   ExtractorProfileName,
		EnabledInvestigators: []string{
			"topology_extractor",
			"flow_extractor",
			"state_extractor",
			"boundary_extractor",
			"contract_extractor",
			"data_shape_extractor",
			"test_extractor",
		},
		SourceSnapshotAlgo: "semantic-input-manifest.v1",
	}
	profileDigest, err := CalculateProfileDigest(profile)
	if err != nil {
		return investigation.Document{}, err
	}

	// 3. Source Snapshot Digest
	snapshotDigest, err := BuildSourceSnapshotManifest(root, repoDomain)
	if err != nil {
		return investigation.Document{}, fmt.Errorf("build source manifest: %w", err)
	}

	// 4. Coverage Entries
	semanticState, semanticReason := executionStateFor("semantic", semanticErr, limitations)
	stateState, stateReason := executionStateFor("ast", astErr, limitations)

	var coverage []investigation.CoverageEntry
	for _, inv := range InvestigatorRegistry {
		var status investigation.CoverageStatus
		var reason string
		var matchingReceiptIDs []string

		state := executionComplete
		reason = ""
		if inv.Engine == "semantic" {
			state = semanticState
			reason = semanticReason
		} else if inv.Engine == "ast" {
			state = stateState
			reason = stateReason
		}

		for _, rec := range dedupReceipts {
			if rec.Provider.ID == inv.ProviderID {
				matchingReceiptIDs = append(matchingReceiptIDs, rec.ID)
			}
		}

		switch state {
		case executionUnavailable:
			status = investigation.CoverageUnavailable
		case executionPartial:
			if len(matchingReceiptIDs) > 0 {
				status = investigation.CoverageSupporting
			} else {
				status = investigation.CoverageUnavailable
			}
		default:
			if discoveredByProvider[inv.ProviderID] > 0 && len(matchingReceiptIDs) == 0 && captureFailuresByProvider[inv.ProviderID] > 0 {
				status = investigation.CoverageUnavailable
				reason = "all discovered evidence failed capture"
			} else if len(matchingReceiptIDs) == 0 {
				status = investigation.CoverageNoResult
			} else {
				status = investigation.CoverageSupporting
			}
		}

		targetDesc := CoverageTargetV1{
			SchemaVersion:          "target.schema.v1",
			Mode:                   investigation.ModeHow,
			ProviderID:             inv.ProviderID,
			ProviderVersion:        inv.ProviderVersion,
			Category:               inv.Category,
			RepositoryDomain:       repoDomain,
			Scope:                  "repository",
			PlanDigestSHA256:       planDigest,
			ExtractorProfileDigest: profileDigest,
		}
		targetDigest, err := CalculateTargetDigest(targetDesc)
		if err != nil {
			return investigation.Document{}, err
		}

		var entryLimitations []architecture.Limitation
		for _, lim := range limitations {
			if lim.Source == inv.ProviderID || (inv.Engine == "semantic" && lim.Source == "go_semantic_extractor") || (inv.Engine == "ast" && lim.Source == "go_ast_extractor") {
				entryLimitations = append(entryLimitations, lim)
			}
		}

		entry := investigation.CoverageEntry{
			ProviderID:                 inv.ProviderID,
			ProviderVersion:            inv.ProviderVersion,
			Category:                   inv.Category,
			TargetDigestSHA256:         targetDigest,
			SourceSnapshotDigestSHA256: snapshotDigest,
			ResultEvidenceIDs:          matchingReceiptIDs,
			Status:                     status,
			Reason:                     reason,
			Limitations:                entryLimitations,
		}
		coverage = append(coverage, entry)
	}

	binding := investigation.Binding{
		Repository:                    opts.Repository,
		EvidenceSnapshotDigestSHA256:  "",
		InvestigationPlanDigestSHA256: planDigest,
		ExtractorProfileDigestSHA256:  profileDigest,
		Model: investigation.ModelBinding{
			Status: investigation.ModelStatusDisabled,
		},
	}

	receipt := investigation.RunReceipt{
		SchemaVersion:                SchemaVersion,
		GeneratedBy:                  GeneratedByIdentity,
		Repository:                   opts.Repository,
		GraphDigestSHA256:            opts.Repository.GraphDigestSHA256,
		PlanDigestSHA256:             planDigest,
		ExtractorProfileDigestSHA256: profileDigest,
		EvidenceSnapshotDigestSHA256: "",
		Model: investigation.ModelBinding{
			Status: investigation.ModelStatusDisabled,
		},
		ModelArtifactDigestSHA256: "",
		PostProcessingVersion:     PostProcessingVersion,
		TimestampSource:           opts.CapturedAt,
		ResourceLimits:            opts.ResourceLimits,
		NondeterminismDeclaration: NondeterminismDeclaration,
	}

	doc := investigation.Document{
		SchemaVersion: SchemaVersion,
		GeneratedBy:   GeneratedByIdentity,
		Mode:          investigation.ModeHow,
		Binding:       binding,
		Plan:          plan,
		Coverage:      coverage,
		RawEvidence:   dedupReceipts,
		Observations:  normalizedFacts,
		Limitations:   limitations,
		Receipt:       receipt,
	}

	normDoc, err := investigation.Normalize(doc)
	if err != nil {
		return investigation.Document{}, err
	}

	docDigest, err := investigation.CalculateDocumentDigest(normDoc)
	if err != nil {
		return investigation.Document{}, err
	}
	normDoc.Receipt.OutputDocumentDigestSHA256 = docDigest

	if err := investigation.Validate(normDoc); err != nil {
		return investigation.Document{}, fmt.Errorf("composed document fails validation: %w", err)
	}

	return normDoc, nil
}

func executionStateFor(engine string, executionErr error, limitations []architecture.Limitation) (executionState, string) {
	if executionErr != nil {
		return executionUnavailable, engine + " engine failed: " + executionErr.Error()
	}
	limitationSource := "go_" + engine + "_extractor"
	for _, limitation := range limitations {
		if limitation.Source == limitationSource {
			return executionPartial, engine + " engine completed partially"
		}
	}
	return executionComplete, ""
}

func readCapturedLines(filePath string, lineStart, lineEnd int) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	if lineStart < 1 || lineEnd < lineStart {
		return "", fmt.Errorf("invalid line range %d-%d", lineStart, lineEnd)
	}
	start, line := 0, 1
	for i, b := range data {
		if line == lineStart {
			start = i
			break
		}
		if b == '\n' {
			line++
		}
	}
	if line != lineStart {
		return "", fmt.Errorf("line %d unavailable", lineStart)
	}
	end := len(data)
	line = lineStart
	for i := start; i < len(data); i++ {
		if data[i] == '\n' {
			line++
			if line > lineEnd {
				end = i + 1
				break
			}
		}
	}
	return string(data[start:end]), nil
}

func sha256Hex(content string) string {
	h := sha256.New()
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))
}

func deduplicateReceipts(receipts []investigation.EvidenceReceipt) ([]investigation.EvidenceReceipt, error) {
	seen := make(map[string][]byte)
	var dedup []investigation.EvidenceReceipt
	for _, rec := range receipts {
		canonical, err := json.Marshal(rec)
		if err != nil {
			return nil, fmt.Errorf("canonicalize evidence receipt %s: %w", rec.ID, err)
		}
		if prior, ok := seen[rec.ID]; ok {
			if string(prior) != string(canonical) {
				return nil, fmt.Errorf("evidence receipt collision for %s", rec.ID)
			}
			continue
		}
		seen[rec.ID] = canonical
		dedup = append(dedup, rec)
	}
	return dedup, nil
}
