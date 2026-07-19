// SPDX-License-Identifier: Apache-2.0

package changebindingconsumer

import "github.com/globulario/sensei/golang/architecture/changebinding"

// Consume is the pure binding gate. Given whether binding is required, the parsed
// publication set, the CURRENT trusted subject, and a provenance verifier bound to the
// current execution boundary, it returns the typed binding-gate result. It reads nothing
// ambient. A binding is authoritative ONLY on exact correspondence to the current subject
// plus positive provenance — never because of its own self-described provider/issuer/tool.
func Consume(required bool, bindings []changebinding.ChangeTaskBinding, cur CurrentSubject, verifier changebinding.ProvenanceVerifier) BindingGate {
	if !required {
		return gate(BindingNotRequired, "completion binding is not required for this domain/execution")
	}

	// The current context must itself be self-consistent BEFORE trusting any binding: the
	// checked-out repository and head must equal the authoritative event, and the event must
	// be fully-formed. An inconsistent or unsupported current context fails closed and the
	// binding is never trusted against a broken subject.
	if anyEmpty(cur.RepositoryIdentity, cur.ChangeProvider, cur.ChangeID, cur.BaseSHA, cur.HeadSHA, cur.TaskDirectory, cur.TaskID, cur.TaskSessionID, cur.CompletionResultDigestSHA256) {
		return gate(BindingGateUnsupportedExecution, "current execution subject is incomplete")
	}
	if cur.CheckoutRepositoryIdentity != cur.RepositoryIdentity {
		return gate(BindingGateCheckoutMismatch, "checked-out repository does not equal the event repository")
	}
	if cur.CheckoutHeadSHA != cur.HeadSHA {
		return gate(BindingGateCheckoutMismatch, "checked-out head does not equal the event head")
	}

	// Exactly one publication is required; a set never resolves by selection.
	switch len(bindings) {
	case 0:
		return gate(BindingGateAbsent, "no change-to-task binding published where one is required")
	case 1:
		// validated below
	default:
		return gate(BindingGateContradictory, "more than one binding publication; exactly one authoritative binding is required")
	}
	b := bindings[0]

	// Expected subject reconstructed from the CURRENT context — never from the publication.
	expected := changebinding.ExpectedSubject{
		Repository: changebinding.RepositoryIdentity{Provider: cur.RepositoryProvider, Identity: cur.RepositoryIdentity},
		Change:     changebinding.ChangeIdentity{Provider: cur.ChangeProvider, ID: cur.ChangeID, HeadSHA: cur.HeadSHA, BaseSHA: cur.BaseSHA},
		Task:       &changebinding.TaskIdentity{Directory: cur.TaskDirectory, ID: cur.TaskID, SessionID: cur.TaskSessionID},
	}
	vr := changebinding.ValidateBinding(b, expected, verifier.VerifyProvenance(b))
	if g := mapValidity(vr.Validity); g != BindingAccepted {
		return gate(g, vr.Detail)
	}

	// The ckpt1 validator confirmed repo/head/base/task + digest + provenance. The consumer
	// ADDS current-context bindings the schema alone cannot: the completion-result digest
	// actually passed to enforcement, the task session, and the expected producer identity.
	if b.CompletionResultDigestSHA256 != cur.CompletionResultDigestSHA256 {
		return gate(BindingGateCompletionResultMismatch, "binding completion-result digest is not the result under evaluation")
	}
	if b.Task.SessionID != cur.TaskSessionID {
		return gate(BindingGateTaskSessionMismatch, "binding task session does not match the current session")
	}
	if (cur.ExpectedIssuer != "" && b.Issuer != cur.ExpectedIssuer) || (cur.ExpectedTool != "" && b.Provenance.Tool != cur.ExpectedTool) {
		return gate(BindingGateProducerMismatch, "binding producer identity is not the expected producer for this execution")
	}
	return gate(BindingAccepted, "")
}

// mapValidity projects the Checkpoint-1 binding validity onto the binding-gate vocabulary.
// An unknown/zero validity fails closed (publication invalid), never accepted.
func mapValidity(v changebinding.BindingValidity) BindingGateValidity {
	switch v {
	case changebinding.BindingAuthoritative:
		return BindingAccepted
	case changebinding.BindingAbsent:
		return BindingGateAbsent
	case changebinding.BindingMalformed:
		return BindingGateMalformed
	case changebinding.BindingStaleHead:
		return BindingGateStaleHead
	case changebinding.BindingRepositoryMismatch:
		return BindingGateRepositoryMismatch
	case changebinding.BindingTaskMismatch:
		return BindingGateTaskMismatch
	case changebinding.BindingChangeRangeMismatch:
		return BindingGateChangeRangeMismatch
	case changebinding.BindingContradictory:
		return BindingGateContradictory
	case changebinding.BindingUnsupportedVersion:
		return BindingGateUnsupportedVersion
	case changebinding.BindingUnverifiableProvenance:
		return BindingGateUnverifiableProvenance
	case changebinding.BindingPublicationInvalid:
		return BindingGatePublicationInvalid
	default:
		return BindingGatePublicationInvalid // fail closed on any unknown/zero validity
	}
}

// Compose joins the binding gate with the completion decision into the final result. The
// completion evaluation is a THUNK invoked ONLY when the binding is accepted or not
// required — so the 9.4b completion evaluation (owner invocation, runtime classification)
// is structurally UNREACHABLE for any binding failure, which always blocks at the binding
// stage before completion is interpreted.
func Compose(bg BindingGate, completion func() CompletionOutcome) FinalResult {
	if !bg.Validity.accepted() {
		return FinalResult{Result: "block", Reason: string(bg.Validity), Stage: "binding", Binding: bg}
	}
	co := completion()
	return FinalResult{Result: co.Result, Reason: co.Reason, Stage: "completion", Binding: bg, Completion: &co}
}

func anyEmpty(vals ...string) bool {
	for _, v := range vals {
		if v == "" {
			return true
		}
	}
	return false
}
