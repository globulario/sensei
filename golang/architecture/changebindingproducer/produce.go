// SPDX-License-Identifier: AGPL-3.0-only

package changebindingproducer

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/globulario/sensei/golang/architecture/changebinding"
)

// Produce is the PURE staged producer. Each stage fails to its own typed reason (never a
// 9.4b runtime/degraded reason). Only after every producer-side identity check succeeds
// does it construct the binding, compute the canonical self-excluding digest through the
// Checkpoint-1 package, and self-validate through the SAME pure validator, requiring an
// authoritative result. It reads nothing ambient; the caller supplies verified checkout
// values. It performs no file I/O — publication is a separate step.
func Produce(in ProducerInput, verifier changebinding.ProvenanceVerifier) ProduceResult {
	audit := AuditRecord{
		SchemaVersion:                AuditSchemaVersion,
		RepositoryIdentity:           in.RepositoryIdentity,
		ChangeID:                     in.ChangeID,
		BaseSHA:                      in.BaseSHA,
		HeadSHA:                      in.HeadSHA,
		TaskID:                       in.TaskID,
		TaskSessionID:                in.TaskSessionID,
		CompletionResultDigestSHA256: in.CompletionResultDigestSHA256,
		Issuer:                       in.Issuer,
		Tool:                         in.Tool,
		ToolVersion:                  in.ToolVersion,
	}
	fail := func(f ProducerFailure) ProduceResult {
		audit.Outcome = "failed"
		audit.Failure = f
		audit.Reason = string(f)
		return ProduceResult{Failure: f, Audit: audit}
	}

	// 1. Authoritative event identity.
	if !supportedEvent(in.EventSource) {
		return fail(FailUnsupportedEvent)
	}
	if anyEmpty(in.RepositoryProvider, in.RepositoryIdentity, in.ChangeProvider, in.ChangeID, in.BaseSHA, in.HeadSHA) {
		return fail(FailMissingEventIdentity)
	}
	if !changebinding.IsCanonicalToken(in.RepositoryProvider) || !changebinding.IsCanonicalToken(in.RepositoryIdentity) {
		return fail(FailMalformedRepositoryIdentity)
	}
	if !changebinding.IsCanonicalToken(in.ChangeProvider) || !changebinding.IsCanonicalToken(in.ChangeID) {
		return fail(FailMalformedChangeIdentity)
	}
	if !changebinding.IsFullHex(in.BaseSHA) {
		return fail(FailMalformedBaseSHA)
	}
	if !changebinding.IsFullHex(in.HeadSHA) {
		return fail(FailMalformedHeadSHA)
	}

	// 2. Checkout verification — the checked-out repo/head must equal the authoritative
	// subject exactly. Full commit ids only; shortened/stale/foreign checkouts fail.
	if in.CheckoutRepositoryIdentity != in.RepositoryIdentity {
		return fail(FailCheckoutRepositoryMismatch)
	}
	if !changebinding.IsFullHex(in.CheckoutHeadSHA) {
		return fail(FailCheckoutHeadMismatch) // e.g. a shortened checkout SHA
	}
	if in.CheckoutHeadSHA != in.HeadSHA {
		return fail(FailCheckoutHeadMismatch)
	}
	audit.CheckoutVerified = true

	// 3. Explicit task identity (from the workflow input) — never inferred.
	if anyEmpty(in.TaskDirectory, in.TaskID, in.TaskSessionID) {
		return fail(FailTaskInputAbsent)
	}
	if !changebinding.IsCanonicalToken(in.TaskDirectory) || !changebinding.IsCanonicalToken(in.TaskID) || !changebinding.IsCanonicalToken(in.TaskSessionID) {
		return fail(FailTaskIdentityInvalid)
	}
	audit.TaskIdentityValidated = true

	// 4. Completion-result identity + subject correspondence. The producer binds the
	// exact digest; it never recomputes completion meaning.
	if in.CompletionResultDigestSHA256 == "" {
		return fail(FailCompletionDigestAbsent)
	}
	if !changebinding.IsSHA256Hex(in.CompletionResultDigestSHA256) {
		return fail(FailCompletionDigestMalformed)
	}
	if in.CompletionResultSessionID != in.TaskSessionID {
		return fail(FailTaskSessionMismatch)
	}
	if in.CompletionResultTaskID != in.TaskID {
		return fail(FailCompletionSubjectMismatch)
	}
	audit.CompletionBound = true

	// 5. Construct — only after every identity check passed.
	b := changebinding.ChangeTaskBinding{
		SchemaVersion:                changebinding.SchemaVersion,
		Repository:                   changebinding.RepositoryIdentity{Provider: in.RepositoryProvider, Identity: in.RepositoryIdentity},
		Change:                       changebinding.ChangeIdentity{Provider: in.ChangeProvider, ID: in.ChangeID, HeadSHA: in.HeadSHA, BaseSHA: in.BaseSHA},
		Task:                         changebinding.TaskIdentity{Directory: in.TaskDirectory, ID: in.TaskID, SessionID: in.TaskSessionID},
		CompletionResultDigestSHA256: in.CompletionResultDigestSHA256,
		Issuer:                       in.Issuer,
		Publication:                  changebinding.PublicationIdentity{ID: derivePublicationID(in)},
		Provenance:                   changebinding.Provenance{EventSource: string(in.EventSource), Checkout: in.Checkout, Tool: in.Tool, ToolVersion: in.ToolVersion},
	}
	if !changebinding.IsCanonicalToken(b.Publication.ID) {
		return fail(FailPublicationIdentityInvalid)
	}
	dig, err := changebinding.BindingDigest(b) // the SHARED canonical digest implementation
	if err != nil {
		return fail(FailBindingConstruction)
	}
	b.DigestSHA256 = dig
	audit.BindingConstructed = true
	audit.BindingDigestSHA256 = dig
	audit.PublicationID = b.Publication.ID

	// 6. Self-validate through the SAME pure validator, requiring authoritative.
	prov := verifier.VerifyProvenance(b)
	audit.ProvenanceVerification = provString(prov)
	vr := changebinding.ValidateBinding(b, changebinding.ExpectedSubject{Repository: b.Repository, Change: b.Change, Task: &b.Task}, prov)
	if vr.Validity == changebinding.BindingUnverifiableProvenance {
		return fail(FailProvenanceUnverifiable)
	}
	if !vr.IsAuthoritative() {
		return fail(FailSelfValidation)
	}
	audit.SelfValidated = true

	audit.Outcome = "produced"
	audit.Reason = "authoritative_binding_produced"
	bb := b
	return ProduceResult{Binding: &bb, Failure: FailNone, Audit: audit}
}

// derivePublicationID is a DETERMINISTIC id over the subject — no timestamp, no random.
// Reruns for the exact same subject yield the same id (and thus identical published bytes).
func derivePublicationID(in ProducerInput) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		in.RepositoryIdentity, in.ChangeProvider, in.ChangeID, in.BaseSHA, in.HeadSHA,
		in.TaskID, in.TaskSessionID, in.CompletionResultDigestSHA256,
	}, "\x00")))
	return "pub." + hex.EncodeToString(sum[:])[:32]
}

func anyEmpty(vals ...string) bool {
	for _, v := range vals {
		if v == "" {
			return true
		}
	}
	return false
}

func provString(p changebinding.ProvenanceVerification) string {
	switch p {
	case changebinding.ProvenanceVerified:
		return "verified"
	case changebinding.ProvenanceUnverifiable:
		return "unverifiable"
	default:
		return "invalid"
	}
}
