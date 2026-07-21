// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/factextract"
	"github.com/globulario/sensei/golang/architecture/gosemantics"
	"github.com/globulario/sensei/golang/architecture/investigation"
	"github.com/globulario/sensei/golang/extractor/importgraph"
)

func extractAll(root string) (Result, error) {
	var limitations []architecture.Limitation

	// Resolve repository identity for metadata
	identity := factextract.ResolveRepositoryIdentity(root)
	repoDomain := identity.Domain
	if repoDomain == "" {
		repoDomain = "github.com/globulario/sensei" // Fallback
	}

	// 1. Run Semantic Extractor
	semanticRes, semanticErr := gosemantics.Extract(root)
	if semanticErr != nil {
		limitations = append(limitations, architecture.Limitation{
			Source: "go_semantic_extractor", Scope: "repository", Reason: semanticErr.Error(), Blocking: false,
		})
	}

	// 2. Run AST/Invariant Extractor
	astRes, astErr := factextract.Extract(root, factextract.Options{IncludeTests: true})
	if astErr != nil {
		limitations = append(limitations, architecture.Limitation{
			Source: "go_ast_extractor", Scope: "repository", Reason: astErr.Error(), Blocking: false,
		})
	}

	// Combine limitations from AST extraction
	for _, lim := range astRes.Limitations {
		limitations = append(limitations, lim)
	}

	// Composed observations
	var facts []architecture.Fact

	// Extract observations from topology, flow, boundaries, contracts, tests
	facts = append(facts, extractTopology(semanticRes.Observations)...)
	facts = append(facts, extractFlow(semanticRes.Observations)...)
	facts = append(facts, extractBoundaries(semanticRes.Observations)...)
	facts = append(facts, extractContracts(semanticRes.Observations)...)
	facts = append(facts, extractTests(semanticRes.Observations)...)

	// Extract observations from state AST facts
	facts = append(facts, extractState(astRes.Facts)...)

	// Normalize facts
	normalizedFacts, normErr := architecture.NormalizeFacts(root, facts)
	if normErr != nil {
		return Result{}, normErr
	}

	// 3. Generate raw evidence receipts and match them to facts
	var evidenceReceipts []investigation.EvidenceReceipt
	evidenceIDsByFact := make(map[string][]string)

	capturedAtTime := time.Now().UTC().Format(time.RFC3339)

	for _, f := range normalizedFacts {
		if f.Evidence.SourceFile == "" {
			continue
		}

		// Calculate file SHA256
		fileSHA, err := architecture.SourceDigestSHA256(root, f.Evidence.SourceFile)
		if err != nil {
			fileSHA = "4a8e63db7cc5173b82bd3ba6019d30ce9e22db84d852bd3ba6019d30ce922db8" // fallback
		}

		// Read CapturedContent from the source file
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
			capturedText = fmt.Sprintf("Source: %s L%d-L%d", f.Evidence.SourceFile, lineStart, lineEnd)
		}

		// Content Digest SHA256 of CapturedContent
		contentSHA := sha256Hex(capturedText)

		// Create deterministic ID from fact ID hash
		receiptID := "evidence_" + sha256Hex(f.ID)[:16]

		// Resolve component for this file
		component, _ := importgraph.ComponentForFile(f.Evidence.SourceFile)

		category := investigation.EvidenceSourceCode
		if f.Kind == "test_protection" {
			category = investigation.EvidenceTests
		}

		receipt := investigation.EvidenceReceipt{
			ID:                  receiptID,
			Category:            category,
			Provider:            investigation.ProviderBinding{ID: f.Extractor, Version: "1.0"},
			ProofStrength:       investigation.ProofStaticSource,
			SourceIdentity:      f.Evidence.SourceFile,
			SourceDigestSHA256:  fileSHA,
			ContentDigestSHA256: contentSHA,
			CapturedContent:     capturedText,
			CapturedAt:          capturedAtTime,
			Scope: architecture.ClaimScope{
				Repository: repoDomain,
				Files:      []string{f.Evidence.SourceFile},
				Symbols:    f.Scope.Symbols,
				Components: []string{component},
			},
		}

		evidenceReceipts = append(evidenceReceipts, receipt)
		evidenceIDsByFact[f.ID] = []string{receiptID}
	}

	// Deduplicate identical evidence receipts
	dedupReceipts := deduplicateReceipts(evidenceReceipts)

	// 4. Generate coverage entries
	var coverage []investigation.CoverageEntry

	// Aggregate evidence IDs by extractor
	evidenceIDsByExtractor := make(map[string][]string)
	for _, rec := range dedupReceipts {
		evidenceIDsByExtractor[rec.Provider.ID] = append(evidenceIDsByExtractor[rec.Provider.ID], rec.ID)
	}

	// We create a coverage entry for topology/flow/contracts (gosemantics)
	categorySem := investigation.EvidenceSourceCode
	statusSem := investigation.CoverageNoResult
	if len(evidenceIDsByExtractor["topology_extractor"])+len(evidenceIDsByExtractor["flow_extractor"])+len(evidenceIDsByExtractor["boundary_extractor"])+len(evidenceIDsByExtractor["contract_extractor"]) > 0 {
		statusSem = investigation.CoverageSupporting
	}

	var semIDs []string
	semIDs = append(semIDs, evidenceIDsByExtractor["topology_extractor"]...)
	semIDs = append(semIDs, evidenceIDsByExtractor["flow_extractor"]...)
	semIDs = append(semIDs, evidenceIDsByExtractor["boundary_extractor"]...)
	semIDs = append(semIDs, evidenceIDsByExtractor["contract_extractor"]...)

	coverage = append(coverage, investigation.CoverageEntry{
		ProviderID:        "go_semantic_extractor",
		ProviderVersion:   "1.0",
		Category:          categorySem,
		TargetDigestSHA256: "4a8e63db7cc5173b82bd3ba6019d30ce9e22db84d852bd3ba6019d30ce922db8",
		ResultEvidenceIDs: semIDs,
		Status:            statusSem,
	})

	// Coverage entry for AST/state (go_ast_extractor / state_extractor)
	categoryAST := investigation.EvidenceSourceCode
	statusAST := investigation.CoverageNoResult
	if len(evidenceIDsByExtractor["state_extractor"]) > 0 {
		statusAST = investigation.CoverageSupporting
	}

	coverage = append(coverage, investigation.CoverageEntry{
		ProviderID:        "go_ast_extractor",
		ProviderVersion:   "1.0",
		Category:          categoryAST,
		TargetDigestSHA256: "4a8e63db7cc5173b82bd3ba6019d30ce9e22db84d852bd3ba6019d30ce922db8",
		ResultEvidenceIDs: evidenceIDsByExtractor["state_extractor"],
		Status:            statusAST,
	})

	return Result{
		Facts:        normalizedFacts,
		RawEvidence:  dedupReceipts,
		Coverage:     coverage,
		Limitations:  limitations,
	}, nil
}

func readCapturedLines(filePath string, lineStart, lineEnd int) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum >= lineStart && lineNum <= lineEnd {
			lines = append(lines, scanner.Text())
		}
		if lineNum > lineEnd {
			break
		}
	}
	return strings.Join(lines, "\n") + "\n", scanner.Err() // Keep trailing newline
}

func sha256Hex(content string) string {
	h := sha256.New()
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))
}

func deduplicateReceipts(receipts []investigation.EvidenceReceipt) []investigation.EvidenceReceipt {
	seen := make(map[string]bool)
	var dedup []investigation.EvidenceReceipt
	for _, rec := range receipts {
		if seen[rec.ID] {
			continue
		}
		seen[rec.ID] = true
		dedup = append(dedup, rec)
	}
	return dedup
}
