// SPDX-License-Identifier: AGPL-3.0-only

package questiondisposition

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/identity"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/resultpipeline"
)

// The dedicated governed authority surface for bounded question disposition
// (authored in docs/awareness, Slice 8.1a). Prepare requires the RESOLVED
// operation result for THIS exact operation — not merely that the actor holds
// some grant.
const (
	DomainQuestionDisposition = "authority.sensei_question_disposition"
	GrantQuestionDisposition  = "grant.sensei.question_disposition"
	MechanismPathDisposition  = "mutation_path.question_disposition"
	TargetKindArchitectQ      = "architect_question"
	dispositionOperationID    = "op.dispose.question"
	// dispositionRiskClass is the operation's risk; it must not exceed the
	// grant's maximum_risk_class (architecture_sensitive).
	dispositionRiskClass = "architecture_sensitive"
)

// PrepareRequest carries the operational inputs to a bounded question
// disposition. Every authoritative fact is reconstructed from the verified
// ledger; the caller supplies only the operator's decision and the identity to
// verify it under.
type PrepareRequest struct {
	TaskDirectory  string
	RepositoryRoot string // for authority.LoadPolicyIndex
	IdentityRoot   string // enrolled answering-actor identity store

	QuestionID  string // StableOpenQuestionID of the disposed question
	Disposition Disposition
	Reusability Reusability
	Rationale   string

	// Answer identity + canonical bytes (answered dispositions only).
	AnswerID    string
	AnswerBytes []byte

	// Effective scope this disposition applies to. Must be a subset of both the
	// question linkage and the admitted task scope; broader fails closed.
	EffectiveScopeDomain string
	EffectiveScopeFiles  []string

	EvidenceRefs []string
}

// DispositionCandidate is a fully reconstructed, digest-checked, validated
// receipt ready to record. It is pure: producing it mutates nothing.
type DispositionCandidate struct {
	Receipt                        QuestionDispositionReceipt
	ReceiptBytes                   []byte
	ReceiptMediaType               string
	ReceiptByteDigestSHA256        string
	ExpectedLedgerHeadDigestSHA256 string

	// AnchorEntryDigestSHA256 is the result_transition_recorded entry the
	// disposition binds; its ProducedAt anchors DisposedAt (byte-identical retry).
	AnchorEntryDigestSHA256 string

	// PriorDispositions are existing dispositions for the same question+result,
	// used by RecordDisposition for replay vs contested detection.
	PriorDispositions []QuestionDispositionReceipt
}

// Prepare reconstructs a bounded question disposition from ONE verified ledger
// snapshot: it locates the exact result transition, the architect-questions
// bundle and the disposed question, verifies the answering actor and REQUIRES a
// resolved question-disposition authority result, bounds the effective scope,
// and builds a validated content-addressed receipt. It never writes.
func Prepare(req PrepareRequest) (DispositionCandidate, error) {
	if strings.TrimSpace(req.TaskDirectory) == "" || strings.TrimSpace(req.RepositoryRoot) == "" ||
		strings.TrimSpace(req.IdentityRoot) == "" {
		return DispositionCandidate{}, qdErr(CodeInvalidRequest, "task directory, repository root, and identity root are required")
	}
	if strings.TrimSpace(req.QuestionID) == "" {
		return DispositionCandidate{}, qdErr(CodeInvalidRequest, "question id is required")
	}
	if !dispositions[req.Disposition] {
		return DispositionCandidate{}, qdErr(CodeInvalidRequest, "invalid disposition %q", req.Disposition)
	}

	// One verified snapshot. Everything below reads only from this chain.
	store := newStore(req.TaskDirectory)
	chain, err := store.VerifyChain()
	if err != nil {
		return DispositionCandidate{}, qdErr(CodeChainVerifyFailed, "%v", err)
	}

	anchor, anchorPayload, ok := latestResultTransition(chain)
	if !ok {
		return DispositionCandidate{}, qdErr(CodeNoResultTransition, "no result_transition_recorded event on the ledger")
	}

	// The exact result the disposition binds.
	receipt, err := loadTransitionReceipt(req.TaskDirectory, anchorPayload)
	if err != nil {
		return DispositionCandidate{}, err
	}
	transitionReceiptDigest, err := closureprotocol.ResultTransitionReceiptDigest(receipt)
	if err != nil {
		return DispositionCandidate{}, qdErr(CodeDigestMismatch, "recompute transition receipt digest: %v", err)
	}

	// The exact architect-questions bundle and the disposed question within it.
	bundle, bundleDigest, err := loadArchitectQuestions(req.TaskDirectory, anchorPayload)
	if err != nil {
		return DispositionCandidate{}, err
	}
	question, ok := findQuestion(bundle, req.QuestionID)
	if !ok {
		return DispositionCandidate{}, qdErr(CodeQuestionNotFound, "question %q is not in the recorded architect-questions bundle", req.QuestionID)
	}

	// Verify the answering actor and REQUIRE a resolved disposition authority.
	index, err := authority.LoadPolicyIndex(req.RepositoryRoot)
	if err != nil {
		return DispositionCandidate{}, qdErr(CodeAuthorityUnresolved, "load policy index: %v", err)
	}
	id, enrolled, err := identity.LoadManifest(req.IdentityRoot)
	if err != nil {
		return DispositionCandidate{}, qdErr(CodeActorNotEnrolled, "load identity: %v", err)
	}
	if !enrolled {
		return DispositionCandidate{}, qdErr(CodeActorNotEnrolled, "no enrolled answering-actor identity")
	}
	binding := id.ActorBinding()

	// DisposedAt is anchored to the transition entry's ledger time, so retries
	// are byte-identical. Actor verification and resolution evaluate at that
	// same causal instant.
	disposedAt := strings.TrimSpace(anchor.Entry.ProducedAt)
	evaluatedAt, err := time.Parse(time.RFC3339, disposedAt)
	if err != nil {
		return DispositionCandidate{}, qdErr(CodeAnchorMissing, "anchor entry produced_at is not RFC3339: %v", err)
	}

	verified, err := authority.VerifyActorBinding(binding, identity.Resolver(req.IdentityRoot), index, evaluatedAt)
	if err != nil {
		return DispositionCandidate{}, qdErr(CodeActorNotVerified, "verify actor: %v", err)
	}
	if verified.Status != closureprotocol.ReceiptValid {
		return DispositionCandidate{}, qdErr(CodeActorNotVerified, "answering actor is not verified (%s)", verified.Status)
	}
	actorDigest, err := closureprotocol.SemanticDigest(binding)
	if err != nil {
		return DispositionCandidate{}, qdErr(CodeDigestMismatch, "actor binding digest: %v", err)
	}

	grantID, roleID, err := resolveDispositionAuthority(index, binding, verified, evaluatedAt, req.TaskDirectory)
	if err != nil {
		return DispositionCandidate{}, err
	}

	// Effective scope must be a subset of both the question linkage and the
	// admitted task scope.
	if err := boundEffectiveScope(req, question); err != nil {
		return DispositionCandidate{}, err
	}

	// Answer bytes → canonical digest (answered dispositions only).
	answerDigest := ""
	if len(req.AnswerBytes) > 0 {
		answerDigest = sha256hex(req.AnswerBytes)
	}

	rc := QuestionDispositionReceipt{
		SchemaVersion:                        SchemaVersion,
		Task:                                 receipt.Task,
		ResultBindingDigestSHA256:            receipt.ResultBindingDigestSHA256,
		ResultTransitionReceiptDigestSHA256:  transitionReceiptDigest,
		ArchitectQuestionsBundleDigestSHA256: bundleDigest,
		QuestionID:                           req.QuestionID,
		BlocksClosureDimension:               question.BlocksClosureDimension,
		BlocksClosureBlockers:                append([]string(nil), question.BlocksClosureBlockers...),
		BlocksClaims:                         append([]string(nil), question.BlocksClaims...),
		Disposition:                          req.Disposition,
		Reusability:                          req.Reusability,
		Rationale:                            strings.TrimSpace(req.Rationale),
		AnswerID:                             strings.TrimSpace(req.AnswerID),
		AnswerBytesDigestSHA256:              answerDigest,
		AnsweringActorBindingDigestSHA256:    actorDigest,
		AuthorityGrantID:                     grantID,
		AuthorityRoleID:                      roleID,
		EffectiveScopeDomain:                 strings.TrimSpace(req.EffectiveScopeDomain),
		EffectiveScopeFiles:                  cleanSorted(req.EffectiveScopeFiles),
		EvidenceRefs:                         cleanSorted(req.EvidenceRefs),
		Producer:                             GeneratedBy,
		DisposedAt:                           disposedAt,
	}
	if err := Validate(rc); err != nil {
		return DispositionCandidate{}, qdErr(CodeInvalidReceipt, "%v", err)
	}
	digest, err := Digest(rc)
	if err != nil {
		return DispositionCandidate{}, qdErr(CodeDigestMismatch, "receipt digest: %v", err)
	}
	rc.ReceiptDigestSHA256 = digest

	bytes, err := closureprotocol.CanonicalJSON(rc)
	if err != nil {
		return DispositionCandidate{}, qdErr(CodeInvalidReceipt, "canonical marshal: %v", err)
	}

	return DispositionCandidate{
		Receipt:                        rc,
		ReceiptBytes:                   bytes,
		ReceiptMediaType:               ReceiptMediaType,
		ReceiptByteDigestSHA256:        sha256hex(bytes),
		ExpectedLedgerHeadDigestSHA256: chain.Head.EntryDigestSHA256,
		AnchorEntryDigestSHA256:        anchor.Entry.EntryDigestSHA256,
		PriorDispositions:              dispositionsForQuestionResult(req.TaskDirectory, chain, req.QuestionID, transitionReceiptDigest),
	}, nil
}

// resolveDispositionAuthority requires a RESOLVED, valid operation result for the
// exact bounded question-disposition operation and returns the concrete grant
// and role that authorized it. It never accepts "the actor holds some grant".
func resolveDispositionAuthority(index authority.PolicyIndex, binding closureprotocol.ActorBinding, verified authority.VerifiedActor, evaluatedAt time.Time, taskDir string) (string, string, error) {
	ra, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		return "", "", qdErr(CodeAuthorityUnresolved, "load recorded authority: %v", err)
	}
	plan := closureprotocol.ChangePlan{
		PlanID: "plan.dispose." + ra.Base.Task.ID,
		Operations: []closureprotocol.ChangeOperation{{
			OperationID:       dispositionOperationID,
			Kind:              closureprotocol.OperationDispose,
			TargetKind:        TargetKindArchitectQ,
			SelectedMechanism: closureprotocol.MechanismOwnerRPC,
			RiskClass:         dispositionRiskClass,
		}},
	}
	app := []authority.AuthorityApplicability{{
		OperationID:                 dispositionOperationID,
		AuthorityDomainIDs:          []string{DomainQuestionDisposition},
		RequiredRuntimeMechanismIDs: []string{MechanismPathDisposition},
	}}
	resolution, err := admission.ResolveAuthority(index, admission.ResolveAuthorityInput{
		Actor:                            binding,
		VerifiedActor:                    verified,
		Base:                             ra.Base,
		ChangePlan:                       plan,
		Applicability:                    app,
		PolicyID:                         ra.Base.Policies.Admission,
		ClosureAssessmentDigestSHA256:    ra.Resolution.ClosureAssessmentDigestSHA256,
		AuthorityPolicyGraphDigestSHA256: closureprotocol.MustSemanticDigest(index),
		EvaluatedAt:                      evaluatedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return "", "", qdErr(CodeAuthorityUnresolved, "resolve disposition authority: %v", err)
	}
	var op *closureprotocol.AuthorityResolutionOperation
	for i := range resolution.OperationResults {
		if resolution.OperationResults[i].OperationID == dispositionOperationID {
			op = &resolution.OperationResults[i]
			break
		}
	}
	if op == nil {
		return "", "", qdErr(CodeAuthorityNotGranted, "no resolved operation result for the disposition operation")
	}
	if op.Status != closureprotocol.ReceiptValid {
		return "", "", qdErr(CodeAuthorityNotGranted, "disposition operation not authorized (%s): %s", op.Status, strings.Join(op.Limitations, ","))
	}
	if !containsString(op.GrantIDs, GrantQuestionDisposition) {
		return "", "", qdErr(CodeAuthorityNotGranted, "disposition not authorized by %s", GrantQuestionDisposition)
	}
	role := grantRole(index, GrantQuestionDisposition, verified.VerifiedRoleIDs)
	if role == "" {
		return "", "", qdErr(CodeAuthorityNotGranted, "no verified role authorizes %s", GrantQuestionDisposition)
	}
	return GrantQuestionDisposition, role, nil
}

// boundEffectiveScope fails closed unless every effective-scope file lies within
// both the question linkage (when the question declares files) and the admitted
// task scope, and the domain matches the question's scope.
func boundEffectiveScope(req PrepareRequest, question architecture.OpenQuestion) error {
	files := cleanSorted(req.EffectiveScopeFiles)
	if len(files) == 0 && strings.TrimSpace(req.EffectiveScopeDomain) == "" {
		return nil // no narrower scope asserted
	}
	qFiles := map[string]bool{}
	for _, f := range question.Scope.Files {
		qFiles[strings.TrimSpace(f)] = true
	}
	for _, f := range files {
		if len(qFiles) > 0 && !qFiles[f] {
			return qdErr(CodeScopeBroadened, "effective scope file %q is outside the question linkage", f)
		}
	}
	if d := strings.TrimSpace(req.EffectiveScopeDomain); d != "" {
		qd := strings.TrimSpace(question.Scope.Domain)
		if qd != "" && d != qd {
			return qdErr(CodeScopeBroadened, "effective scope domain %q does not match the question domain %q", d, qd)
		}
	}
	return nil
}

func newStore(taskDir string) *ledger.Store {
	return ledger.NewStore(taskDir, ledger.WithPayloadValidator(dispositionPayloadValidator))
}

// latestResultTransition returns the highest-sequence result_transition_recorded
// entry and its parsed payload from the verified snapshot.
func latestResultTransition(chain ledger.VerifiedChain) (ledger.VerifiedEntry, ledger.TaskEventPayload, bool) {
	for i := len(chain.Entries) - 1; i >= 0; i-- {
		ve := chain.Entries[i]
		if ve.Entry.EventType != closureprotocol.LedgerEventResultTransitionRecorded {
			continue
		}
		data, err := ledger.ReadVerifiedPayload(ve)
		if err != nil {
			continue
		}
		payload, err := ledger.ParseTaskEventPayload(data)
		if err != nil {
			continue
		}
		return ve, payload, true
	}
	return ledger.VerifiedEntry{}, ledger.TaskEventPayload{}, false
}

func loadTransitionReceipt(taskDir string, payload ledger.TaskEventPayload) (closureprotocol.ResultTransitionReceipt, error) {
	ref, ok := payload.Artifacts["result_transition_receipt"]
	if !ok {
		return closureprotocol.ResultTransitionReceipt{}, qdErr(CodeArtifactReadFailed, "transition event has no result_transition_receipt artifact")
	}
	data, err := readArtifact(taskDir, ref)
	if err != nil {
		return closureprotocol.ResultTransitionReceipt{}, err
	}
	var receipt closureprotocol.ResultTransitionReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return closureprotocol.ResultTransitionReceipt{}, qdErr(CodeArtifactReadFailed, "decode transition receipt: %v", err)
	}
	return receipt, nil
}

func loadArchitectQuestions(taskDir string, payload ledger.TaskEventPayload) (resultpipeline.ArchitectQuestionsBundle, string, error) {
	ref, ok := payload.Artifacts["result_stage."+string(closureprotocol.StageArchitectQuestions)]
	if !ok {
		return resultpipeline.ArchitectQuestionsBundle{}, "", qdErr(CodeArtifactReadFailed, "transition event has no architect_questions stage artifact")
	}
	data, err := readArtifact(taskDir, ref)
	if err != nil {
		return resultpipeline.ArchitectQuestionsBundle{}, "", err
	}
	var bundle resultpipeline.ArchitectQuestionsBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return resultpipeline.ArchitectQuestionsBundle{}, "", qdErr(CodeArtifactReadFailed, "decode architect-questions bundle: %v", err)
	}
	digest, err := closureprotocol.SemanticDigest(bundle)
	if err != nil {
		return resultpipeline.ArchitectQuestionsBundle{}, "", qdErr(CodeDigestMismatch, "bundle digest: %v", err)
	}
	return bundle, digest, nil
}

func findQuestion(bundle resultpipeline.ArchitectQuestionsBundle, questionID string) (architecture.OpenQuestion, bool) {
	for _, q := range bundle.Dialogue.OpenQuestions {
		if q.ID == questionID || architecture.StableOpenQuestionID(q) == questionID {
			return q, true
		}
	}
	return architecture.OpenQuestion{}, false
}

// dispositionsForQuestionResult collects prior disposition receipts on the chain
// that bind the same question and the same result transition.
func dispositionsForQuestionResult(taskDir string, chain ledger.VerifiedChain, questionID, transitionReceiptDigest string) []QuestionDispositionReceipt {
	var out []QuestionDispositionReceipt
	for _, ve := range chain.Entries {
		if ve.Entry.EventType != closureprotocol.LedgerEventQuestionDispositionRecorded {
			continue
		}
		rc, _, err := readDispositionReceipt(taskDir, ve)
		if err != nil {
			continue
		}
		if rc.QuestionID == questionID && rc.ResultTransitionReceiptDigestSHA256 == transitionReceiptDigest {
			out = append(out, rc)
		}
	}
	return out
}

func readDispositionReceipt(taskDir string, ve ledger.VerifiedEntry) (QuestionDispositionReceipt, closureprotocol.LedgerPayloadRef, error) {
	data, err := ledger.ReadVerifiedPayload(ve)
	if err != nil {
		return QuestionDispositionReceipt{}, closureprotocol.LedgerPayloadRef{}, err
	}
	payload, err := ledger.ParseTaskEventPayload(data)
	if err != nil {
		return QuestionDispositionReceipt{}, closureprotocol.LedgerPayloadRef{}, err
	}
	ref, ok := payload.Artifacts[ArtifactKeyReceipt]
	if !ok {
		return QuestionDispositionReceipt{}, closureprotocol.LedgerPayloadRef{}, qdErr(CodeArtifactReadFailed, "disposition event has no receipt artifact")
	}
	raw, err := readArtifact(taskDir, ref)
	if err != nil {
		return QuestionDispositionReceipt{}, closureprotocol.LedgerPayloadRef{}, err
	}
	var rc QuestionDispositionReceipt
	if err := json.Unmarshal(raw, &rc); err != nil {
		return QuestionDispositionReceipt{}, closureprotocol.LedgerPayloadRef{}, qdErr(CodeArtifactReadFailed, "decode disposition receipt: %v", err)
	}
	return rc, ref, nil
}

// readArtifact reads a content-addressed artifact and fails closed unless its
// bytes reproduce the ref's digest.
func readArtifact(taskDir string, ref closureprotocol.LedgerPayloadRef) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(taskDir, filepath.FromSlash(ref.Path)))
	if err != nil {
		return nil, qdErr(CodeArtifactReadFailed, "read %q: %v", ref.Path, err)
	}
	if sha256hex(data) != ref.DigestSHA256 {
		return nil, qdErr(CodeDigestMismatch, "artifact %q byte digest does not match its ref", ref.Path)
	}
	return data, nil
}

func grantRole(index authority.PolicyIndex, grantID string, verifiedRoles []string) string {
	grant, ok := index.AuthorityGrants[grantID]
	if !ok {
		return ""
	}
	for _, r := range grant.ActorRoleIDs {
		if containsString(verifiedRoles, r) {
			return r
		}
	}
	return ""
}

func containsString(in []string, want string) bool {
	for _, s := range in {
		if s == want {
			return true
		}
	}
	return false
}

func cleanSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
