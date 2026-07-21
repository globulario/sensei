// SPDX-License-Identifier: AGPL-3.0-only

package questionpromotion

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// Promotion-local journal event vocabulary. It is deliberately NOT part of
// closureprotocol.LedgerEventTypes: the promotion journal is recovery metadata,
// never a task event, and no task reducer or task-phase projection consumes it.
type JournalEventType string

const (
	EventPrepared           JournalEventType = "prepared"
	EventSourceCommitted    JournalEventType = "source_committed"
	EventGraphVerified      JournalEventType = "graph_verified"
	EventPromotionCommitted JournalEventType = "promotion_committed"
)

var journalOrder = map[JournalEventType]int{
	EventPrepared: 0, EventSourceCommitted: 1, EventGraphVerified: 2, EventPromotionCommitted: 3,
}

// JournalEntry is one hash-chained transition. The payload is the small state
// world for that transition; PayloadDigestSHA256 binds it so recovery can compare
// recomputed disk state to what the journal recorded.
type JournalEntry struct {
	Sequence                  int              `json:"sequence"`
	PreviousEntryDigestSHA256 string           `json:"previous_entry_digest_sha256,omitempty"`
	EventType                 JournalEventType `json:"event_type"`
	Payload                   json.RawMessage  `json:"payload,omitempty"`
	PayloadDigestSHA256       string           `json:"payload_digest_sha256"`
	ProducedAt                string           `json:"produced_at"`
	EntryDigestSHA256         string           `json:"entry_digest_sha256,omitempty"`
}

// Journal is a promotion-local, append-only, hash-chained, CAS-guarded event log
// under <promotionDir>/journal/.
type Journal struct{ dir string }

// JournalError is a typed journal failure (tampered / stale / illegal transition).
type JournalError struct{ Code, Detail string }

func (e *JournalError) Error() string { return "promotion journal " + e.Code + ": " + e.Detail }

func OpenJournal(promotionDir string) *Journal {
	return &Journal{dir: filepath.Join(promotionDir, "journal")}
}

func (j *Journal) entryPath(seq int) string {
	return filepath.Join(j.dir, fmt.Sprintf("%06d.json", seq))
}

func entryDigest(e JournalEntry) (string, error) {
	e.EntryDigestSHA256 = ""
	return closureprotocol.SemanticDigest(e)
}

// Verify loads and verifies the whole chain: contiguous sequence, recomputed
// entry digests, previous-digest linkage, and payload-digest agreement. A
// tampered, reordered, or truncated journal fails closed.
func (j *Journal) Verify() ([]JournalEntry, error) {
	files, err := os.ReadDir(j.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, &JournalError{Code: "unreadable", Detail: err.Error()}
	}
	var seqs []int
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		n, perr := strconv.Atoi(strings.TrimSuffix(f.Name(), ".json"))
		if perr != nil {
			continue
		}
		seqs = append(seqs, n)
	}
	sort.Ints(seqs)
	var chain []JournalEntry
	prevDigest := ""
	for i, seq := range seqs {
		if seq != i {
			return nil, &JournalError{Code: "sequence_gap", Detail: fmt.Sprintf("expected %d, got %d", i, seq)}
		}
		data, rerr := os.ReadFile(j.entryPath(seq))
		if rerr != nil {
			return nil, &JournalError{Code: "unreadable", Detail: rerr.Error()}
		}
		var e JournalEntry
		if uerr := json.Unmarshal(data, &e); uerr != nil {
			return nil, &JournalError{Code: "malformed", Detail: uerr.Error()}
		}
		if e.Sequence != seq {
			return nil, &JournalError{Code: "sequence_mismatch", Detail: fmt.Sprintf("file %d claims %d", seq, e.Sequence)}
		}
		if e.PreviousEntryDigestSHA256 != prevDigest {
			return nil, &JournalError{Code: "previous_digest_mismatch", Detail: fmt.Sprintf("entry %d", seq)}
		}
		wantEntry, derr := entryDigest(e)
		if derr != nil {
			return nil, &JournalError{Code: "digest_error", Detail: derr.Error()}
		}
		if e.EntryDigestSHA256 != wantEntry {
			return nil, &JournalError{Code: "entry_digest_mismatch", Detail: fmt.Sprintf("entry %d", seq)}
		}
		wantPayload, perr := closureprotocol.SemanticDigest(json.RawMessage(e.Payload))
		if perr == nil && len(e.Payload) > 0 && e.PayloadDigestSHA256 != wantPayload {
			return nil, &JournalError{Code: "payload_digest_mismatch", Detail: fmt.Sprintf("entry %d", seq)}
		}
		if i > 0 {
			if journalOrder[e.EventType] != journalOrder[chain[i-1].EventType]+1 {
				return nil, &JournalError{Code: "illegal_transition", Detail: fmt.Sprintf("%s after %s", e.EventType, chain[i-1].EventType)}
			}
		} else if e.EventType != EventPrepared {
			return nil, &JournalError{Code: "illegal_transition", Detail: "first event must be prepared"}
		}
		prevDigest = e.EntryDigestSHA256
		chain = append(chain, e)
	}
	return chain, nil
}

// Head returns the last verified entry and its digest, or ("", false) when empty.
func (j *Journal) Head() (JournalEntry, string, bool, error) {
	chain, err := j.Verify()
	if err != nil {
		return JournalEntry{}, "", false, err
	}
	if len(chain) == 0 {
		return JournalEntry{}, "", false, nil
	}
	last := chain[len(chain)-1]
	return last, last.EntryDigestSHA256, true, nil
}

// Append adds one transition under compare-and-swap on the expected head. An
// exact replay (same event type + payload digest at the same head) returns the
// existing entry and writes nothing. Illegal transitions fail closed.
func (j *Journal) Append(expectedHead string, event JournalEventType, payload any, producedAt string) (JournalEntry, bool, error) {
	chain, err := j.Verify()
	if err != nil {
		return JournalEntry{}, false, err
	}
	curHead := ""
	if len(chain) > 0 {
		curHead = chain[len(chain)-1].EntryDigestSHA256
	}
	payloadBytes, merr := json.Marshal(payload)
	if merr != nil {
		return JournalEntry{}, false, &JournalError{Code: "payload_marshal", Detail: merr.Error()}
	}
	payloadDigest, _ := closureprotocol.SemanticDigest(json.RawMessage(payloadBytes))

	// Exact replay: the head already IS this event with this payload.
	if len(chain) > 0 {
		last := chain[len(chain)-1]
		if last.EventType == event && last.PayloadDigestSHA256 == payloadDigest {
			return last, true, nil
		}
	}
	if curHead != expectedHead {
		return JournalEntry{}, false, &JournalError{Code: "stale_head", Detail: "expected head does not match"}
	}
	// Transition legality.
	if len(chain) == 0 {
		if event != EventPrepared {
			return JournalEntry{}, false, &JournalError{Code: "illegal_transition", Detail: "first event must be prepared"}
		}
	} else if journalOrder[event] != journalOrder[chain[len(chain)-1].EventType]+1 {
		return JournalEntry{}, false, &JournalError{Code: "illegal_transition", Detail: fmt.Sprintf("%s after %s", event, chain[len(chain)-1].EventType)}
	}

	seq := len(chain)
	e := JournalEntry{
		Sequence:                  seq,
		PreviousEntryDigestSHA256: curHead,
		EventType:                 event,
		Payload:                   json.RawMessage(payloadBytes),
		PayloadDigestSHA256:       payloadDigest,
		ProducedAt:                producedAt,
	}
	digest, derr := entryDigest(e)
	if derr != nil {
		return JournalEntry{}, false, &JournalError{Code: "digest_error", Detail: derr.Error()}
	}
	e.EntryDigestSHA256 = digest
	out, merr := json.MarshalIndent(e, "", "  ")
	if merr != nil {
		return JournalEntry{}, false, &JournalError{Code: "marshal", Detail: merr.Error()}
	}
	if err := os.MkdirAll(j.dir, 0o755); err != nil {
		return JournalEntry{}, false, &JournalError{Code: "mkdir", Detail: err.Error()}
	}
	if err := writeFileAtomic(j.entryPath(seq), out); err != nil {
		return JournalEntry{}, false, &JournalError{Code: "write", Detail: err.Error()}
	}
	return e, false, nil
}

// writeFileAtomic writes via temp + fsync + rename.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
