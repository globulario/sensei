// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

func appendEntry(ctx context.Context, s *Store, req AppendRequest) (AppendResult, error) {
	if err := ctx.Err(); err != nil {
		return AppendResult{}, err
	}
	req.TaskID = sanitizeTaskID(req.TaskID)
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.ExpectedHeadDigestSHA256 = strings.TrimSpace(req.ExpectedHeadDigestSHA256)
	req.ProducerID = strings.TrimSpace(req.ProducerID)
	if req.TaskID == "" || req.SessionID == "" || req.ProducerID == "" {
		return AppendResult{}, fmt.Errorf("task_id, session_id, and producer_id are required")
	}
	if req.ProducedAt.IsZero() {
		return AppendResult{}, fmt.Errorf("produced_at is required")
	}
	release, err := acquireLock(ctx, s.lockDir())
	if err != nil {
		return AppendResult{}, err
	}
	defer release()

	report, err := verifyTaskLedger(ctx, s.taskDir, s.payloadValidator)
	if err != nil {
		return AppendResult{}, err
	}
	if !report.Valid {
		recovered, rerr := recoverUnpublishedHead(ctx, s)
		if rerr != nil {
			return AppendResult{}, rerr
		}
		if !recovered {
			return AppendResult{}, fmt.Errorf("cannot append to invalid ledger")
		}
		if report, err = verifyTaskLedger(ctx, s.taskDir, s.payloadValidator); err != nil {
			return AppendResult{}, err
		}
		if !report.Valid {
			return AppendResult{}, fmt.Errorf("cannot append to invalid ledger")
		}
	}
	currentHead := report.HeadDigestSHA256
	if currentHead != req.ExpectedHeadDigestSHA256 {
		if report.EntryCount > 0 && currentHead != "" && replayMatchesCurrentHead(ctx, s, req, report) {
			chain, err := loadVerifiedChain(ctx, s.taskDir, s.payloadValidator)
			if err != nil {
				return AppendResult{}, err
			}
			last := chain.Entries[len(chain.Entries)-1]
			return AppendResult{Entry: last.Entry, Head: chain.Head, PayloadPath: last.PayloadPath, Replay: true}, nil
		}
		return AppendResult{}, ErrStaleHead{Expected: req.ExpectedHeadDigestSHA256, Actual: currentHead, Sequence: report.EntryCount}
	}

	payload, err := renderPayload(req.Payload, req.PayloadMediaType)
	if err != nil {
		return AppendResult{}, err
	}
	if err := storePayloadArtifacts(s.taskDir, payload); err != nil {
		return AppendResult{}, err
	}
	if s.payloadValidator != nil {
		if err := s.payloadValidator(req.EventType, payload.mediaType, payload.data); err != nil {
			return AppendResult{}, err
		}
	}

	entry := Entry{
		Sequence:                  report.EntryCount + 1,
		PreviousEntryDigestSHA256: currentHead,
		EventType:                 req.EventType,
		Task: closureprotocol.TaskBinding{
			ID:        req.TaskID,
			SessionID: req.SessionID,
		},
		Payload: closureprotocol.LedgerPayloadRef{
			Path:         payload.path,
			MediaType:    payload.mediaType,
			DigestSHA256: payload.semanticDigest,
		},
		Producer:   req.ProducerID,
		ProducedAt: req.ProducedAt.UTC().Format(time.RFC3339),
	}
	if err := closureprotocol.ValidateLedgerEntry(entry); err != nil {
		return AppendResult{}, err
	}
	digest, err := closureprotocol.LedgerEntryDigest(entry)
	if err != nil {
		return AppendResult{}, err
	}
	entry.EntryDigestSHA256 = digest

	// writeEntry is atomic (temp + rename): once it returns nil the entry is
	// durable and the append is committed. A failure here is genuinely pre-commit.
	if err := writeEntry(filepath.Join(s.ledgerDir(), ledgerEntryFilename(entry.Sequence, entry.EventType, digest)), entry); err != nil {
		return AppendResult{}, err
	}
	head := Head{
		SchemaVersion:     HeadSchemaVersion,
		TaskID:            req.TaskID,
		Sequence:          entry.Sequence,
		EntryDigestSHA256: digest,
		EntryPath:         filepath.ToSlash(filepath.Join("ledger", ledgerEntryFilename(entry.Sequence, entry.EventType, digest))),
	}
	// The entry is now durable. Publication is retried here, still under the lock,
	// because this is the only owner that knows the pointer it is writing. If every
	// attempt fails that is a POST-commit condition, not a pre-commit failure:
	// ErrEntryDurable carries the committed entry identity, and generic readers
	// refuse the ledger until a later Append republishes HEAD.
	// Tests reach this branch only through writeHead's sensei_faultinject seam.
	if err := publishHead(s.headPath(), head); err != nil {
		return AppendResult{Entry: entry, Head: head, PayloadPath: payload.path},
			ErrEntryDurable{Entry: entry, Head: head, Detail: err.Error()}
	}
	return AppendResult{Entry: entry, Head: head, PayloadPath: payload.path}, nil
}

// headPublicationAttempts bounds the HEAD writes one publication may make.
const headPublicationAttempts = 3

// publishHead is the bounded HEAD-publication retry. Only appendEntry calls it.
func publishHead(path string, head Head) error {
	var err error
	for attempt := 0; attempt < headPublicationAttempts; attempt++ {
		if err = writeHead(path, head); err == nil {
			return nil
		}
	}
	return fmt.Errorf("HEAD not published after %d attempts: %w", headPublicationAttempts, err)
}

// recoverUnpublishedHead republishes HEAD when, and only when, the ledger's sole
// defect is the residue an interrupted publication by appendEntry leaves behind.
// It runs only inside appendEntry, which holds the append lock.
//
// That residue has exactly two shapes, because writeFileAtomic replaces HEAD by
// rename and never removes a published pointer:
//
//	head_stale    HEAD names the entry immediately before the last one
//	head_missing  the chain is a single entry and HEAD was never published
//
// Anything else is refused. A HEAD naming an entry the chain no longer contains is
// what tail truncation produces (#352), and rewriting it would resurrect the
// authority the deleted entries consumed. A missing HEAD on a longer chain cannot
// come from an interrupted publication, so it is not recovered either.
func recoverUnpublishedHead(ctx context.Context, s *Store) (bool, error) {
	chain, report, head, err := verifyAndLoadChain(ctx, s.taskDir, s.payloadValidator)
	if err != nil {
		return false, err
	}
	if len(report.Errors) > 0 || head.finding == nil || !isUnpublishedHeadResidue(chain, head) {
		return false, nil
	}
	if err := publishHead(s.headPath(), chain.Head); err != nil {
		return false, err
	}
	return true, nil
}

func isUnpublishedHeadResidue(chain VerifiedChain, head headObservation) bool {
	n := len(chain.Entries)
	switch head.finding.Code {
	case "ledger.head_missing":
		return n == 1
	case "ledger.head_stale":
		if n < 2 {
			return false
		}
		prev := chain.Entries[n-2]
		return head.published == Head{
			SchemaVersion:     HeadSchemaVersion,
			TaskID:            chain.TaskID,
			Sequence:          prev.Entry.Sequence,
			EntryDigestSHA256: prev.Entry.EntryDigestSHA256,
			EntryPath:         filepath.ToSlash(filepath.Join("ledger", filepath.Base(prev.EntryPath))),
		}
	}
	return false
}

func replayMatchesCurrentHead(ctx context.Context, s *Store, req AppendRequest, report VerificationReport) bool {
	payload, err := renderPayload(req.Payload, req.PayloadMediaType)
	if err != nil {
		return false
	}
	chain, err := loadVerifiedChain(ctx, s.taskDir, s.payloadValidator)
	if err != nil || len(chain.Entries) == 0 {
		return false
	}
	last := chain.Entries[len(chain.Entries)-1].Entry
	return last.Task.ID == sanitizeTaskID(req.TaskID) &&
		last.Task.SessionID == strings.TrimSpace(req.SessionID) &&
		last.EventType == req.EventType &&
		last.Payload.DigestSHA256 == payload.semanticDigest &&
		last.Producer == strings.TrimSpace(req.ProducerID) &&
		last.ProducedAt == req.ProducedAt.UTC().Format(time.RFC3339)
}
