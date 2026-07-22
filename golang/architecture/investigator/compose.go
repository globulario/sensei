// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

// CandidateDigest computes a stable SHA256 digest binding candidate identity.
func CandidateDigest(ci CandidateIdentity) (string, error) {
	// Canonicalize scope files and symbols
	sort.Strings(ci.Scope.Files)
	sort.Strings(ci.Scope.Symbols)
	sort.Strings(ci.Scope.Components)

	descriptor := struct {
		Repository             string                  `json:"repository"`
		Proposition            string                  `json:"proposition"`
		Scope                  architecture.ClaimScope `json:"scope"`
		CandidateKind          string                  `json:"candidate_kind"`
		GeneratorVersion       string                  `json:"generator_version"`
		InputGraphDigest       string                  `json:"input_graph_digest"`
		EvidenceSnapshotDigest string                  `json:"evidence_snapshot_digest"`
	}{
		Repository:             ci.Repository,
		Proposition:            ci.Proposition,
		Scope:                  ci.Scope,
		CandidateKind:          ci.CandidateKind,
		GeneratorVersion:       ci.GeneratorVersion,
		InputGraphDigest:       ci.InputGraphDigest,
		EvidenceSnapshotDigest: ci.EvidenceSnapshotDigest,
	}

	bytes, err := json.Marshal(descriptor)
	if err != nil {
		return "", err
	}
	return investigation.SHA256Bytes(bytes), nil
}

// StableCandidateID generates a stable candidate ID prefix-bound to the kind.
func StableCandidateID(kind string, digest string) string {
	prefix := "candidate"
	switch kind {
	case "claim":
		prefix = "claim"
	case "question":
		prefix = "question"
	case "counterexample":
		prefix = "counterexample"
	}
	return fmt.Sprintf("candidate_%s_%s", prefix, digest[:12])
}

type defaultEngine struct {
	synthesizer   Synthesizer
	challenger    Challenger
	ranker        Ranker
	postProcessor PostProcessor
}

// NewEngine constructs a default Engine instance orchestrating synthesis, challenge, ranking, and validation.
func NewEngine(s Synthesizer, c Challenger, r Ranker, p PostProcessor) Engine {
	return &defaultEngine{
		synthesizer:   s,
		challenger:    c,
		ranker:        r,
		postProcessor: p,
	}
}

func (e *defaultEngine) Compose(ctx context.Context, input Input, opts Options) (investigation.Document, error) {
	// 1. Synthesize candidate claims and questions
	claims, questions, err := e.synthesizer.Synthesize(ctx, input, opts)
	if err != nil {
		return investigation.Document{}, fmt.Errorf("synthesis failed: %w", err)
	}

	graphDigest := input.Document.Binding.Repository.GraphDigestSHA256
	evidenceDigest := input.Document.Binding.EvidenceSnapshotDigestSHA256
	candidateDigests := make(map[string]string)

	// Assign stable IDs to candidate claims before challenging them
	for i, claim := range claims {
		digest, err := CandidateDigest(CandidateIdentity{
			Repository:             input.Document.Binding.Repository.RepositoryDomain,
			Proposition:            claim.Statement.Subject + "|" + claim.Statement.Predicate + "|" + claim.Statement.Object,
			Scope:                  claim.Scope,
			CandidateKind:          "claim",
			GeneratorVersion:       "1.0",
			InputGraphDigest:       graphDigest,
			EvidenceSnapshotDigest: evidenceDigest,
		})
		if err != nil {
			return investigation.Document{}, err
		}
		if claim.ID == "" {
			claim.ID = StableCandidateID("claim", digest)
			claims[i] = claim
		}
		candidateDigests[claim.ID] = digest
	}

	// 2. Challenger challenges candidate claims, producing counterexamples and adversarial questions
	counterexamples, challengeQuestions, err := e.challenger.Challenge(ctx, input, claims, opts)
	if err != nil {
		return investigation.Document{}, fmt.Errorf("adversarial challenge failed: %w", err)
	}
	questions = append(questions, challengeQuestions...)

	// 3. Rank candidates
	claims = e.ranker.RankClaims(ctx, claims, input)
	questions = e.ranker.RankQuestions(ctx, questions, input)

	// 4. Build output document
	doc := investigation.Document{
		SchemaVersion:      "investigation.schema.v1",
		GeneratedBy:        "sensei.investigator.v1",
		Mode:               investigation.ModeChallenge,
		Binding:            input.Document.Binding,
		Plan:               input.Document.Plan,
		Coverage:           input.Document.Coverage,
		RawEvidence:        input.Document.RawEvidence,
		Observations:       input.Document.Observations,
		CandidateClaims:    claims,
		CandidateQuestions: questions,
		Counterexamples:    counterexamples,
		Limitations:        input.Document.Limitations,
	}

	// Make sure captured_at is in RFC3339
	capturedAt := opts.CapturedAt
	if capturedAt == "" {
		capturedAt = time.Now().Format(time.RFC3339)
	}

	// 5. Compute candidate digests for questions and counterexamples

	for i, q := range doc.CandidateQuestions {
		digest, err := CandidateDigest(CandidateIdentity{
			Repository:             doc.Binding.Repository.RepositoryDomain,
			Proposition:            q.QuestionText,
			Scope:                  q.Scope,
			CandidateKind:          "question",
			GeneratorVersion:       "1.0",
			InputGraphDigest:       graphDigest,
			EvidenceSnapshotDigest: evidenceDigest,
		})
		if err != nil {
			return investigation.Document{}, err
		}
		if q.ID == "" {
			q.ID = StableCandidateID("question", digest)
			doc.CandidateQuestions[i] = q
		}
		candidateDigests[q.ID] = digest
	}

	for i, ce := range doc.Counterexamples {
		digest, err := CandidateDigest(CandidateIdentity{
			Repository:             doc.Binding.Repository.RepositoryDomain,
			Proposition:            ce.Description,
			Scope:                  ce.Scope,
			CandidateKind:          "counterexample",
			GeneratorVersion:       "1.0",
			InputGraphDigest:       graphDigest,
			EvidenceSnapshotDigest: evidenceDigest,
		})
		if err != nil {
			return investigation.Document{}, err
		}
		if ce.ID == "" {
			ce.ID = StableCandidateID("counterexample", digest)
			doc.Counterexamples[i] = ce
		}
		candidateDigests[ce.ID] = digest
	}

	doc.Receipt = BuildRunReceipt(&doc, candidateDigests, opts)

	// 6. Validate grounding before normalization
	if err := e.postProcessor.ValidateGrounding(ctx, doc, opts.Root); err != nil {
		return investigation.Document{}, fmt.Errorf("grounding validation failed: %w", err)
	}

	// 7. Normalize and compute document digest
	norm, err := investigation.Normalize(doc)
	if err != nil {
		return investigation.Document{}, fmt.Errorf("normalization failed: %w", err)
	}

	docDigest, err := investigation.CalculateDocumentDigest(norm)
	if err != nil {
		return investigation.Document{}, err
	}
	norm.Receipt.OutputDocumentDigestSHA256 = docDigest

	// Final validation check
	if err := investigation.Validate(norm); err != nil {
		return investigation.Document{}, fmt.Errorf("document validation failed: %w", err)
	}

	return norm, nil
}
