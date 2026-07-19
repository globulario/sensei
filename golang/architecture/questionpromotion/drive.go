// SPDX-License-Identifier: Apache-2.0

package questionpromotion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/globulario/sensei/golang/architecture/governedmutation"
	"github.com/globulario/sensei/golang/architecture/repograph"
	"github.com/globulario/sensei/golang/rdf"
)

// Journal payload shapes (small state worlds bound by the journal payload digest).
type preparedPayload struct {
	PromotionLineageID            string `json:"promotion_lineage_id"`
	CanonicalMutationDigestSHA256 string `json:"canonical_mutation_digest_sha256"`
	PreManifestDigestSHA256       string `json:"pre_manifest_digest_sha256"`
	GovernedNodeIRI               string `json:"governed_node_iri"`
}
type sourcePayload struct {
	CanonicalMutationDigestSHA256 string `json:"canonical_mutation_digest_sha256"`
	PostManifestDigestSHA256      string `json:"post_manifest_digest_sha256"`
}
type graphPayload struct {
	PostManifestDigestSHA256           string `json:"post_manifest_digest_sha256"`
	GraphBuildInputDigestSHA256        string `json:"graph_build_input_digest_sha256"`
	PersistedGraphByteDigestSHA256     string `json:"persisted_graph_byte_digest_sha256"`
	GraphSemanticDigestSHA256          string `json:"graph_semantic_digest_sha256"`
	MarkerDigestSHA256                 string `json:"marker_digest_sha256"`
	SupplementalProvenanceDigestSHA256 string `json:"supplemental_provenance_digest_sha256"`
}
type commitPayload struct {
	ReceiptDigestSHA256           string `json:"receipt_digest_sha256"`
	CommittedCausalIdentitySHA256 string `json:"committed_causal_identity_sha256"`
}

const receiptFileName = "receipt.json"

// drive runs (or resumes) the transaction from the journal head. It re-proves the
// durable world at each state and never trusts the journal payload alone.
func drive(ctx context.Context, req PromoteRequest, prepared QuestionPromotionReceipt, lineageID, promotionDir string, deps promoteDeps) (PromoteResult, error) {
	j := OpenJournal(promotionDir)

	// Acquire the single repository-scoped governed-mutation lock; hold it through
	// graph verification. Recovery reacquires it before re-touching the world.
	release, lerr := governedmutation.AcquireLock(ctx, req.RepositoryRoot, "promote", deps.now())
	if lerr != nil {
		return PromoteResult{Outcome: OutcomeManifestCASFailure, Detail: "acquire lock: " + lerr.Error()}, nil
	}
	defer release()

	head, headDigest, has, err := j.Head()
	if err != nil {
		return tampered(err)
	}

	// Ensure the prepared event exists (idempotent).
	if !has {
		pp := preparedPayload{PromotionLineageID: lineageID, CanonicalMutationDigestSHA256: prepared.CanonicalMutationDigestSHA256,
			PreManifestDigestSHA256: prepared.PreMutationManifestDigestSHA256, GovernedNodeIRI: prepared.GovernedNodeIRI}
		e, _, aerr := j.Append("", EventPrepared, pp, deps.now().Format(time.RFC3339))
		if aerr != nil {
			return tampered(aerr)
		}
		head, headDigest = e, e.EntryDigestSHA256
		if deps.stopAfter == EventPrepared {
			return PromoteResult{Outcome: OutcomeIncompleteAtSource, PromotionLineageID: lineageID}, nil
		}
	}

	// Load the FROZEN pre-mutation manifest from the prepared event so retries use
	// the first-run value (the current manifest is post-mutation after a crash).
	chain0, cerr := j.Verify()
	if cerr != nil || len(chain0) == 0 {
		return tampered(&JournalError{Code: "no_prepared", Detail: "prepared event missing"})
	}
	var pp preparedPayload
	if uerr := json.Unmarshal(chain0[0].Payload, &pp); uerr != nil {
		return tampered(&JournalError{Code: "malformed_prepared", Detail: uerr.Error()})
	}
	if pp.PromotionLineageID != lineageID || pp.GovernedNodeIRI != prepared.GovernedNodeIRI ||
		pp.CanonicalMutationDigestSHA256 != prepared.CanonicalMutationDigestSHA256 {
		return tampered(&JournalError{Code: "prepared_mismatch", Detail: "prepared world does not match the recomputed inputs"})
	}
	prepared.PreMutationManifestDigestSHA256 = pp.PreManifestDigestSHA256

	// Drive forward. Each state re-proves disk before advancing.
	for {
		switch head.EventType {
		case EventPrepared:
			e, out, serr := sourceCommit(req, prepared, j, headDigest, deps)
			if serr != nil {
				return tampered(serr)
			}
			if out != nil {
				return *out, nil
			}
			head, headDigest = e, e.EntryDigestSHA256
		case EventSourceCommitted:
			e, out, gerr := graphVerify(ctx, req, prepared, lineageID, j, headDigest, deps)
			if gerr != nil {
				return tampered(gerr)
			}
			if out != nil {
				return *out, nil
			}
			head, headDigest = e, e.EntryDigestSHA256
		case EventGraphVerified:
			return receiptCommit(ctx, req, prepared, lineageID, promotionDir, j, head, deps)
		case EventPromotionCommitted:
			return committedReplay(ctx, req, prepared, lineageID, promotionDir, j)
		default:
			return tampered(&JournalError{Code: "illegal_state", Detail: string(head.EventType)})
		}
	}
}

// sourceCommit applies the governed mutation under the held lock, re-proving the
// pre-manifest CAS, then records source_committed only after the exact mutation
// and post-manifest are recomputed.
func sourceCommit(req PromoteRequest, prepared QuestionPromotionReceipt, j *Journal, headDigest string, deps promoteDeps) (JournalEntry, *PromoteResult, error) {
	greq := governedmutation.Request{RepositoryRoot: req.RepositoryRoot, Proposal: req.Proposal}
	// Classify against the CURRENT source (recovery-aware): a replay means the
	// record was already applied by a prior crashed attempt.
	plan, perr := governedmutation.Plan(greq)
	if perr != nil {
		var ce *governedmutation.ContradictionError
		if errors.As(perr, &ce) {
			return incomplete(OutcomeContradiction, ce.Error())
		}
		return incomplete(OutcomeIncompleteAtSource, "plan: "+perr.Error())
	}
	if plan.MutationDigestSHA256 != prepared.CanonicalMutationDigestSHA256 {
		return incomplete(OutcomeContradiction, "mutation digest does not match the prepared world")
	}
	if plan.Disposition != governedmutation.DispositionReplay {
		// Not yet applied: verify the frozen pre-manifest CAS, then apply.
		pre, err := governedmutation.GovernedManifestDigest(req.RepositoryRoot)
		if err != nil {
			return incomplete(OutcomeManifestCASFailure, "pre-manifest: "+err.Error())
		}
		if pre != prepared.PreMutationManifestDigestSHA256 {
			return incomplete(OutcomeManifestCASFailure, "pre-mutation manifest changed since prepare")
		}
		greq.ExpectedManifestDigestSHA256 = pre
		applied, aerr := governedmutation.Apply(greq)
		if aerr != nil {
			var ce *governedmutation.ContradictionError
			var se *governedmutation.StaleManifestError
			switch {
			case errors.As(aerr, &ce):
				return incomplete(OutcomeContradiction, ce.Error())
			case errors.As(aerr, &se):
				return incomplete(OutcomeManifestCASFailure, se.Error())
			default:
				return incomplete(OutcomeIncompleteAtSource, "apply: "+aerr.Error())
			}
		}
		if applied.MutationDigestSHA256 != prepared.CanonicalMutationDigestSHA256 {
			return incomplete(OutcomeContradiction, "applied mutation digest does not match the prepared world")
		}
		if deps.afterSourceApply != nil {
			deps.afterSourceApply()
			return incomplete(OutcomeIncompleteAtSource, "crash injected after source apply")
		}
	}
	post, err := governedmutation.GovernedManifestDigest(req.RepositoryRoot)
	if err != nil {
		return incomplete(OutcomeManifestCASFailure, "post-manifest: "+err.Error())
	}
	e, _, jerr := j.Append(headDigest, EventSourceCommitted,
		sourcePayload{CanonicalMutationDigestSHA256: prepared.CanonicalMutationDigestSHA256, PostManifestDigestSHA256: post},
		deps.now().Format(time.RFC3339))
	if jerr != nil {
		return failEntry(jerr)
	}
	if deps.stopAfter == EventSourceCommitted {
		out := PromoteResult{Outcome: OutcomeIncompleteAtGraph, PromotionLineageID: prepared.PromotionLineageID}
		return e, &out, nil
	}
	return e, nil, nil
}

// graphVerify rebuilds the repository graph with the provenance supplemental and
// independently verifies it, then records graph_verified.
func graphVerify(ctx context.Context, req PromoteRequest, prepared QuestionPromotionReceipt, lineageID string, j *Journal, headDigest string, deps promoteDeps) (JournalEntry, *PromoteResult, error) {
	post, err := governedmutation.GovernedManifestDigest(req.RepositoryRoot)
	if err != nil {
		return incomplete(OutcomeManifestCASFailure, "post-manifest: "+err.Error())
	}
	prov := buildProvenanceInput(prepared, lineageID)
	vp, berr := repograph.Build(ctx, repograph.BuildRequest{
		RepositoryRoot: req.RepositoryRoot, RepositoryDomain: req.RepositoryDomain,
		ExpectedManifestDigestSHA256: post, Provenance: prov,
	})
	if berr != nil {
		var sm *repograph.StaleManifestError
		if errors.As(berr, &sm) {
			return incomplete(OutcomeManifestCASFailure, sm.Error())
		}
		return incomplete(OutcomeGraphVerificationFailure, berr.Error())
	}
	if deps.afterGraphBuild != nil {
		deps.afterGraphBuild()
		return incomplete(OutcomeIncompleteAtGraph, "crash injected after graph build")
	}
	e, _, jerr := j.Append(headDigest, EventGraphVerified, graphPayload{
		PostManifestDigestSHA256: post, GraphBuildInputDigestSHA256: vp.GraphBuildInputDigestSHA256,
		PersistedGraphByteDigestSHA256: vp.CompiledGraphByteDigestSHA256, GraphSemanticDigestSHA256: vp.GraphSemanticDigestSHA256,
		MarkerDigestSHA256: vp.MarkerDigestSHA256, SupplementalProvenanceDigestSHA256: vp.SupplementalProvenanceDigestSHA256,
	}, deps.now().Format(time.RFC3339))
	if jerr != nil {
		return failEntry(jerr)
	}
	if deps.stopAfter == EventGraphVerified {
		out := PromoteResult{Outcome: OutcomeIncompleteAtCommit, PromotionLineageID: lineageID}
		return e, &out, nil
	}
	return e, nil, nil
}

// receiptCommit re-proves the verified graph world, constructs+persists the final
// receipt, then appends promotion_committed under CAS. Crash between receipt
// persistence and the commit append reconciles to exactly one commit.
func receiptCommit(ctx context.Context, req PromoteRequest, prepared QuestionPromotionReceipt, lineageID, promotionDir string, j *Journal, gvHead JournalEntry, deps promoteDeps) (PromoteResult, error) {
	// Re-prove the current governed manifest and persisted graph still match the
	// graph_verified world (a later governed mutation must not make this authoritative).
	var gp graphPayload
	_ = json.Unmarshal(gvHead.Payload, &gp)
	cur, err := governedmutation.GovernedManifestDigest(req.RepositoryRoot)
	if err != nil || cur != gp.PostManifestDigestSHA256 {
		return PromoteResult{Outcome: OutcomeManifestCASFailure, Detail: "governed manifest changed after graph_verified", PromotionLineageID: lineageID}, nil
	}
	reloaded, verr := repograph.VerifyPersisted(ctx, req.RepositoryRoot)
	if verr != nil {
		return PromoteResult{Outcome: OutcomeGraphVerificationFailure, Detail: verr.Error(), PromotionLineageID: lineageID}, nil
	}
	if reloaded.CompiledGraphByteDigestSHA256 != gp.PersistedGraphByteDigestSHA256 ||
		reloaded.GraphSemanticDigestSHA256 != gp.GraphSemanticDigestSHA256 {
		return PromoteResult{Outcome: OutcomeGraphVerificationFailure, Detail: "persisted graph differs from graph_verified", PromotionLineageID: lineageID}, nil
	}
	// Prove the governed node + provenance chain are present in the reloaded graph.
	if perr := repograph.VerifyProvenance(ctx, req.RepositoryRoot, buildProvenanceInput(prepared, lineageID)); perr != nil {
		return PromoteResult{Outcome: OutcomeGraphVerificationFailure, Detail: perr.Error(), PromotionLineageID: lineageID}, nil
	}

	// Construct the final receipt binding the verified projection.
	rc := prepared
	rc.PostMutationManifestDigestSHA256 = gp.PostManifestDigestSHA256
	rc.GraphBuildInputDigestSHA256 = gp.GraphBuildInputDigestSHA256
	rc.PersistedGraphByteDigestSHA256 = gp.PersistedGraphByteDigestSHA256
	rc.GraphSemanticDigestSHA256 = gp.GraphSemanticDigestSHA256
	rc.MarkerDigestSHA256 = gp.MarkerDigestSHA256
	rc.MarkerIRI = reloaded.MarkerIRI
	rc.ProjectionProducerID = reloaded.ProducerID
	rc.PromotedAt = gvHead.ProducedAt // ledger-anchored to the graph_verified event

	rc.ReceiptDigestSHA256 = ""
	receiptDigest, derr := Digest(rc)
	if derr != nil {
		return PromoteResult{}, derr
	}
	rc.ReceiptDigestSHA256 = receiptDigest

	// Deterministic committed causal identity from the fixed graph_verified head.
	committed := committedCausalIdentity(lineageID, receiptDigest, gvHead.EntryDigestSHA256, gvHead.ProducedAt)
	rc.CommittedCausalIdentitySHA256 = committed
	if verr := Validate(rc); verr != nil {
		return PromoteResult{Outcome: OutcomeIncompleteAtCommit, Detail: "final receipt invalid: " + verr.Error(), PromotionLineageID: lineageID}, nil
	}

	// Durably persist the authoritative receipt BEFORE the commit event.
	receiptPath := filepath.Join(promotionDir, receiptFileName)
	if !receiptMatches(receiptPath, rc) {
		out, merr := json.MarshalIndent(rc, "", "  ")
		if merr != nil {
			return PromoteResult{}, merr
		}
		if werr := writeFileAtomic(receiptPath, out); werr != nil {
			return PromoteResult{Outcome: OutcomeIncompleteAtCommit, Detail: "persist receipt: " + werr.Error(), PromotionLineageID: lineageID}, nil
		}
	}
	if deps.afterReceiptPersist != nil {
		deps.afterReceiptPersist() // simulate a crash before the commit append
		return PromoteResult{Outcome: OutcomeIncompleteAtCommit, PromotionLineageID: lineageID, ReceiptDigestSHA256: receiptDigest}, nil
	}

	// Append promotion_committed under CAS on the graph_verified head.
	if _, _, jerr := j.Append(gvHead.EntryDigestSHA256, EventPromotionCommitted,
		commitPayload{ReceiptDigestSHA256: receiptDigest, CommittedCausalIdentitySHA256: committed},
		gvHead.ProducedAt); jerr != nil {
		return tamperedResult(jerr, lineageID)
	}
	if _, cerr := proveCommittedConjunction(ctx, req.RepositoryRoot, lineageID, promotionDir, j); cerr != nil {
		return PromoteResult{Outcome: OutcomeIncompleteAtCommit, Detail: cerr.Error(), PromotionLineageID: lineageID}, nil
	}
	return PromoteResult{Outcome: OutcomeCommitted, PromotionLineageID: lineageID, ReceiptDigestSHA256: receiptDigest,
		CommittedCausalIdentitySHA256: committed, Receipt: &rc}, nil
}

// committedReplay returns the already-committed authoritative receipt without
// writing, after re-proving the full conjunctive authority.
func committedReplay(ctx context.Context, req PromoteRequest, prepared QuestionPromotionReceipt, lineageID, promotionDir string, j *Journal) (PromoteResult, error) {
	rc, cerr := proveCommittedConjunction(ctx, req.RepositoryRoot, lineageID, promotionDir, j)
	if cerr != nil {
		return PromoteResult{Outcome: OutcomeTamperedJournal, Detail: cerr.Error(), PromotionLineageID: lineageID}, nil
	}
	return PromoteResult{Outcome: OutcomeExactReplay, PromotionLineageID: lineageID, ReceiptDigestSHA256: rc.ReceiptDigestSHA256,
		CommittedCausalIdentitySHA256: rc.CommittedCausalIdentitySHA256, Receipt: &rc}, nil
}

func buildProvenanceInput(prepared QuestionPromotionReceipt, lineageID string) *repograph.PromotionProvenance {
	nt := ProvenanceTriples(prepared)
	receipt := ReceiptNodeIRI(prepared)
	edges := []repograph.ProvenanceEdge{
		{Subject: prepared.GovernedNodeIRI, Predicate: rdf.PropPromotedVia, Object: receipt},
		{Subject: receipt, Predicate: rdf.PropRecordsDisposition, Object: DispositionNodeIRI(prepared)},
		{Subject: DispositionNodeIRI(prepared), Predicate: rdf.PropResolvesAnswer, Object: AnswerNodeIRI(prepared)},
		{Subject: AnswerNodeIRI(prepared), Predicate: rdf.PropAnswersQuestion, Object: QuestionNodeIRI(prepared)},
		{Subject: QuestionNodeIRI(prepared), Predicate: rdf.PropRaisedForTask, Object: TaskNodeIRI(prepared)},
		{Subject: TaskNodeIRI(prepared), Predicate: rdf.PropInSession, Object: SessionNodeIRI(prepared)},
		{Subject: receipt, Predicate: rdf.PropForResult, Object: ResultNodeIRI(prepared)},
	}
	return &repograph.PromotionProvenance{
		ID: "promotion." + lineageID, Version: "v1", NTriples: nt,
		ExpectedEdges:  edges,
		ConflictGuards: []repograph.ProvenanceEdge{{Subject: prepared.GovernedNodeIRI, Predicate: rdf.PropPromotedVia, Object: receipt}},
	}
}

func committedCausalIdentity(lineageID, receiptDigest, graphVerifiedEntryDigest, causalTime string) string {
	h := sha256.Sum256([]byte("questionpromotion.committed/v1\x00" + lineageID + "\x00" + receiptDigest + "\x00" + graphVerifiedEntryDigest + "\x00" + causalTime))
	return hex.EncodeToString(h[:])
}

func receiptMatches(path string, rc QuestionPromotionReceipt) bool {
	existing, err := loadReceipt(path)
	if err != nil {
		return false
	}
	return existing.ReceiptDigestSHA256 == rc.ReceiptDigestSHA256
}

func loadReceipt(path string) (QuestionPromotionReceipt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return QuestionPromotionReceipt{}, err
	}
	var rc QuestionPromotionReceipt
	if err := json.Unmarshal(data, &rc); err != nil {
		return QuestionPromotionReceipt{}, err
	}
	return rc, nil
}

// ── small typed-return helpers ──────────────────────────────────────────────

func incomplete(o Outcome, detail string) (JournalEntry, *PromoteResult, error) {
	out := PromoteResult{Outcome: o, Detail: detail}
	return JournalEntry{}, &out, nil
}
func failEntry(err error) (JournalEntry, *PromoteResult, error) {
	return JournalEntry{}, nil, err
}
func tampered(err error) (PromoteResult, error) {
	return PromoteResult{Outcome: OutcomeTamperedJournal, Detail: err.Error()}, nil
}
func tamperedResult(err error, lineageID string) (PromoteResult, error) {
	return PromoteResult{Outcome: OutcomeTamperedJournal, Detail: err.Error(), PromotionLineageID: lineageID}, nil
}
