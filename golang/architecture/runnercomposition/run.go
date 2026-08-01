// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"context"
	"fmt"
	"time"

	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/synthesis"
	"github.com/globulario/sensei/golang/architecture/workspacecontract"
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

// extractSnapshotFn/initializeCandidateBufferFn/newCandidateWorkspaceFn/
// buildFinalManifestFn are indirections over this package's own real,
// already-hardened functions -- Run calls through these package variables
// rather than the functions directly, purely so a test can substitute a
// controlled failure for one internal step without needing to fabricate an
// external condition (filesystem permissions, TMPDIR, ...) that would
// necessarily also break an EARLIER step (every earlier step already uses
// the same real function, so sabotaging it externally cannot be isolated to
// one specific later stage). Production code never reassigns these; they
// default to, and always call through to, the real functions below.
var (
	extractSnapshotFn           = ExtractSnapshot
	initializeCandidateBufferFn = InitializeCandidateBuffer
	newCandidateWorkspaceFn     = newFSCandidateWorkspace
	buildFinalManifestFn        = BuildManifest
)

// Run is O3's governed runner: composes the O1 session (in PhaseAttempting)
// with O2's Run for exactly one OperationGeneration attempt, per
// docs/design/governed-runner-composition-o3.md's architectural-position
// sequence (snapshot -> workspace/buffer init -> provider construction ->
// O2 Run -> workspace freeze -> evidence computation -> verification ->
// sealing -> RunnerReceipt).
//
// Four checks are rejected with a plain Go error before any snapshot is
// ever taken -- there is no disposition for any of them, since no runner
// sequence stage was ever attempted:
//
//   - sessionState.Phase must be synthesis.PhaseAttempting (hard law 4);
//   - identity must be the EXACT workspacecontract.Identity
//     sessionState.Session.WorkspaceIdentityDigestSHA256 references (a
//     fresh recomputation must match, not merely a caller's own account of
//     it -- the same "declared must equal recomputed" law applied
//     everywhere else in this codebase), and its Binding.RepositoryDomain/
//     Binding.Revision must agree with Session's own copies -- the
//     canonical workspace identity owner is what repositoryRoot's
//     authority actually traces to, not a bare, unverified caller string;
//   - plan must be the session's CURRENT accepted plan: its digest must
//     equal sessionState.LatestPlanDigestSHA256 and its PlanGeneration must
//     equal sessionState.PlanGeneration -- O1 itself treats those
//     SessionState fields as the current accepted plan authority, so a
//     caller cannot substitute a different, unrelated (even if internally
//     valid) Plan.
//
// Every other outcome -- however far the sequence progressed -- returns a
// nil error alongside a fully populated, schema-and-semantically-valid
// RunnerReceipt (mirroring providerport.Run's own error-reservation
// convention: a non-nil error means no receipt could be built at all).
//
// repositoryRoot is the caller-owned local checkout the session's
// RepositoryDomain corresponds to; O3 does not resolve a repository domain
// to a filesystem location itself (out of scope) -- identity's own
// verification above is what binds repositoryRoot's use to the canonical
// workspace owner; O3 has no independent way to confirm repositoryRoot's
// physical bytes correspond to that domain (workspacecontract.Identity
// carries no filesystem path field, by design -- see its package doc
// comment on why git remote origin is never a source of governed identity).
// now supplies the wall-clock reading for CompletedAt and is threaded
// through to providerport.Run, mirroring its own now parameter, so tests
// are deterministic.
//
// RepositoryDomain/BaseRevision are sourced ONLY from sessionState.Session
// (hard law 3): Run builds the providerport.Request itself, from Session
// and plan, rather than accepting a caller-supplied Request whose embedded
// copies of these fields would need independent verification against
// Session -- there is exactly one source of truth for them, by
// construction.
//
// Workspace revocation (CandidateWorkspace.Close, hard law 6) is attempted
// on EVERY path once a workspace has been constructed -- not only the path
// that reaches a completed O2 result. A provider that retained a handle
// across a construction failure, an O2 hard error, or an O2 non-completion
// must still find every further call failing closed; skipping Close on
// those earlier-failure paths would let such a handle silently keep
// working. Only once O2 reports a completed result does Close become its
// own dedicated, disposition-defining step (workspace-freeze-failure) --
// on every earlier failure path, Close's own outcome is folded into that
// path's existing CleanupSucceeded/CleanupFailureDetail aggregate instead,
// since the design doc names no disposition for "construction/O2 failed
// AND the resulting close also failed" distinctly from the failure that
// was already occurring. On those folded paths, Close always runs FIRST,
// before either directory's removal -- a retained or still-unwinding
// provider handle must be revoked before its backing directories can be
// removed out from under it, never concurrently with or after.
func Run(
	ctx context.Context,
	sessionState synthesis.SessionState,
	identity workspacecontract.Identity,
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

	identityDigest, err := workspacecontract.IdentityDigest(identity)
	if err != nil {
		return RunnerReceipt{}, fmt.Errorf("runnercomposition.Run: compute workspace identity digest: %w", err)
	}
	if identityDigest != sessionState.Session.WorkspaceIdentityDigestSHA256 {
		return RunnerReceipt{}, fmt.Errorf("runnercomposition.Run: identity's actual digest %q does not match session's workspace_identity_digest_sha256 %q -- rejected before any snapshot is taken", identityDigest, sessionState.Session.WorkspaceIdentityDigestSHA256)
	}
	if identity.Binding.RepositoryDomain != sessionState.Session.RepositoryDomain {
		return RunnerReceipt{}, fmt.Errorf("runnercomposition.Run: identity.Binding.RepositoryDomain %q does not match session.RepositoryDomain %q", identity.Binding.RepositoryDomain, sessionState.Session.RepositoryDomain)
	}
	if identity.Binding.Revision == nil || *identity.Binding.Revision != sessionState.Session.BaseRevision {
		return RunnerReceipt{}, fmt.Errorf("runnercomposition.Run: identity.Binding.Revision does not match session.BaseRevision %q", sessionState.Session.BaseRevision)
	}

	planDigest, err := synthesis.PlanDigest(plan)
	if err != nil {
		return RunnerReceipt{}, fmt.Errorf("runnercomposition.Run: compute plan digest: %w", err)
	}
	// plan's own declared digest must equal this fresh recomputation --
	// the same "declared must equal recomputed" self-consistency law
	// applied everywhere else in this codebase, checked BEFORE the
	// authority check below. Without it, a plan whose CONTENT genuinely is
	// the accepted one (so the recomputed-vs-session check below would
	// pass) but whose own PlanDigestSHA256 field is stale or wrong would
	// still poison buildGenerationRequest's ParentArtifactDigestSHA256 and
	// the sealed CandidateArtifact.PlanDigestSHA256 downstream, both of
	// which use plan.PlanDigestSHA256 directly, not the recomputed value.
	if plan.PlanDigestSHA256 != planDigest {
		return RunnerReceipt{}, fmt.Errorf("runnercomposition.Run: plan's declared digest %q does not match its own recomputed content digest %q", plan.PlanDigestSHA256, planDigest)
	}
	if planDigest != sessionState.LatestPlanDigestSHA256 {
		return RunnerReceipt{}, fmt.Errorf("runnercomposition.Run: plan's actual digest %q does not match sessionState.LatestPlanDigestSHA256 %q -- plan is not the session's currently accepted plan", planDigest, sessionState.LatestPlanDigestSHA256)
	}
	if plan.PlanGeneration != sessionState.PlanGeneration {
		return RunnerReceipt{}, fmt.Errorf("runnercomposition.Run: plan.PlanGeneration %d does not match sessionState.PlanGeneration %d", plan.PlanGeneration, sessionState.PlanGeneration)
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
	snapshotDir, snapshotManifest, inputDigest, snapshotCleanup, err := extractSnapshotFn(ctx, repositoryRoot, sessionState.Session.BaseRevision)
	if err != nil {
		return finalize(receipt, DispositionSnapshotFailure, "snapshot: "+err.Error(), nil, now)
	}

	cleanups := []func() error{snapshotCleanup}
	receipt.InputCandidateDigestSHA256 = &inputDigest

	// Step 2: ephemeral candidate buffer, a full copy of the snapshot,
	// bound to the EXACT manifest ExtractSnapshot itself returned -- never
	// rebuilt from disk. Rebuilding would re-read snapshotDir independently
	// of the bytes InputCandidateDigestSHA256 was actually computed over;
	// any divergence between the two reads (however it could arise) would
	// silently present mutated bytes under the original, already-sealed
	// digest.
	bufferDir, _, _, bufferCleanup, err := initializeCandidateBufferFn(snapshotDir, snapshotManifest)
	if err != nil {
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionWorkspaceInitFailure, "initialize candidate buffer: "+err.Error(), &succeeded, now, detail)
	}
	cleanups = append(cleanups, bufferCleanup)

	// Step 3: CandidateWorkspace -- the typed, closable channel a provider
	// reads/writes through. Never O2's Execute signature, never ambient.
	workspace, err := newCandidateWorkspaceFn(snapshotDir, bufferDir)
	if err != nil {
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionWorkspaceInitFailure, "construct candidate workspace: "+err.Error(), &succeeded, now, detail)
	}

	// Step 4: a fresh, workspace-bound Provider for this attempt alone --
	// never reused across attempts or sessions (hard law 5).
	provider, err := factory.NewProvider(workspace)
	if err != nil {
		// The workspace itself was already constructed above even though
		// the factory failed to build a Provider from it -- close it too
		// (hard law 6: any handle a provider retained fails closed), folded
		// into this path's own cleanup aggregate rather than a dedicated
		// disposition, since none is named for this combination.
		cleanups = append([]func() error{workspace.Close}, cleanups...) // Close must run BEFORE directory removal, not after.
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionProviderConstructionFailure, "construct provider: "+err.Error(), &succeeded, now, detail)
	}

	// Step 5: one capability-driven O2 Provider.Execute call, via O2's Run,
	// reused verbatim, unchanged.
	result, _, o2Receipt, err := providerport.Run(ctx, provider, request, now)
	if err != nil {
		cleanups = append([]func() error{workspace.Close}, cleanups...) // Close must run BEFORE directory removal, not after.
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionO2RunError, "o2 run: "+err.Error(), &succeeded, now, detail)
	}
	resultDigest := result.ResultDigestSHA256
	o2ReceiptDigest := o2Receipt.ReceiptDigestSHA256
	receipt.ResultDigestSHA256 = &resultDigest
	receipt.O2ReceiptDigestSHA256 = &o2ReceiptDigest

	if result.TerminalOutcome != providerport.OutcomeCompleted {
		cleanups = append([]func() error{workspace.Close}, cleanups...) // Close must run BEFORE directory removal, not after.
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionO2NonCompleted, "o2 non-completed: "+string(result.TerminalOutcome)+": "+result.Detail, &succeeded, now, detail)
	}
	if result.GenerationPayload == nil {
		cleanups = append([]func() error{workspace.Close}, cleanups...) // Close must run BEFORE directory removal, not after.
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionO2NonCompleted, "o2 reported completed with a nil generation payload", &succeeded, now, detail)
	}
	attempt := *result.GenerationPayload

	// Step 6: O3 treats Result/O2 Receipt as immutable -- never rewritten,
	// whatever they say. Nothing below this point ever mutates result,
	// o2Receipt, attempt, or their referenced digests.

	// Step 7: freeze the workspace before verification -- its own
	// dedicated disposition now that O2 reported completed. Any handle a
	// provider retained past this point fails closed.
	if closeErr := workspace.Close(); closeErr != nil {
		succeeded, detail := runCleanupsTracked(cleanups)
		return finalize(receipt, DispositionWorkspaceFreezeFailure, "workspace close: "+closeErr.Error(), &succeeded, now, detail)
	}

	// Step 8: independently compute repository evidence -- never trusted
	// from the provider's own declared values (hard law 11).
	finalManifest, err := buildFinalManifestFn(bufferDir)
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
