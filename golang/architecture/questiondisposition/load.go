// SPDX-License-Identifier: Apache-2.0

package questiondisposition

import (
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// RecordedDisposition is a disposition receipt reconstructed and revalidated
// from one verified ledger snapshot.
type RecordedDisposition struct {
	Receipt           QuestionDispositionReceipt
	EntryDigestSHA256 string
	LedgerSequence    int
	ReceiptRef        closureprotocol.LedgerPayloadRef
}

// LoadRecordedDisposition finds the durable disposition whose self-excluding
// receipt digest equals receiptDigest, verifying integrity from a single
// verified snapshot: the receipt's stored digest must reproduce and its shape
// must validate.
func LoadRecordedDisposition(taskDir, receiptDigest string) (RecordedDisposition, error) {
	if strings.TrimSpace(receiptDigest) == "" {
		return RecordedDisposition{}, qdErr(CodeReloadFailed, "receipt digest is required")
	}
	store := newStore(taskDir)
	chain, err := store.VerifyChain()
	if err != nil {
		return RecordedDisposition{}, qdErr(CodeChainVerifyFailed, "%v", err)
	}
	for _, ve := range chain.Entries {
		if ve.Entry.EventType != closureprotocol.LedgerEventQuestionDispositionRecorded {
			continue
		}
		rc, ref, err := readDispositionReceipt(taskDir, ve)
		if err != nil {
			return RecordedDisposition{}, qdErr(CodeReloadFailed, "%v", err)
		}
		recomputed, err := Digest(rc)
		if err != nil {
			return RecordedDisposition{}, qdErr(CodeDigestMismatch, "%v", err)
		}
		if recomputed != receiptDigest {
			continue
		}
		if rc.ReceiptDigestSHA256 != "" && rc.ReceiptDigestSHA256 != recomputed {
			return RecordedDisposition{}, qdErr(CodeDigestMismatch, "stored receipt digest does not reproduce")
		}
		if err := Validate(rc); err != nil {
			return RecordedDisposition{}, qdErr(CodeInvalidReceipt, "%v", err)
		}
		return RecordedDisposition{
			Receipt:           rc,
			EntryDigestSHA256: ve.Entry.EntryDigestSHA256,
			LedgerSequence:    ve.Entry.Sequence,
			ReceiptRef:        ref,
		}, nil
	}
	return RecordedDisposition{}, qdErr(CodeReloadFailed, "disposition %q not found", receiptDigest)
}

// ListRecordedDispositions returns every durable disposition on the ledger in
// sequence order, revalidated from one verified snapshot.
func ListRecordedDispositions(taskDir string) ([]RecordedDisposition, error) {
	store := newStore(taskDir)
	chain, err := store.VerifyChain()
	if err != nil {
		return nil, qdErr(CodeChainVerifyFailed, "%v", err)
	}
	var out []RecordedDisposition
	for _, ve := range chain.Entries {
		if ve.Entry.EventType != closureprotocol.LedgerEventQuestionDispositionRecorded {
			continue
		}
		rc, ref, err := readDispositionReceipt(taskDir, ve)
		if err != nil {
			return nil, qdErr(CodeReloadFailed, "%v", err)
		}
		if err := Validate(rc); err != nil {
			return nil, qdErr(CodeInvalidReceipt, "%v", err)
		}
		out = append(out, RecordedDisposition{
			Receipt:           rc,
			EntryDigestSHA256: ve.Entry.EntryDigestSHA256,
			LedgerSequence:    ve.Entry.Sequence,
			ReceiptRef:        ref,
		})
	}
	return out, nil
}
