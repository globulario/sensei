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
	// directory (no active pointer / unreadable repository path).
	UnavailableTaskDirectoryUnresolved CompletionUnavailableClass = "task_directory_unresolved"
	// UnavailableProjectionOwnerError: the projection owner returned an error.
	UnavailableProjectionOwnerError CompletionUnavailableClass = "projection_owner_error"
)

func validUnavailableClass(c CompletionUnavailableClass) bool {
	return c == UnavailableTaskDirectoryUnresolved || c == UnavailableProjectionOwnerError
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

// UnavailableProjectionOwnerEnvelope is the ONLY constructor for a
// projection_owner_error envelope.
func UnavailableProjectionOwnerEnvelope(detail string) CompletionProjectionEnvelope {
	return stampEnvelope(CompletionProjectionEnvelope{Availability: CompletionUnavailable, UnavailableClass: UnavailableProjectionOwnerError, UnavailableDetail: detail})
}

// BuildCompletionProjectionEnvelope builds the canonical projection and wraps it in a
// typed availability envelope. It never errors: a projection-owner error becomes an
// explicit `unavailable` envelope rather than silence. Read-only, mutates nothing.
func BuildCompletionProjectionEnvelope(ctx context.Context, req Request) CompletionProjectionEnvelope {
	p, err := BuildCompletionProjection(ctx, req)
	if err != nil {
		return UnavailableProjectionOwnerEnvelope(err.Error())
	}
	return stampEnvelope(CompletionProjectionEnvelope{Availability: CompletionAvailable, Projection: &p})
}

// Summary renders the envelope as one deterministic line. It VALIDATES first and
// never reinterprets a malformed envelope: an invalid envelope renders as an explicit
// invalid line rather than being silently mapped into the unavailable path.
func (e CompletionProjectionEnvelope) Summary() string {
	if err := ValidateCompletionEnvelope(e); err != nil {
		return fmt.Sprintf("invalid completion projection envelope (%v) [non-authoritative projection]", err)
	}
	if e.Availability == CompletionAvailable {
		return e.Projection.Summary()
	}
	return fmt.Sprintf("unavailable (%s: %s) [non-authoritative projection]", e.UnavailableClass, e.UnavailableDetail)
}
