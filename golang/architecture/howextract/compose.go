// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/extractbudget"
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

// runMetrics carries what only the run itself can know: what it measured, and
// how it ended. It is a struct rather than more positional arguments so a
// future measurement cannot be silently dropped by a caller that forgot to
// thread it.
type runMetrics struct {
	consumption extractbudget.Consumption
	outcome     extractbudget.RunOutcome
}

func extractAll(ctx context.Context, root string, opts Options) (investigation.Document, error) {
	var limitations []architecture.Limitation
	var consumption extractbudget.Consumption
	var outcome extractbudget.RunOutcome

	repoDomain := opts.Repository.RepositoryDomain
	if repoDomain == "" {
		identity := factextract.ResolveRepositoryIdentity(root)
		repoDomain = identity.Domain
		opts.Repository.RepositoryDomain = repoDomain
	}
	if repoDomain == "" {
		return investigation.Document{}, fmt.Errorf("resolve repository identity: domain is unavailable")
	}

	// 1. Run Semantic Extractor. Its budget bounds which files may produce
	// observations, and the wall clock bounds the package load; it does not
	// narrow the load itself (see gosemantics.ExtractBounded for why loading
	// less would produce cheaper and wronger types).
	semanticRes, semanticErr := gosemantics.ExtractBounded(ctx, root, opts.Budget)
	consumption.Files = semanticRes.Selection.Consumption.Files
	consumption.SourceBytes = semanticRes.Selection.Consumption.SourceBytes
	consumption.Packages = semanticRes.Packages
	outcome.Cancelled = outcome.Cancelled || semanticRes.Cancelled
	outcome.WallClockExhausted = outcome.WallClockExhausted || semanticRes.WallClockExhausted
	// The dimensions that actually cut, as reported by the stage that cut.
	outcome.Truncated = append(outcome.Truncated, semanticRes.Selection.Truncated...)
	outcome.Truncated = append(outcome.Truncated, semanticRes.Truncated...)
	limitations = append(limitations, semanticRes.Selection.Limitations("go_semantic_extractor")...)
	// A scope that names nothing produced a "completed" run over zero
	// observations, which reads as evidence of absence. It is a degraded run:
	// the extractor finished, but it searched nothing it was asked to.
	outcome.Degraded = outcome.Degraded || semanticRes.Selection.ScopesMatchedNothing
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

	// The include/exclude scopes bind at the FACT level, not only at the
	// semantic extractor's file selection.
	//
	// This is not belt-and-braces. The AST/invariant extractor is a separate
	// owner with its own repository walk and no file-set parameter, so a run
	// that excluded a directory still produced state observations from inside
	// it -- caught by a test, not by reading the code. Filtering the composed
	// fact set is the one place every extractor's output passes through, so a
	// future extractor cannot reintroduce the leak by not knowing about
	// budgets.
	//
	// What this does NOT do is bound that extractor's COST: it still walks the
	// whole repository and its work is then discarded.
	//
	// Nor does the wall clock bound it. factextract.Extract takes no context,
	// so it cannot observe the deadline and can overrun it; the ceiling is
	// enforced at the stages that DO honour a context (the semantic package
	// load) and checked between stages, not inside this one. Saying "wall
	// clock bounds the AST pass" would be the comfortable version and is not
	// true.
	if len(opts.Budget.IncludePaths) > 0 || len(opts.Budget.ExcludePaths) > 0 {
		kept := facts[:0]
		dropped := 0
		for _, f := range facts {
			if f.Evidence.SourceFile != "" && !opts.Budget.InScope(f.Evidence.SourceFile) {
				dropped++
				continue
			}
			kept = append(kept, f)
		}
		facts = kept
		if dropped > 0 {
			limitations = append(limitations, architecture.Limitation{
				Source: "how_extraction_budget", Scope: "repository",
				Reason: fmt.Sprintf("%d observation(s) from files outside the bound include/exclude scopes were discarded; the repository outside those scopes is not described by this document",
					dropped),
				Blocking: false,
			})
		}
	}

	// Normalize facts
	normalizedFacts, normErr := architecture.NormalizeFacts(root, facts)
	if normErr != nil {
		return investigation.Document{}, normErr
	}

	// The observation ceiling binds AFTER normalization, on the normalized set
	// -- bounding the raw set would let deduplication decide how much of the
	// budget a run got, which would make the same repository produce different
	// coverage on a run where nothing about the budget changed.
	if opts.Budget.MaxObservations > 0 && len(normalizedFacts) > opts.Budget.MaxObservations {
		limitations = append(limitations, architecture.Limitation{
			Source: "how_extraction_budget", Scope: "repository",
			Reason: fmt.Sprintf("extraction budget reached (max_observations): %d of %d normalized observation(s) were kept",
				opts.Budget.MaxObservations, len(normalizedFacts)),
			Blocking: false,
		})
		normalizedFacts = normalizedFacts[:opts.Budget.MaxObservations]
		outcome.Truncated = append(outcome.Truncated, extractbudget.DimensionObservations)
	}
	consumption.Observations = len(normalizedFacts)

	return composeReceiptsAndCoverage(root, normalizedFacts, repoDomain, opts, limitations, semanticErr, astErr,
		runMetrics{consumption: consumption, outcome: outcome})
}

func composeReceiptsAndCoverage(
	root string,
	normalizedFacts []architecture.Fact,
	repoDomain string,
	opts Options,
	initialLimitations []architecture.Limitation,
	semanticErr error,
	astErr error,
	metrics runMetrics,
) (investigation.Document, error) {
	consumption, outcome := metrics.consumption, metrics.outcome
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
		component, _ := importgraph.ComponentForFile(f.Evidence.SourceFile, "go")

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

	// The evidence ceilings bind HERE, before coverage is computed from the
	// receipt set. Measuring them and reporting "budget_exhausted" without
	// having cut anything would be the same failure this whole checkpoint
	// exists to fix, only in the opposite direction: a receipt claiming a
	// limit constrained a run it never touched. The cut is taken in the
	// receipts' own stable order, and coverage is derived from the surviving
	// set, so a bounded run's coverage describes the evidence it actually
	// kept rather than evidence it discarded.
	dedupReceipts, droppedByProvider, evidenceCut, evidenceLimitations := boundEvidence(dedupReceipts, opts.Budget)
	limitations = append(limitations, evidenceLimitations...)
	outcome.Truncated = append(outcome.Truncated, evidenceCut...)

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
			} else if droppedByProvider[inv.ProviderID] > 0 && len(matchingReceiptIDs) == 0 {
				// It searched and it found; the budget discarded the result.
				// Reporting searched_no_result here would state, in the
				// document, that this provider looked and found nothing.
				status = investigation.CoverageSkipped
				reason = fmt.Sprintf("all %d discovered evidence receipt(s) were discarded by the extraction budget; this provider found results that are not in this document",
					droppedByProvider[inv.ProviderID])
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

	// Every run carries a budget receipt, bounded or not.
	//
	// The two cases stay distinguishable without needing the field's presence
	// to carry that meaning: an unbounded run's Budget is all zeros, which
	// says "no limit was bound" more directly than an absent field ever could,
	// and its measured Consumption still reports what the run actually did.
	// Making the receipt unconditional is also what lets the disposition be
	// unconditional -- "cancelled" and "unavailable" are facts about a run
	// that nobody set limits on just as much as one that had them.
	consumption.EvidenceReceipts = len(dedupReceipts)
	for _, rec := range dedupReceipts {
		consumption.CapturedContentBytes += int64(len(rec.CapturedContent))
	}
	outcome.Degraded = outcome.Degraded || semanticErr != nil || astErr != nil
	for _, lim := range limitations {
		if lim.Blocking {
			outcome.Degraded = true
		}
	}
	budgetReceipt := extractbudget.ComposeReceipt(opts.Budget, consumption, outcome)
	receipt.ResourceBudget = &budgetReceipt

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

// boundEvidence applies the evidence-receipt and captured-content ceilings,
// deterministically and at whole-receipt granularity. A half-captured receipt
// is not weaker evidence, it is evidence of something that was never observed:
// its content digest would no longer describe the source range it names.
func boundEvidence(receipts []investigation.EvidenceReceipt, budget extractbudget.Budget) ([]investigation.EvidenceReceipt, map[string]int, []string, []architecture.Limitation) {
	if budget.MaxEvidenceReceipts <= 0 && budget.MaxCapturedContentBytes <= 0 {
		return receipts, nil, nil, nil
	}
	kept := make([]investigation.EvidenceReceipt, 0, len(receipts))
	var bytes int64
	var reason string
	for i, rec := range receipts {
		size := int64(len(rec.CapturedContent))
		switch {
		case budget.MaxEvidenceReceipts > 0 && len(kept) >= budget.MaxEvidenceReceipts:
			reason = extractbudget.DimensionEvidenceReceipts
		case budget.MaxCapturedContentBytes > 0 && bytes+size > budget.MaxCapturedContentBytes:
			reason = extractbudget.DimensionCapturedContentBytes
		}
		if reason != "" {
			// Which providers lost evidence is not bookkeeping. Without it,
			// a provider whose receipts were all discarded would be reported
			// as "searched, no result" -- the document would say the provider
			// looked and found nothing, when it found something the budget
			// threw away. That is the exact class of untruth this contract
			// exists to remove, so the count travels to the coverage entry.
			dropped := map[string]int{}
			for _, lost := range receipts[i:] {
				dropped[lost.Provider.ID]++
			}
			return kept, dropped, []string{reason}, []architecture.Limitation{{
				Source: "how_extraction_budget", Scope: "repository",
				Reason: fmt.Sprintf("extraction budget reached (%s): %d of %d evidence receipt(s) were kept; the rest were discarded, beginning at %s",
					reason, len(kept), len(receipts), receipts[i].ID),
				Blocking: false,
			}}
		}
		kept = append(kept, rec)
		bytes += size
	}
	return kept, nil, nil, nil
}
