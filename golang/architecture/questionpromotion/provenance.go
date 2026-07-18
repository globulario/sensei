// SPDX-License-Identifier: AGPL-3.0-only

package questionpromotion

import (
	"strings"

	"github.com/globulario/sensei/golang/rdf"
)

// Provenance node IRIs (bare) for the lineage a promotion receipt anchors. They
// are content-addressed / id-addressed so the same lineage always mints the same
// nodes, and they match the bare IRIs the repograph store adapter indexes.

func ReceiptNodeIRI(in QuestionPromotionReceipt) string {
	return bareIRI(rdf.MintIRI(rdf.ClassQuestionPromotionReceipt, in.ReceiptDigestSHA256))
}

func DispositionNodeIRI(in QuestionPromotionReceipt) string {
	return bareIRI(rdf.MintIRI(rdf.ClassQuestionDispositionReceipt, in.QuestionDispositionReceiptDigestSHA256))
}

func AnswerNodeIRI(in QuestionPromotionReceipt) string {
	return bareIRI(rdf.MintIRI(rdf.ClassArchitectAnswer, in.AnswerID))
}

func QuestionNodeIRI(in QuestionPromotionReceipt) string {
	return bareIRI(rdf.MintIRI(rdf.ClassOpenQuestion, in.QuestionID))
}

func TaskNodeIRI(in QuestionPromotionReceipt) string {
	return bareIRI(rdf.MintIRI(rdf.ClassTask, in.Task.ID))
}

func SessionNodeIRI(in QuestionPromotionReceipt) string {
	return bareIRI(rdf.MintIRI(rdf.ClassTaskSession, in.Task.SessionID))
}

func ResultNodeIRI(in QuestionPromotionReceipt) string {
	return bareIRI(rdf.MintIRI(rdf.ClassResultBinding, in.ResultBindingDigestSHA256))
}

// ProvenanceTriples renders the canonical N-Triples for the promotion lineage:
//
//	governed node -> promotion receipt -> disposition receipt -> architect answer
//	-> open question -> task -> session, plus promotion receipt -> result binding.
//
// It carries lineage only — no certification, completion, phase, or correctness
// meaning — and it emits raw dialogue for none of the nodes; only stable typed
// identities. It performs NO graph mutation or persistence: the later promotion
// transaction emits these into the rebuilt repository graph.
func ProvenanceTriples(in QuestionPromotionReceipt) []byte {
	receipt := ReceiptNodeIRI(in)
	disposition := DispositionNodeIRI(in)
	answer := AnswerNodeIRI(in)
	question := QuestionNodeIRI(in)
	task := TaskNodeIRI(in)
	session := SessionNodeIRI(in)
	result := ResultNodeIRI(in)
	node := in.GovernedNodeIRI

	var b strings.Builder
	typed := func(subj, class string) { triple(&b, subj, rdf.PropType, class) }
	// Type the promotion-specific nodes so the lineage is self-describing.
	typed(receipt, rdf.ClassQuestionPromotionReceipt)
	typed(disposition, rdf.ClassQuestionDispositionReceipt)
	typed(answer, rdf.ClassArchitectAnswer)
	typed(question, rdf.ClassOpenQuestion)
	typed(task, rdf.ClassTask)
	typed(session, rdf.ClassTaskSession)
	typed(result, rdf.ClassResultBinding)

	// The directed lineage chain.
	triple(&b, node, rdf.PropPromotedVia, receipt)
	triple(&b, receipt, rdf.PropRecordsDisposition, disposition)
	triple(&b, disposition, rdf.PropResolvesAnswer, answer)
	triple(&b, answer, rdf.PropAnswersQuestion, question)
	triple(&b, question, rdf.PropRaisedForTask, task)
	triple(&b, task, rdf.PropInSession, session)
	triple(&b, receipt, rdf.PropForResult, result)

	return []byte(b.String())
}

func triple(b *strings.Builder, subj, pred, obj string) {
	b.WriteString(rdf.IRI(subj))
	b.WriteByte(' ')
	b.WriteString(rdf.IRI(pred))
	b.WriteByte(' ')
	b.WriteString(rdf.IRI(obj))
	b.WriteString(" .\n")
}
