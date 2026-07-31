// SPDX-License-Identifier: AGPL-3.0-only

// Package runnercomposition is the canonical data-model layer for the O3
// governed runner composition contract
// (docs/design/governed-runner-composition-o3.md): the CandidateArtifact and
// RunnerReceipt closed documents O3 introduces, plus their JSON Schema
// validation and semantic digests, and the canonical, collision-safe
// candidate-tree manifest encoding both digests are built on.
//
// This package owns no provider construction, no repository read/write, no
// git invocation, and no O2 Run orchestration -- those are a separate, later
// piece of work (O3 step 2), not part of this package's step 1 scope. It
// composes with existing typed owners rather than inventing parallel ones:
// synthesis.Attempt already reserves InputCandidateDigestSHA256/
// ProposedChangeDigestSHA256 (this package gives those fields real,
// independently-computed content); providerport.Request/Result/Receipt are
// referenced here by digest only, never embedded or rewritten (see hard laws
// 12-13 in the design doc).
package runnercomposition

import "fmt"

// CandidateFileMode is the closed, three-value vocabulary for a candidate
// tree entry's kind, per hard law 9. It intentionally does not carry full
// POSIX permission bits -- only what real git diff semantics distinguish.
type CandidateFileMode string

const (
	ModeRegular    CandidateFileMode = "regular"
	ModeExecutable CandidateFileMode = "executable"
	ModeSymlink    CandidateFileMode = "symlink"
)

// CandidateManifestEntry is one canonical entry in a candidate tree's
// manifest -- either the pinned input snapshot or a sealed final candidate.
// Exactly one of Content or SymlinkTarget is meaningful, selected by Mode:
// Content for ModeRegular/ModeExecutable, SymlinkTarget for ModeSymlink. The
// field that does not apply is always its zero value (an empty, non-nil
// byte slice / an empty string) -- never nil -- so JSON encoding is
// deterministic and never emits a null where a string is expected.
//
// Content and SymlinkTarget preserve the actual candidate bytes: a manifest
// entry is never digest-only, so a CandidateArtifact built from these
// entries can be inspected and evaluated, not just compared by hash.
type CandidateManifestEntry struct {
	// Path is POSIX-relative, "/"-separated, with no "." or ".." segment, no
	// leading "/", and no embedded NUL or newline byte -- see
	// ValidateCandidatePath. A path outside these rules is rejected before
	// it can appear in any manifest, so a symlink target or traversal
	// sequence can never be used to address outside the candidate tree.
	Path string            `json:"path"`
	Mode CandidateFileMode `json:"mode"`

	// Content is the entry's raw file bytes, meaningful only when Mode is
	// ModeRegular or ModeExecutable. Never transcoded, never
	// line-ending-normalized. Empty (never nil) when Mode is ModeSymlink.
	Content []byte `json:"content"`
	// SymlinkTarget is the literal, unresolved target string, meaningful
	// only when Mode is ModeSymlink. O3 never resolves or follows a symlink
	// target -- it is stored and digested as opaque data, so a symlink can
	// never be used to escape the candidate tree. Empty when Mode is
	// ModeRegular or ModeExecutable.
	SymlinkTarget string `json:"symlink_target"`

	// ContentDigestSHA256 is sha256 of Content (ModeRegular/ModeExecutable)
	// or of the raw bytes of SymlinkTarget (ModeSymlink). CanonicalizeManifest
	// recomputes and verifies this from Content/SymlinkTarget; it is never
	// trusted as a bare declared value on its own.
	ContentDigestSHA256 string `json:"content_digest_sha256"`
}

// --- sensei.runnercomposition.candidateartifact.v1 ---

const CandidateArtifactSchemaVersion = "sensei.runnercomposition.candidateartifact.v1"

// CandidateArtifact is the immutable, content-addressed sealing of one
// generation attempt's candidate, per hard laws 10 and 14. It is sealed by a
// CandidateArtifactStore before the ephemeral capture surface that produced
// it is destroyed, and is the only thing O4, O5, retry, and resume address a
// candidate by -- never a temporary directory or any other filesystem
// location.
type CandidateArtifact struct {
	SchemaVersion string `json:"schema_version"`

	// Repository/base/workspace identity -- structural, sourced only from
	// the originating synthesis.Session and the workspacecontract.Identity
	// it references (hard law 3), never independently re-derived.
	RepositoryDomain              string `json:"repository_domain"`
	BaseRevision                  string `json:"base_revision"`
	WorkspaceIdentityDigestSHA256 string `json:"workspace_identity_digest_sha256"`

	// Session/plan/generation/attempt identity this candidate belongs to.
	SessionDigestSHA256 string `json:"session_digest_sha256"`
	PlanDigestSHA256    string `json:"plan_digest_sha256"`
	PlanGeneration      int    `json:"plan_generation"`
	AttemptNumber       int    `json:"attempt_number"`

	// Three distinct digests -- see hard law 10. None substitutes for
	// another. InputCandidateDigestSHA256 and FinalCandidateContentDigestSHA256
	// are both computed by ManifestDigest, over the input snapshot's and the
	// final tree's manifest respectively. ProposedChangeDigestSHA256 is
	// computed independently (the git-diff-shaped convention described in
	// the design doc), over the change between the two, not over either
	// tree alone.
	InputCandidateDigestSHA256        string `json:"input_candidate_digest_sha256"`
	ProposedChangeDigestSHA256        string `json:"proposed_change_digest_sha256"`
	FinalCandidateContentDigestSHA256 string `json:"final_candidate_content_digest_sha256"`

	// Manifest is the final candidate tree's canonical, sorted entry list --
	// see CanonicalizeManifest. FinalCandidateContentDigestSHA256 is
	// ManifestDigest(Manifest).
	Manifest []CandidateManifestEntry `json:"manifest"`

	// CandidateArtifactDigestSHA256 is the self-referential semantic digest
	// of this document with this field zeroed before hashing. A
	// CandidateArtifactStore's Put verifies it before storing; Get
	// reverifies it before returning, so storage corruption cannot silently
	// launder a tampered artifact (hard law 14).
	CandidateArtifactDigestSHA256 string `json:"candidate_artifact_digest_sha256"`
}

// --- sensei.runnercomposition.runnerreceipt.v1 ---

const RunnerReceiptSchemaVersion = "sensei.runnercomposition.runnerreceipt.v1"

// Disposition is the closed, ten-value vocabulary stating exactly how far
// one generation attempt's runner sequence (snapshot -> workspace/buffer
// init -> provider construction -> O2 Run -> workspace freezing -> evidence
// computation -> verification -> sealing) progressed before it stopped, per
// hard laws 5, 5a, 6a, 8a, and 13. It is not a correctness, admission, or
// completion verdict -- only Disposition == DispositionVerified means the
// accompanying Result may reach MapToCommand. See the "Runner disposition
// and evidence-presence matrix" table in
// docs/design/governed-runner-composition-o3.md for the authoritative,
// field-by-field presence rule each value implies -- RunnerReceiptFieldPresence
// below is that table encoded as data.
type Disposition string

const (
	// DispositionSnapshotFailure: the bounded, read-only repository
	// snapshot could not be built. O2's Run was never invoked.
	DispositionSnapshotFailure Disposition = "snapshot-failure"
	// DispositionWorkspaceInitFailure: the snapshot succeeded, but
	// initializing the ephemeral buffer or constructing CandidateWorkspace
	// over it failed. O2's Run was never invoked. Whether a partial
	// ephemeral surface exists at this point is not generally knowable, so
	// this is the one disposition CleanupRequirementFor does not pin to a
	// fixed presence (see CleanupAmbiguous).
	DispositionWorkspaceInitFailure Disposition = "workspace-init-failure"
	// DispositionProviderConstructionFailure: the snapshot and
	// workspace/buffer init succeeded, but
	// GenerationProviderFactory.NewProvider failed. O2's Run was never
	// invoked.
	DispositionProviderConstructionFailure Disposition = "provider-construction-failure"
	// DispositionO2RunError: the snapshot, workspace init, and provider
	// construction succeeded, but O2's Run itself returned a hard Go error
	// -- no valid Result/Receipt was constructed at all (O2's own reserved
	// meaning for that error).
	DispositionO2RunError Disposition = "o2-run-error"
	// DispositionO2NonCompleted: O2's Run produced a valid Result/Receipt,
	// but TerminalOutcome was unavailable, timed-out, cancelled,
	// invalid-output, or unsupported-capability -- a legitimate, evidenced
	// non-completion, not an error.
	DispositionO2NonCompleted Disposition = "o2-non-completed"
	// DispositionWorkspaceFreezeFailure: O2's Run completed, but
	// CandidateWorkspace.Close itself failed, so the final buffer content
	// cannot be trusted as stable. Evidence computation does not proceed.
	DispositionWorkspaceFreezeFailure Disposition = "workspace-freeze-failure"
	// DispositionEvidenceComputationFailure: O2's Run completed and the
	// workspace froze cleanly, but computing ProposedChangeDigestSHA256/
	// FinalCandidateContentDigestSHA256 itself failed -- not a mismatch,
	// a failure to produce a value at all. Distinct from both
	// DispositionWorkspaceFreezeFailure (stops before computation is
	// attempted) and DispositionDigestMismatch (computation succeeds, just
	// doesn't match).
	DispositionEvidenceComputationFailure Disposition = "evidence-computation-failure"
	// DispositionDigestMismatch: O2's Run completed with
	// TerminalOutcome: completed, the workspace froze cleanly, evidence
	// computation succeeded, but O3's independently computed evidence did
	// NOT match the Result's declared values. The candidate is still
	// sealed, as evidence of what the mismatch actually was.
	DispositionDigestMismatch Disposition = "digest-mismatch"
	// DispositionSealFailure: every stage through verification succeeded
	// and matched, but CandidateArtifactStore.Put failed.
	DispositionSealFailure Disposition = "seal-failure"
	// DispositionVerified: every stage succeeded and O3's evidence matched.
	DispositionVerified Disposition = "verified"
)

// AllDispositions lists every closed Disposition value, in the order they
// appear in the design doc's disposition matrix.
func AllDispositions() []Disposition {
	return []Disposition{
		DispositionSnapshotFailure,
		DispositionWorkspaceInitFailure,
		DispositionProviderConstructionFailure,
		DispositionO2RunError,
		DispositionO2NonCompleted,
		DispositionWorkspaceFreezeFailure,
		DispositionEvidenceComputationFailure,
		DispositionDigestMismatch,
		DispositionSealFailure,
		DispositionVerified,
	}
}

// RunnerReceiptFieldPresence states which of RunnerReceipt's nullable digest
// fields must be non-nil for a given Disposition -- the disposition matrix
// from the design doc, encoded as data so ValidateRunnerReceipt and its
// tests share one source of truth instead of duplicating the table.
type RunnerReceiptFieldPresence struct {
	Result                bool
	O2Receipt             bool
	InputCandidate        bool
	ProposedChange        bool
	FinalCandidateContent bool
	CandidateArtifact     bool
}

// FieldPresenceFor returns the required non-nil/nil shape for d's digest
// fields other than RequestDigestSHA256 (always present). Returns an error
// if d is not one of the ten closed Disposition values.
func FieldPresenceFor(d Disposition) (RunnerReceiptFieldPresence, error) {
	switch d {
	case DispositionSnapshotFailure:
		return RunnerReceiptFieldPresence{}, nil
	case DispositionWorkspaceInitFailure, DispositionProviderConstructionFailure, DispositionO2RunError:
		return RunnerReceiptFieldPresence{InputCandidate: true}, nil
	case DispositionO2NonCompleted, DispositionWorkspaceFreezeFailure, DispositionEvidenceComputationFailure:
		return RunnerReceiptFieldPresence{Result: true, O2Receipt: true, InputCandidate: true}, nil
	case DispositionSealFailure:
		return RunnerReceiptFieldPresence{Result: true, O2Receipt: true, InputCandidate: true, ProposedChange: true, FinalCandidateContent: true}, nil
	case DispositionDigestMismatch, DispositionVerified:
		return RunnerReceiptFieldPresence{Result: true, O2Receipt: true, InputCandidate: true, ProposedChange: true, FinalCandidateContent: true, CandidateArtifact: true}, nil
	default:
		return RunnerReceiptFieldPresence{}, fmt.Errorf("runnercomposition: %q is not one of the ten closed Disposition values", d)
	}
}

// CleanupRequirement states whether RunnerReceipt.CleanupSucceeded must be
// nil, must be a non-nil boolean, or may legitimately be either, for a given
// Disposition. Unlike the six digest fields (RunnerReceiptFieldPresence),
// this is NOT a strict function of Disposition alone for
// DispositionWorkspaceInitFailure -- see CleanupAmbiguous.
type CleanupRequirement int

const (
	// CleanupNotApplicable: CleanupSucceeded must be nil. No ephemeral
	// surface can exist yet.
	CleanupNotApplicable CleanupRequirement = iota
	// CleanupRequired: CleanupSucceeded must be a non-nil boolean. An
	// ephemeral surface definitely exists.
	CleanupRequired
	// CleanupAmbiguous: CleanupSucceeded may legitimately be either nil or
	// a non-nil boolean. Only DispositionWorkspaceInitFailure has this
	// requirement -- whether a partial ephemeral surface was created
	// before that failure is not generally knowable.
	CleanupAmbiguous
)

// CleanupRequirementFor returns d's CleanupRequirement. Returns an error if
// d is not one of the ten closed Disposition values.
func CleanupRequirementFor(d Disposition) (CleanupRequirement, error) {
	switch d {
	case DispositionSnapshotFailure:
		return CleanupNotApplicable, nil
	case DispositionWorkspaceInitFailure:
		return CleanupAmbiguous, nil
	case DispositionProviderConstructionFailure, DispositionO2RunError,
		DispositionO2NonCompleted, DispositionWorkspaceFreezeFailure, DispositionEvidenceComputationFailure,
		DispositionDigestMismatch, DispositionSealFailure, DispositionVerified:
		return CleanupRequired, nil
	default:
		return 0, fmt.Errorf("runnercomposition: %q is not one of the ten closed Disposition values", d)
	}
}

// RunnerReceipt is O3's own closed evidence document -- a second, later
// layer of truth than O2's Receipt, never a rewrite of it (hard laws 12-13).
// Every digest field other than RequestDigestSHA256 is nullable; its
// presence for a given Disposition is exactly what FieldPresenceFor(d)
// (equivalently, the design doc's disposition matrix) says -- see
// ValidateRunnerReceipt, which enforces this.
type RunnerReceipt struct {
	SchemaVersion string `json:"schema_version"`
	ReceiptID     string `json:"receipt_id"`

	// RequestDigestSHA256 references the providerport.Request this attempt
	// was constructed for. Always present -- the request exists before the
	// runner sequence begins.
	RequestDigestSHA256 string `json:"request_digest_sha256"`

	// ResultDigestSHA256 / O2ReceiptDigestSHA256 reference O2's own,
	// unaltered Result and Receipt (hard laws 12-13). Named as owning
	// package plus field verbatim, the same convention synthesis.Receipt
	// already uses for its own opaque AdmissionDecisionDigestSHA256 /
	// AdmissionVerificationDigestSHA256 references.
	ResultDigestSHA256    *string `json:"result_digest_sha256"`
	O2ReceiptDigestSHA256 *string `json:"o2_receipt_digest_sha256"`

	InputCandidateDigestSHA256        *string `json:"input_candidate_digest_sha256"`
	ProposedChangeDigestSHA256        *string `json:"proposed_change_digest_sha256"`
	FinalCandidateContentDigestSHA256 *string `json:"final_candidate_content_digest_sha256"`

	// CandidateArtifactDigestSHA256 references the sealed CandidateArtifact
	// -- see hard law 14.
	CandidateArtifactDigestSHA256 *string `json:"candidate_artifact_digest_sha256"`

	Disposition Disposition `json:"disposition"`

	// FailureDetail is a human-readable explanation. Required non-empty
	// exactly when Disposition is not DispositionVerified; empty when
	// verified.
	FailureDetail string `json:"failure_detail"`

	// CleanupSucceeded records whether destroying the ephemeral capture
	// surface afterward succeeded -- ORTHOGONAL to Disposition (hard law
	// 6a note; design doc "Runner disposition and evidence-presence
	// matrix"). nil means "not applicable": true only for
	// DispositionSnapshotFailure, since no ephemeral surface was ever
	// created there. Every other Disposition has a non-nil value here,
	// independent of what Disposition itself is.
	CleanupSucceeded *bool `json:"cleanup_succeeded"`
	// CleanupFailureDetail is required non-empty exactly when
	// CleanupSucceeded is non-nil and false; empty otherwise.
	CleanupFailureDetail string `json:"cleanup_failure_detail"`

	// CompletedAt is an observation timestamp explicitly excluded from
	// RunnerReceiptDigestSHA256 (see RunnerReceiptDigest in digest.go) so
	// receipt identity does not depend on wall-clock time.
	CompletedAt string `json:"completed_at"`

	RunnerReceiptDigestSHA256 string `json:"runner_receipt_digest_sha256"`
}
