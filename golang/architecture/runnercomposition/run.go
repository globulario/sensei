// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"context"
	"fmt"
	"time"

	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// RequestPolicy carries the caller-supplied execution constraints
// providerport.Request needs that are not owned by synthesis.Session or
// synthesis.Plan: the precommitted deadline and observation limits O2's Run
// enforces. These are not part of any O1 schema, so whichever process is
// driving the O1 state machine supplies them here, once per attempt.
type RequestPolicy struct {
	// DeadlineAt must be RFC3339 -- providerport.Run parses it directly.
	DeadlineAt          string
	MaxObservationCount int
	MaxObservationBytes int
}

// Run is O3's governed runner: composes the O1 session (in PhaseAttempting)
// with O2's Run for exactly one OperationGeneration attempt, per
// docs/design/governed-runner-composition-o3.md's architectural-position
// sequence (snapshot -> workspace/buffer init -> provider construction ->
// O2 Run -> workspace freeze -> evidence computation -> verification ->
// sealing -> RunnerReceipt).
//
// sessionState.Phase must be synthesis.PhaseAttempting; any other phase is
// rejected before a snapshot is ever taken (hard law 4), with a plain Go
// error -- there is no disposition for "wrong phase," since no runner
// sequence stage was ever attempted. Every other outcome -- however far the
// sequence progressed -- returns a nil error alongside a fully populated,
// schema-and-semantically-valid RunnerReceipt (mirroring providerport.Run's
// own error-reservation convention: a non-nil error means no receipt could
// be built at all).
//
// repositoryRoot is the caller-owned local checkout the session's
// RepositoryDomain corresponds to; O3 does not resolve a repository domain
// to a filesystem location itself (out of scope). plan is the parent O1
// Plan this attempt extends -- Request.GenerationPayload. now supplies the
// wall-clock reading for CompletedAt and is threaded through to
// providerport.Run, mirroring its own now parameter, so tests are
// deterministic.
//
// RepositoryDomain/BaseRevision are sourced ONLY from sessionState.Session
// (hard law 3): Run builds the providerport.Request itself, from Session
// and plan, rather than accepting a caller-supplied Request whose embedded
// copies of these fields would need independent verification against
// Session -- there is exactly one source of truth for them, by
// construction.
func Run(
	ctx context.Context,
	sessionState synthesis.SessionState,
	repositoryRoot string,
	plan synthesis.Plan,
	factory GenerationProviderFactory,
	store CandidateArtifactStore,
	policy RequestPolicy,
	now func() time.Time,
) (RunnerReceipt, error) {
	if sessionState.Phase != synthesis.PhaseAttempting {
		return RunnerReceipt{}, fmt.Errorf("runnercomposition.Run: session phase %q is not %q -- rejected before any snapshot is taken", sessionState.Phase, synthesis.PhaseAttempting)
	}

	request, err := buildGenerationRequest(sessionState, plan, policy)
	if err != nil {
		return RunnerReceipt{}, fmt.Errorf("runnercomposition.Run: build request: %w", err)
	}

	receipt := RunnerReceipt{
		SchemaVersion:       RunnerReceiptSchemaVersion,
		ReceiptID:           "runner-receipt." + request.RequestID,
		RequestDigestSHA256: request.RequestDigestSHA256,
	}

	// Step 1: bounded, read-only snapshot at the session's exact
	// BaseRevision. ExtractSnapshot itself refuses anything other than a
	// full, verified commit object ID -- there is no fallback to HEAD or
	// the live working tree anywhere in this call.
	snapshotDir, _, inputDigest, snapshotCleanup, err := ExtractSnapshot(ctx, repositoryRoot, sessionState.Session.BaseRevision)
	if err != nil {
		return finalize(receipt, DispositionSnapshotFailure, "snapshot: "+err.Error(), nil, now)
	}

	cleanups := []func() error{snapshotCleanup}
	receipt.InputCandidateDigestSHA256 = &inputDigest

	// Step 2: ephemeral candidate buffer, a full copy of the snapshot,
	// bound to the snapshot's own manifest identity.
	snapshotManifest, err := BuildManifest(snapshotDir)
	if err != nil {
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionWorkspaceInitFailure, "rebuild snapshot manifest: "+err.Error(), &succeeded, now, detail)
	}
	bufferDir, _, _, bufferCleanup, err := InitializeCandidateBuffer(snapshotDir, snapshotManifest)
	if err != nil {
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionWorkspaceInitFailure, "initialize candidate buffer: "+err.Error(), &succeeded, now, detail)
	}
	cleanups = append(cleanups, bufferCleanup)

	// Step 3: CandidateWorkspace -- the typed, closable channel a provider
	// reads/writes through. Never O2's Execute signature, never ambient.
	workspace, err := newFSCandidateWorkspace(snapshotDir, bufferDir)
	if err != nil {
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionWorkspaceInitFailure, "construct candidate workspace: "+err.Error(), &succeeded, now, detail)
	}

	// Step 4: a fresh, workspace-bound Provider for this attempt alone --
	// never reused across attempts or sessions (hard law 5).
	provider, err := factory.NewProvider(workspace)
	if err != nil {
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionProviderConstructionFailure, "construct provider: "+err.Error(), &succeeded, now, detail)
	}

	// Step 5: one capability-driven O2 Provider.Execute call, via O2's Run,
	// reused verbatim, unchanged.
	result, _, o2Receipt, err := providerport.Run(ctx, provider, request, now)
	if err != nil {
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionO2RunError, "o2 run: "+err.Error(), &succeeded, now, detail)
	}
	resultDigest := result.ResultDigestSHA256
	o2ReceiptDigest := o2Receipt.ReceiptDigestSHA256
	receipt.ResultDigestSHA256 = &resultDigest
	receipt.O2ReceiptDigestSHA256 = &o2ReceiptDigest

	if result.TerminalOutcome != providerport.OutcomeCompleted {
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionO2NonCompleted, "o2 non-completed: "+string(result.TerminalOutcome)+": "+result.Detail, &succeeded, now, detail)
	}
	if result.GenerationPayload == nil {
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionO2NonCompleted, "o2 reported completed with a nil generation payload", &succeeded, now, detail)
	}
	attempt := *result.GenerationPayload

	// Step 6: O3 treats Result/O2 Receipt as immutable -- never rewritten,
	// whatever they say. Nothing below this point ever mutates result,
	// o2Receipt, attempt, or their referenced digests.

	// Step 7: freeze the workspace before verification. Any handle a
	// provider retained past this point fails closed.
	if closeErr := workspace.Close(); closeErr != nil {
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionWorkspaceFreezeFailure, "workspace close: "+closeErr.Error(), &succeeded, now, detail)
	}

	// Step 8: independently compute repository evidence -- never trusted
	// from the provider's own declared values (hard law 11).
	finalManifest, err := BuildManifest(bufferDir)
	if err != nil {
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionEvidenceComputationFailure, "build final manifest: "+err.Error(), &succeeded, now, detail)
	}
	finalDigest, err := ManifestDigest(finalManifest)
	if err != nil {
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionEvidenceComputationFailure, "compute final candidate content digest: "+err.Error(), &succeeded, now, detail)
	}
	proposedChangeDigest, err := GitChangeDigest(ctx, snapshotDir, bufferDir)
	if err != nil {
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionEvidenceComputationFailure, "compute proposed change digest: "+err.Error(), &succeeded, now, detail)
	}

	receipt.ProposedChangeDigestSHA256 = &proposedChangeDigest
	receipt.FinalCandidateContentDigestSHA256 = &finalDigest

	// A mismatch is a finding, not a stop: the design doc is explicit that
	// "the candidate is still sealed, as evidence of what the mismatch
	// actually was" -- digest-mismatch's own field-presence row requires
	// CandidateArtifact present, same as verified. The seal below always
	// uses O3's OWN independently computed digests, never the provider's
	// declared (possibly wrong) ones (hard law 11) -- a mismatched
	// candidate is sealed as exactly what it actually is, not repaired
	// into agreement with what the provider claimed.
	mismatched := attempt.InputCandidateDigestSHA256 != inputDigest || attempt.ProposedChangeDigestSHA256 != proposedChangeDigest
	var mismatchDetail string
	if mismatched {
		mismatchDetail = fmt.Sprintf("declared-vs-computed mismatch: input_candidate declared=%q computed=%q; proposed_change declared=%q computed=%q",
			attempt.InputCandidateDigestSHA256, inputDigest, attempt.ProposedChangeDigestSHA256, proposedChangeDigest)
	}

	// Step 9: seal the candidate before the ephemeral buffer is destroyed.
	artifact := CandidateArtifact{
		SchemaVersion:                     CandidateArtifactSchemaVersion,
		RepositoryDomain:                  sessionState.Session.RepositoryDomain,
		BaseRevision:                      sessionState.Session.BaseRevision,
		WorkspaceIdentityDigestSHA256:     sessionState.Session.WorkspaceIdentityDigestSHA256,
		SessionDigestSHA256:               sessionState.Session.SessionDigestSHA256,
		PlanDigestSHA256:                  plan.PlanDigestSHA256,
		PlanGeneration:                    attempt.PlanGeneration,
		AttemptNumber:                     attempt.AttemptNumber,
		InputCandidateDigestSHA256:        inputDigest,
		ProposedChangeDigestSHA256:        proposedChangeDigest,
		FinalCandidateContentDigestSHA256: finalDigest,
		Manifest:                          finalManifest,
	}
	artifactDigest, err := CandidateArtifactDigest(artifact)
	if err != nil {
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionSealFailure, "compute candidate artifact digest: "+err.Error(), &succeeded, now, detail)
	}
	artifact.CandidateArtifactDigestSHA256 = artifactDigest

	if err := store.Put(ctx, artifact); err != nil {
		// The design doc's matrix does not name a disposition for "seal
		// failed while sealing mismatch evidence" distinctly from an
		// ordinary seal-failure -- DispositionSealFailure's own required
		// field shape (Result/O2Receipt/InputCandidate/ProposedChange/
		// FinalCandidateContent present, CandidateArtifact nil) is exactly
		// what this branch already has regardless of mismatched, so it is
		// reused here for both. FailureDetail still records the mismatch
		// that was in progress, if any.
		detailPrefix := "seal candidate artifact: "
		if mismatched {
			detailPrefix = "seal candidate artifact (during mismatch evidence sealing, " + mismatchDetail + "): "
		}
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionSealFailure, detailPrefix+err.Error(), &succeeded, now, detail)
	}
	receipt.CandidateArtifactDigestSHA256 = &artifactDigest

	if mismatched {
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionDigestMismatch, mismatchDetail, &succeeded, now, detail)
	}

	// Step 10/11: verified, and the ephemeral capture surface is destroyed.
	succeeded, detail := runCleanupsTracked(cleanups)
	return finalize(receipt, DispositionVerified, "", &succeeded, now, detail)
}

// buildGenerationRequest constructs a schema-valid, correctly-digested
// providerport.Request for exactly one OperationGeneration attempt, sourced
// only from sessionState/plan/policy -- never from any caller-supplied
// Request, so there is exactly one place RepositoryDomain/BaseRevision/
// SessionDigestSHA256 can originate from (hard law 3).
func buildGenerationRequest(sessionState synthesis.SessionState, plan synthesis.Plan, policy RequestPolicy) (providerport.Request, error) {
	expectedPlanGeneration := plan.PlanGeneration
	expectedAttemptNumber := sessionState.ExpectedAttemptNumber

	request := providerport.Request{
		SchemaVersion:              providerport.RequestSchemaVersion,
		RequestID:                  fmt.Sprintf("request.%s.%d.%d", sessionState.Session.SessionID, expectedPlanGeneration, expectedAttemptNumber),
		Operation:                  providerport.OperationGeneration,
		SessionDigestSHA256:        sessionState.Session.SessionDigestSHA256,
		RepositoryDomain:           sessionState.Session.RepositoryDomain,
		BaseRevision:               sessionState.Session.BaseRevision,
		ParentArtifactDigestSHA256: plan.PlanDigestSHA256,
		ExpectedPlanGeneration:     &expectedPlanGeneration,
		ExpectedAttemptNumber:      &expectedAttemptNumber,
		DeadlineAt:                 policy.DeadlineAt,
		MaxObservationCount:        policy.MaxObservationCount,
		MaxObservationBytes:        policy.MaxObservationBytes,
		GenerationPayload:          &plan,
	}
	digest, err := providerport.RequestDigest(request)
	if err != nil {
		return providerport.Request{}, fmt.Errorf("compute request digest: %w", err)
	}
	request.RequestDigestSHA256 = digest
	return request, nil
}

// runCleanupsTracked runs every cleanup in cleanups and reports whether ALL
// of them succeeded, plus a combined detail message naming every failure --
// the ephemeral capture surface's own destruction outcome, independent of
// Disposition (hard law 6a / the design doc's CleanupSucceeded note).
func runCleanupsTracked(cleanups []func() error) (succeeded bool, detail string) {
	succeeded = true
	for i, c := range cleanups {
		if err := c(); err != nil {
			succeeded = false
			if detail != "" {
				detail += "; "
			}
			detail += fmt.Sprintf("cleanup[%d]: %v", i, err)
		}
	}
	return succeeded, detail
}

// finalize stamps disposition/failureDetail/cleanup fields onto receipt,
// sets CompletedAt, computes RunnerReceiptDigestSHA256, and returns the
// normalized, digest-complete RunnerReceipt. cleanupSucceeded is nil only
// for DispositionSnapshotFailure (the one caller that never runs any
// cleanup, since ExtractSnapshot's own contract guarantees nothing was left
// behind on its own failure -- there is structurally nothing to clean up
// yet, not merely "not attempted").
func finalize(receipt RunnerReceipt, disposition Disposition, failureDetail string, cleanupSucceeded *bool, now func() time.Time, cleanupFailureDetail ...string) (RunnerReceipt, error) {
	receipt.Disposition = disposition
	receipt.FailureDetail = failureDetail
	receipt.CleanupSucceeded = cleanupSucceeded
	if cleanupSucceeded != nil && !*cleanupSucceeded && len(cleanupFailureDetail) > 0 {
		receipt.CleanupFailureDetail = cleanupFailureDetail[0]
	}
	receipt.CompletedAt = now().UTC().Format(time.RFC3339)

	receipt = NormalizeRunnerReceipt(receipt)
	digest, err := RunnerReceiptDigest(receipt)
	if err != nil {
		return RunnerReceipt{}, fmt.Errorf("runnercomposition.Run: compute runner receipt digest: %w", err)
	}
	receipt.RunnerReceiptDigestSHA256 = digest

	if err := ValidateRunnerReceipt(receipt); err != nil {
		return RunnerReceipt{}, fmt.Errorf("runnercomposition.Run: internal error: constructed an invalid RunnerReceipt: %w", err)
	}
	return receipt, nil
}
