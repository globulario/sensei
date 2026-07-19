// SPDX-License-Identifier: Apache-2.0

package completion

import (
	"context"
	"fmt"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

const completionProjectionSchemaVersion = "completion.projection/v1"

// CompletionProjection is the ONE canonical, deterministic, read-only operational
// view of a task's completion status. Server, task-status, and task-briefing consume
// this single projection rather than each inventing its own mapping of the Phase-8
// owners. It is a presentation of the owner's reconstruction and end-to-end
// verification — it is NOT an owner, holds no mutation authority, and is never
// terminal truth. It preserves the owners' closed state/verdict vocabularies without
// collapsing them.
type CompletionProjection struct {
	SchemaVersion string                        `json:"schema_version" yaml:"schema_version"`
	Task          closureprotocol.TaskBinding   `json:"task" yaml:"task"`
	ResultBinding closureprotocol.ResultBinding `json:"result_binding,omitempty" yaml:"result_binding,omitempty"`
	// TerminalState is exactly what InspectTerminalState reconstructed.
	TerminalState TerminalState `json:"terminal_state" yaml:"terminal_state"`
	// ClosureVerdict is exactly what VerifyCompletionClosure produced.
	ClosureVerdict ClosureVerdict `json:"closure_verdict" yaml:"closure_verdict"`
	// AuthoritativeCompletion is derived ONLY from ClosureAuthoritativeCompletion.
	AuthoritativeCompletion bool `json:"authoritative_completion" yaml:"authoritative_completion"`
	// GovernedDriftAfterCompletion preserves the distinction between historical
	// authoritative completion and current governed drift.
	GovernedDriftAfterCompletion bool                    `json:"governed_drift_after_completion" yaml:"governed_drift_after_completion"`
	Components                   []ComponentVerification `json:"components,omitempty" yaml:"components,omitempty"`
	Detail                       string                  `json:"detail,omitempty" yaml:"detail,omitempty"`
	// Distinctions state which of the three claims this view shows and disclaims the
	// other two.
	Distinctions []string `json:"distinctions" yaml:"distinctions"`
	// NonAuthoritativeProjection is always true — explicit read-only semantics.
	NonAuthoritativeProjection bool     `json:"non_authoritative_projection" yaml:"non_authoritative_projection"`
	Bound                      []string `json:"bound" yaml:"bound"`
	DigestSHA256               string   `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
}

func projectionDistinctions() []string {
	return []string{
		"this projection shows the completion status of exactly ONE task's current result; it is not Phase-8 implementation closure and not repository-wide perfection",
		"authoritative_completion here means only that the durable event/receipt conjunction re-verified for THIS task; it asserts nothing about any other task or the repository",
		"the Phase-8 closure report is implementation evidence, never a task-completion fact, and is never used here as terminal authority",
	}
}

func projectionBound() []string {
	return []string{
		"a read-only, non-authoritative operational projection composed from the completion owner's read surfaces (InspectTerminalState, VerifyCompletionClosure)",
		"the durable completed event plus its matching receipt, reconstructed by the owner, is the sole terminal truth; building or rendering this projection mutates nothing",
	}
}

// BuildCompletionProjection composes the canonical read-only completion projection by
// calling the exported Phase-8 read owners. It re-derives no terminal truth, reads no
// raw ledger/receipt files, and never calls CompleteTask or RecoverProjections. It is
// deterministic: identical durable evidence yields a byte-identical projection.
func BuildCompletionProjection(ctx context.Context, req Request) (CompletionProjection, error) {
	closure, err := VerifyCompletionClosure(ctx, req)
	if err != nil {
		return CompletionProjection{}, err
	}
	p := CompletionProjection{
		SchemaVersion:                completionProjectionSchemaVersion,
		Task:                         closure.Terminal.Task,
		ResultBinding:                closure.Terminal.CurrentResultBinding,
		TerminalState:                closure.Terminal.State,
		ClosureVerdict:               closure.Verdict,
		AuthoritativeCompletion:      closure.Verdict == ClosureAuthoritativeCompletion,
		GovernedDriftAfterCompletion: closure.GovernedDriftAfterCompletion,
		Components:                   closure.Components,
		Detail:                       closure.Terminal.Detail,
		Distinctions:                 projectionDistinctions(),
		NonAuthoritativeProjection:   true,
		Bound:                        projectionBound(),
	}
	p.DigestSHA256 = ""
	if d, derr := closureprotocol.SemanticDigest(p); derr == nil {
		p.DigestSHA256 = d
	}
	return p, nil
}

// Summary is a single deterministic line for compact operational display. It is the
// single canonical text mapping — surfaces render this rather than re-deriving one.
func (p CompletionProjection) Summary() string {
	drift := ""
	if p.GovernedDriftAfterCompletion {
		drift = " (governed drift after completion)"
	}
	return fmt.Sprintf("completion: state=%s verdict=%s authoritative=%v%s [non-authoritative projection]",
		p.TerminalState, p.ClosureVerdict, p.AuthoritativeCompletion, drift)
}

// recomputeProjectionDigest reproduces the projection's self-excluding digest: the
// DigestSHA256 field is cleared before hashing, exactly as BuildCompletionProjection
// stamped it, so any post-stamp change to any other field alters the result.
func recomputeProjectionDigest(p CompletionProjection) (string, error) {
	p.DigestSHA256 = ""
	return closureprotocol.SemanticDigest(p)
}

func validCompletionTerminalState(s TerminalState) bool {
	for _, v := range AssessmentBoundStates() {
		if v == s {
			return true
		}
	}
	return false
}

func validClosureVerdict(v ClosureVerdict) bool {
	switch v {
	case ClosureAuthoritativeCompletion, ClosureNotCompleted, ClosureBroken, ClosureContradictory, ClosureUnsupported:
		return true
	}
	return false
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ValidateCanonicalCompletionProjection enforces the projection's OWN canonical contract,
// one layer below the envelope. A verified envelope digest proves the projection bytes
// are intact, not that the projection is meaningful: a caller could stamp an internally
// consistent but impossible projection — e.g. verdict=not_completed with
// authoritative_completion=true — and re-digest it. This validator requires the exact
// projection schema, the closed terminal-state and verdict vocabularies, the
// authoritative boolean derived ONLY from the authoritative verdict, the
// non-authoritative marker, the canonical distinctions and bound statement, and a
// verified self-excluding digest.
func ValidateCanonicalCompletionProjection(p CompletionProjection) error {
	if p.SchemaVersion != completionProjectionSchemaVersion {
		return fmt.Errorf("projection schema version %q is not the canonical %q", p.SchemaVersion, completionProjectionSchemaVersion)
	}
	if !validCompletionTerminalState(p.TerminalState) {
		return fmt.Errorf("projection terminal state %q is off-vocabulary", p.TerminalState)
	}
	if !validClosureVerdict(p.ClosureVerdict) {
		return fmt.Errorf("projection closure verdict %q is off-vocabulary", p.ClosureVerdict)
	}
	if p.AuthoritativeCompletion != (p.ClosureVerdict == ClosureAuthoritativeCompletion) {
		return fmt.Errorf("authoritative_completion must be derived only from the authoritative verdict")
	}
	if !p.NonAuthoritativeProjection {
		return fmt.Errorf("completion projection must be non-authoritative")
	}
	if !stringSlicesEqual(p.Distinctions, projectionDistinctions()) {
		return fmt.Errorf("projection distinctions are not the canonical set")
	}
	if !stringSlicesEqual(p.Bound, projectionBound()) {
		return fmt.Errorf("projection bound is not the canonical statement")
	}
	if !isHex64(p.DigestSHA256) {
		return fmt.Errorf("projection carries no well-formed self-excluding digest")
	}
	recomputed, err := recomputeProjectionDigest(p)
	if err != nil {
		return fmt.Errorf("recompute projection digest: %w", err)
	}
	if recomputed != p.DigestSHA256 {
		return fmt.Errorf("projection digest %s does not match content; it was altered after stamping", p.DigestSHA256)
	}
	return nil
}

const completionEnvelopeSchemaVersion = "completion.projection_envelope/v1"

// CompletionAvailability is the typed availability of the completion projection at a
// surface boundary. It distinguishes a real projection (available — which itself
// carries not_completed/unsupported/integrity_failure states) from the projection
// being unestablishable (unavailable), so a surface never collapses those realities
// into silence. Omission is a remapping; this envelope forbids it.
type CompletionAvailability string

const (
	CompletionAvailable   CompletionAvailability = "available"
	CompletionUnavailable CompletionAvailability = "unavailable"
)

// CompletionUnavailableClass is the CLOSED vocabulary of reasons a completion
// projection could not be established at a surface boundary. Only these values are
// canonical; an arbitrary synonym cannot become a valid envelope.
type CompletionUnavailableClass string

const (
	// UnavailableTaskDirectoryUnresolved: the surface could not resolve a task
	// directory (no active pointer / unreadable repository path). This is an IDENTITY
	// cause — an absent task identity, not an outage.
	UnavailableTaskDirectoryUnresolved CompletionUnavailableClass = "task_directory_unresolved"
	// UnavailableProjectionOwnerError: the projection owner returned an error, cause
	// unspecified. Retained for internal callers that do not classify; the projection
	// ENVELOPE builder never emits this — it splits the cause below.
	UnavailableProjectionOwnerError CompletionUnavailableClass = "projection_owner_error"
	// UnavailableProjectionOwnerIdentityError: the owner could not be selected or
	// trusted because the task identity was absent/malformed/noncanonical/out-of-scope/
	// contradictory BEFORE a legitimate owner invocation began. An IDENTITY cause.
	UnavailableProjectionOwnerIdentityError CompletionUnavailableClass = "projection_owner_identity_error"
	// UnavailableProjectionOwnerRuntimeError: task identity was valid and the canonical
	// owner was resolved and invoked, but the owner failed at runtime (execution,
	// transport, I/O, timeout, decoding). A RUNTIME cause.
	UnavailableProjectionOwnerRuntimeError CompletionUnavailableClass = "projection_owner_runtime_error"
)

func validUnavailableClass(c CompletionUnavailableClass) bool {
	switch c {
	case UnavailableTaskDirectoryUnresolved, UnavailableProjectionOwnerError,
		UnavailableProjectionOwnerIdentityError, UnavailableProjectionOwnerRuntimeError:
		return true
	}
	return false
}

// CompletionProjectionEnvelope makes projection availability explicit and typed. When
// available it carries the deterministic projection; when unavailable it carries a
// class from the closed CompletionUnavailableClass vocabulary and a detail. It is
// always non-authoritative and never fabricates a terminal state for a construction
// error.
type CompletionProjectionEnvelope struct {
	SchemaVersion              string                     `json:"schema_version" yaml:"schema_version"`
	Availability               CompletionAvailability     `json:"availability" yaml:"availability"`
	Projection                 *CompletionProjection      `json:"projection,omitempty" yaml:"projection,omitempty"`
	UnavailableClass           CompletionUnavailableClass `json:"unavailable_class,omitempty" yaml:"unavailable_class,omitempty"`
	UnavailableDetail          string                     `json:"unavailable_detail,omitempty" yaml:"unavailable_detail,omitempty"`
	NonAuthoritativeProjection bool                       `json:"non_authoritative_projection" yaml:"non_authoritative_projection"`
	DigestSHA256               string                     `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
}

// ValidateCompletionEnvelope enforces the availability/field conjunction and the
// closed unavailable vocabulary, so an arbitrary class or a malformed available/
// unavailable combination cannot pass as canonical.
func ValidateCompletionEnvelope(e CompletionProjectionEnvelope) error {
	switch e.Availability {
	case CompletionAvailable:
		if e.Projection == nil {
			return fmt.Errorf("available envelope must carry a projection")
		}
		if e.UnavailableClass != "" || e.UnavailableDetail != "" {
			return fmt.Errorf("available envelope must carry no unavailable class/detail")
		}
	case CompletionUnavailable:
		if e.Projection != nil {
			return fmt.Errorf("unavailable envelope must carry no projection")
		}
		if !validUnavailableClass(e.UnavailableClass) {
			return fmt.Errorf("unavailable envelope class %q is not a recognized CompletionUnavailableClass", e.UnavailableClass)
		}
	default:
		return fmt.Errorf("availability %q is off-vocabulary", e.Availability)
	}
	if !e.NonAuthoritativeProjection {
		return fmt.Errorf("completion envelope must be non-authoritative")
	}
	return nil
}

// recomputeEnvelopeDigest reproduces the exact self-excluding digest stampEnvelope
// wrote: the DigestSHA256 field is cleared before hashing so the digest never covers
// itself. Every other field — including a post-stamp mutation to schema, class, detail,
// or the nested projection — participates, so any alteration changes the result.
func recomputeEnvelopeDigest(e CompletionProjectionEnvelope) (string, error) {
	e.DigestSHA256 = ""
	return closureprotocol.SemanticDigest(e)
}

// CompletionPublicationInvalidClass is the CLOSED vocabulary of reasons a stamped
// envelope is not canonically publishable. It is produced only by the classifier below —
// never caller-supplied — so a publication surface reports a recognized category rather
// than an ad-hoc string.
type CompletionPublicationInvalidClass string

const (
	// PublicationInvalidStructure: the envelope fails the pre-stamp structural conjunction.
	PublicationInvalidStructure CompletionPublicationInvalidClass = "structure"
	// PublicationInvalidSchema: the envelope schema version is not the canonical one.
	PublicationInvalidSchema CompletionPublicationInvalidClass = "schema"
	// PublicationInvalidDigestMalformed: the envelope carries no well-formed / recomputable digest.
	PublicationInvalidDigestMalformed CompletionPublicationInvalidClass = "digest_malformed"
	// PublicationInvalidDigestMismatch: the stored digest no longer matches the content (altered after stamping).
	PublicationInvalidDigestMismatch CompletionPublicationInvalidClass = "digest_mismatch"
	// PublicationInvalidProjection: the nested projection does not obey its own canonical contract.
	PublicationInvalidProjection CompletionPublicationInvalidClass = "projection"
)

func validPublicationInvalidClass(c CompletionPublicationInvalidClass) bool {
	switch c {
	case PublicationInvalidStructure, PublicationInvalidSchema, PublicationInvalidDigestMalformed, PublicationInvalidDigestMismatch, PublicationInvalidProjection:
		return true
	}
	return false
}

// classifyCanonicalCompletionEnvelope is the PUBLICATION check, distinct from the
// pre-stamp structural ValidateCompletionEnvelope. Closing the constructor door stops
// invalid construction, but a stamped envelope can still be mutated or forged between
// the workshop and the display case: its fields keep satisfying the structural
// conjunction while its digest no longer represents its content. Publication therefore
// re-verifies canonical identity — structural validity PLUS the exact envelope schema
// version, a well-formed digest, and equality between the stored digest and a freshly
// recomputed self-excluding digest. It returns ("", nil) when canonical, else the typed
// class and the detail. Detectable invalidity is not enforced validity; identity not
// re-verified at publication is still advisory.
func classifyCanonicalCompletionEnvelope(e CompletionProjectionEnvelope) (CompletionPublicationInvalidClass, error) {
	if err := ValidateCompletionEnvelope(e); err != nil {
		return PublicationInvalidStructure, err
	}
	if e.SchemaVersion != completionEnvelopeSchemaVersion {
		return PublicationInvalidSchema, fmt.Errorf("envelope schema version %q is not the canonical %q", e.SchemaVersion, completionEnvelopeSchemaVersion)
	}
	if !isHex64(e.DigestSHA256) {
		return PublicationInvalidDigestMalformed, fmt.Errorf("envelope carries no well-formed self-excluding digest")
	}
	recomputed, err := recomputeEnvelopeDigest(e)
	if err != nil {
		return PublicationInvalidDigestMalformed, fmt.Errorf("recompute envelope digest: %w", err)
	}
	if recomputed != e.DigestSHA256 {
		return PublicationInvalidDigestMismatch, fmt.Errorf("envelope digest %s does not match content; it was altered after stamping", e.DigestSHA256)
	}
	// One layer below the envelope: an available envelope must carry a projection that
	// obeys its OWN canonical contract. A verified envelope digest proves the bytes are
	// intact, not that the nested projection is meaningful, so an impossible projection
	// (e.g. verdict=not_completed with authoritative_completion=true) is rejected here.
	if e.Availability == CompletionAvailable {
		if perr := ValidateCanonicalCompletionProjection(*e.Projection); perr != nil {
			return PublicationInvalidProjection, fmt.Errorf("nested projection is not canonical: %w", perr)
		}
	}
	return "", nil
}

// ValidateCanonicalCompletionEnvelope is the boolean-style publication gate used by
// surfaces that publish completion truth (Summary, structured task-status). It reports
// the classifier's error and hides the class from callers that only need validity.
func ValidateCanonicalCompletionEnvelope(e CompletionProjectionEnvelope) error {
	_, err := classifyCanonicalCompletionEnvelope(e)
	return err
}

// stampEnvelope ENFORCES the availability/field conjunction before it computes a
// digest. An invalid envelope is never stamped: it receives no canonical digest, so
// validation governs construction rather than being an optional review step. Only the
// dedicated constructors reach this path, so a stamped envelope is always valid.
func stampEnvelope(e CompletionProjectionEnvelope) CompletionProjectionEnvelope {
	e.SchemaVersion = completionEnvelopeSchemaVersion
	e.NonAuthoritativeProjection = true
	e.DigestSHA256 = ""
	if ValidateCompletionEnvelope(e) != nil {
		return e // unstamped, digest-less, detectably non-canonical
	}
	if d, err := closureprotocol.SemanticDigest(e); err == nil {
		e.DigestSHA256 = d
	}
	return e
}

// UnavailableTaskDirectoryEnvelope is the ONLY constructor for a
// task_directory_unresolved envelope. The class cannot be caller-supplied, so no
// off-vocabulary reason can be minted.
func UnavailableTaskDirectoryEnvelope(detail string) CompletionProjectionEnvelope {
	return stampEnvelope(CompletionProjectionEnvelope{Availability: CompletionUnavailable, UnavailableClass: UnavailableTaskDirectoryUnresolved, UnavailableDetail: detail})
}

// UnavailableProjectionOwnerEnvelope is the ONLY constructor for a generic (unclassified)
// projection_owner_error envelope. The projection ENVELOPE builder does not use it — it
// classifies the cause via UnavailableProjectionOwnerCauseEnvelope. It remains for
// internal callers that surface a non-canonical envelope without classifying a cause.
func UnavailableProjectionOwnerEnvelope(detail string) CompletionProjectionEnvelope {
	return stampEnvelope(CompletionProjectionEnvelope{Availability: CompletionUnavailable, UnavailableClass: UnavailableProjectionOwnerError, UnavailableDetail: detail})
}

// UnavailableProjectionOwnerCauseEnvelope constructs an unavailable envelope whose class
// is the TYPED cause of err — identity vs runtime — classified at this boundary from the
// error's type (see ProjectionOwnerErrorClass), never from its text.
func UnavailableProjectionOwnerCauseEnvelope(err error) CompletionProjectionEnvelope {
	return stampEnvelope(CompletionProjectionEnvelope{
		Availability:      CompletionUnavailable,
		UnavailableClass:  ProjectionOwnerErrorClass(err),
		UnavailableDetail: err.Error(),
	})
}

// BuildCompletionProjectionEnvelope builds the canonical projection and wraps it in a
// typed availability envelope. It never errors: a projection-owner error becomes an
// explicit `unavailable` envelope rather than silence. Read-only, mutates nothing.
func BuildCompletionProjectionEnvelope(ctx context.Context, req Request) CompletionProjectionEnvelope {
	// Positive identity gate at THIS boundary, before any owner invocation. An absent,
	// unresolvable, out-of-scope, or otherwise invalid task identity fails here as a
	// typed identity error and the owner is never invoked — so the runtime lane below is
	// structurally unreachable for an identity failure.
	if err := validateRepositoryTaskBinding(req.RepositoryRoot, req.TaskDirectory); err != nil {
		return UnavailableProjectionOwnerCauseEnvelope(err)
	}
	// Identity established. Invoke the canonical owner. Any error from the invocation is
	// tagged with POSITIVE runtime evidence (invokeCompletionOwner IS the invocation
	// boundary) — the only way to earn the runtime class.
	p, err := invokeCompletionOwner(ctx, req)
	if err != nil {
		return UnavailableProjectionOwnerCauseEnvelope(err)
	}
	return stampEnvelope(CompletionProjectionEnvelope{Availability: CompletionAvailable, Projection: &p})
}

// invokeCompletionOwner is the owner-invocation boundary: reaching it proves identity
// was validated and the owner is being invoked, so ANY error it returns is positive
// runtime evidence (runtimeError keeps a stray identity error as identity). This
// placement — not the mere absence of an identity error — is what earns the runtime
// class.
func invokeCompletionOwner(ctx context.Context, req Request) (CompletionProjection, error) {
	p, err := buildProjectionForEnvelope(ctx, req)
	return p, runtimeError(err)
}

// buildProjectionForEnvelope is the projection builder invokeCompletionOwner runs. It is
// a package var ONLY so a test can simulate a post-identity runtime failure of the
// resolved owner — which the in-process owner does not otherwise surface — to prove the
// runtime cause is classified distinctly from identity. Production always uses
// BuildCompletionProjection.
var buildProjectionForEnvelope = BuildCompletionProjection

// Summary renders the envelope as one deterministic line. It requires CANONICAL
// publication validity — not merely the structural conjunction — so an envelope altered
// or forged after stamping renders as explicitly invalid rather than presenting its
// tampered content under a stale digest. It never reinterprets a malformed envelope
// into the unavailable path.
func (e CompletionProjectionEnvelope) Summary() string {
	if err := ValidateCanonicalCompletionEnvelope(e); err != nil {
		return fmt.Sprintf("invalid completion projection envelope (%v) [non-authoritative projection]", err)
	}
	if e.Availability == CompletionAvailable {
		return e.Projection.Summary()
	}
	return fmt.Sprintf("unavailable (%s: %s) [non-authoritative projection]", e.UnavailableClass, e.UnavailableDetail)
}

const completionPublicationSchemaVersion = "completion.projection_publication/v1"

// CompletionProjectionPublication is the STABLE typed wire union a surface publishes for
// a completion projection. It has ONE schema identifier and ONE outer shape across both
// outcomes, so a single schema never describes two incompatible shapes: `canonical: true`
// carries the verified envelope; `canonical: false` carries a typed invalid class from
// the closed CompletionPublicationInvalidClass vocabulary plus a human reason. It is
// always non-authoritative.
type CompletionProjectionPublication struct {
	SchemaVersion              string                            `json:"schema_version" yaml:"schema_version"`
	Canonical                  bool                              `json:"canonical" yaml:"canonical"`
	Envelope                   *CompletionProjectionEnvelope     `json:"envelope,omitempty" yaml:"envelope,omitempty"`
	InvalidClass               CompletionPublicationInvalidClass `json:"invalid_class,omitempty" yaml:"invalid_class,omitempty"`
	InvalidReason              string                            `json:"invalid_reason,omitempty" yaml:"invalid_reason,omitempty"`
	NonAuthoritativeProjection bool                              `json:"non_authoritative_projection" yaml:"non_authoritative_projection"`
}

// PublicationView is the representation a surface may PUBLISH in structured output. It
// always returns the same typed union under one schema: a canonical envelope is carried
// verbatim under `canonical: true`; a tampered, unstamped, or wrong-schema envelope
// becomes `canonical: false` with a typed class + reason — never presented as if it were
// the canonical envelope, and never mislabeled with the envelope's own schema.
func (e CompletionProjectionEnvelope) PublicationView() CompletionProjectionPublication {
	pub := CompletionProjectionPublication{
		SchemaVersion:              completionPublicationSchemaVersion,
		NonAuthoritativeProjection: true,
	}
	if class, err := classifyCanonicalCompletionEnvelope(e); err != nil {
		pub.Canonical = false
		pub.InvalidClass = class
		pub.InvalidReason = err.Error()
		return pub
	}
	env := e
	pub.Canonical = true
	pub.Envelope = &env
	return pub
}

// ValidateCompletionPublication enforces the union's own conjunction so a consumer can
// re-verify a published value: the canonical schema, non-authoritative, and exactly one
// coherent path — canonical with a canonically valid envelope and no invalid fields, or
// non-canonical with a recognized class, a reason, and no envelope.
func ValidateCompletionPublication(p CompletionProjectionPublication) error {
	if p.SchemaVersion != completionPublicationSchemaVersion {
		return fmt.Errorf("publication schema version %q is not the canonical %q", p.SchemaVersion, completionPublicationSchemaVersion)
	}
	if !p.NonAuthoritativeProjection {
		return fmt.Errorf("completion publication must be non-authoritative")
	}
	if p.Canonical {
		if p.Envelope == nil {
			return fmt.Errorf("canonical publication must carry an envelope")
		}
		if p.InvalidClass != "" || p.InvalidReason != "" {
			return fmt.Errorf("canonical publication must carry no invalid class/reason")
		}
		return ValidateCanonicalCompletionEnvelope(*p.Envelope)
	}
	if p.Envelope != nil {
		return fmt.Errorf("non-canonical publication must carry no envelope")
	}
	if !validPublicationInvalidClass(p.InvalidClass) {
		return fmt.Errorf("non-canonical publication class %q is not recognized", p.InvalidClass)
	}
	if p.InvalidReason == "" {
		return fmt.Errorf("non-canonical publication must carry a reason")
	}
	return nil
}
