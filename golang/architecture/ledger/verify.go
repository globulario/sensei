// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

func verifyTaskLedger(ctx context.Context, taskDir string, validator PayloadValidator) (VerificationReport, error) {
	chain, report, err := verifyAndLoadChain(ctx, taskDir, validator)
	if err != nil {
		return VerificationReport{}, err
	}
	if len(chain.Entries) > 0 {
		report.HeadDigestSHA256 = chain.Head.EntryDigestSHA256
		report.TaskID = chain.TaskID
	}
	report.EntryCount = len(chain.Entries)
	if chain.TaskDir != "" && len(chain.Entries) > 0 && len(report.Errors) == 0 {
		if set, err := Project(chain); err == nil {
			report.ProjectionState = ProjectionState(chain.TaskDir, set)
		}
	}
	report.Valid = len(report.Errors) == 0
	return report, nil
}

func loadVerifiedChain(ctx context.Context, taskDir string, validator PayloadValidator) (VerifiedChain, error) {
	chain, report, err := verifyAndLoadChain(ctx, taskDir, validator)
	if err != nil {
		return VerifiedChain{}, err
	}
	if len(report.Errors) > 0 {
		return VerifiedChain{}, fmt.Errorf("invalid ledger chain")
	}
	return chain, nil
}

// loadChainForDerivedRepair loads the entry chain for DERIVED-STATE REPAIR only.
// It accepts a chain whose only remaining defects are in HEAD itself, because HEAD
// is derived state that the repair is about to recompute from the entries; every
// other verification error still refuses. Nothing that reads a task's authority may
// use it. Authority readers take loadVerifiedChain, which refuses a HEAD that
// disagrees with its chain, so a truncated ledger can never be folded as truth.
func loadChainForDerivedRepair(ctx context.Context, taskDir string, validator PayloadValidator) (VerifiedChain, error) {
	chain, report, err := verifyAndLoadChain(ctx, taskDir, validator)
	if err != nil {
		return VerifiedChain{}, err
	}
	for _, e := range report.Errors {
		if !isDerivedHeadFinding(e.Code) {
			return VerifiedChain{}, fmt.Errorf("invalid ledger chain")
		}
	}
	return chain, nil
}

// isDerivedHeadFinding reports whether a verification error is about HEAD.yaml
// rather than about the entry chain. These findings still make the ledger's
// verdict invalid -- Store.Verify reports them as errors and report.Valid is
// false -- they merely do not block the repair that recomputes HEAD.
func isDerivedHeadFinding(code string) bool {
	switch code {
	case "ledger.head_stale", "ledger.head_missing":
		return true
	default:
		return false
	}
}

func verifyAndLoadChain(ctx context.Context, taskDir string, validator PayloadValidator) (VerifiedChain, VerificationReport, error) {
	s := NewStore(taskDir, WithPayloadValidator(validator))
	files, err := listLedgerEntryFiles(s.ledgerDir())
	if err != nil {
		return VerifiedChain{}, VerificationReport{}, err
	}
	var (
		out       = VerifiedChain{TaskDir: taskDir}
		report    VerificationReport
		usedPaths = map[string]bool{}
	)
	for idx, path := range files {
		entry, err := readEntry(path)
		if err != nil {
			report.Errors = append(report.Errors, VerificationError{Code: "ledger.entry_unreadable", Detail: err.Error(), Path: filepath.ToSlash(path)})
			continue
		}
		if err := closureprotocol.ValidateLedgerEntry(entry); err != nil {
			report.Errors = append(report.Errors, VerificationError{Code: "ledger.entry_invalid", Detail: err.Error(), Path: filepath.ToSlash(path)})
			continue
		}
		if seq, err := parseSequenceFromFilename(filepath.Base(path)); err != nil || seq != entry.Sequence {
			detail := "entry filename sequence does not match content"
			if err != nil {
				detail = err.Error()
			}
			report.Errors = append(report.Errors, VerificationError{Code: "ledger.sequence_filename_mismatch", Detail: detail, Path: filepath.ToSlash(path)})
		}
		expectedSeq := idx + 1
		if entry.Sequence != expectedSeq {
			report.Errors = append(report.Errors, VerificationError{Code: "ledger.sequence_gap", Detail: fmt.Sprintf("expected sequence %d got %d", expectedSeq, entry.Sequence), Path: filepath.ToSlash(path)})
		}
		recomputed, err := closureprotocol.LedgerEntryDigest(entry)
		if err != nil || recomputed != entry.EntryDigestSHA256 {
			detail := "entry digest mismatch"
			if err != nil {
				detail = err.Error()
			}
			report.Errors = append(report.Errors, VerificationError{Code: "ledger.entry_digest_mismatch", Detail: detail, Path: filepath.ToSlash(path)})
		}
		if idx == 0 {
			out.TaskID = entry.Task.ID
			if entry.PreviousEntryDigestSHA256 != "" {
				report.Errors = append(report.Errors, VerificationError{Code: "ledger.first_entry_previous_digest", Detail: "first entry must not carry previous digest", Path: filepath.ToSlash(path)})
			}
		} else {
			prev := out.Entries[idx-1].Entry
			if entry.PreviousEntryDigestSHA256 != prev.EntryDigestSHA256 {
				report.Errors = append(report.Errors, VerificationError{Code: "ledger.previous_digest_mismatch", Detail: "previous digest does not match prior entry", Path: filepath.ToSlash(path)})
			}
			if entry.Task.ID != out.TaskID {
				report.Errors = append(report.Errors, VerificationError{Code: "ledger.task_id_changed", Detail: "task id changes within one chain", Path: filepath.ToSlash(path)})
			}
		}
		payloadPath := filepath.Join(taskDir, filepath.FromSlash(entry.Payload.Path))
		usedPaths[filepath.ToSlash(entry.Payload.Path)] = true
		info, err := os.Lstat(payloadPath)
		if err != nil {
			report.Errors = append(report.Errors, VerificationError{Code: "ledger.payload_missing", Detail: err.Error(), Path: filepath.ToSlash(payloadPath)})
		} else if info.Mode()&os.ModeSymlink != 0 {
			report.Errors = append(report.Errors, VerificationError{Code: "ledger.payload_symlink", Detail: "payload path must not be a symlink", Path: filepath.ToSlash(payloadPath)})
		} else {
			data, err := os.ReadFile(payloadPath)
			if err != nil {
				report.Errors = append(report.Errors, VerificationError{Code: "ledger.payload_unreadable", Detail: err.Error(), Path: filepath.ToSlash(payloadPath)})
			} else {
				digest, err := semanticDigestForBytesCtx(ctx, entry.Payload.MediaType, data)
				if err != nil {
					report.Errors = append(report.Errors, VerificationError{Code: "ledger.payload_render_failed", Detail: err.Error(), Path: filepath.ToSlash(payloadPath)})
				} else if digest != entry.Payload.DigestSHA256 {
					report.Errors = append(report.Errors, VerificationError{Code: "ledger.payload_digest_mismatch", Detail: "payload digest does not match stored payload", Path: filepath.ToSlash(payloadPath)})
				}
				if validator != nil {
					if err := validator(entry.EventType, entry.Payload.MediaType, data); err != nil {
						report.Errors = append(report.Errors, VerificationError{Code: "ledger.payload_schema_invalid", Detail: err.Error(), Path: filepath.ToSlash(payloadPath)})
					}
				}
			}
		}
		out.Entries = append(out.Entries, VerifiedEntry{Entry: entry, EntryPath: filepath.ToSlash(path), PayloadPath: filepath.ToSlash(payloadPath)})
	}
	if len(out.Entries) > 0 {
		last := out.Entries[len(out.Entries)-1]
		out.Head = Head{
			SchemaVersion:     HeadSchemaVersion,
			TaskID:            out.TaskID,
			Sequence:          last.Entry.Sequence,
			EntryDigestSHA256: last.Entry.EntryDigestSHA256,
			EntryPath:         filepath.ToSlash(filepath.Join("ledger", filepath.Base(last.EntryPath))),
		}
	}
	// A HEAD that disagrees with the recomputed chain is evidence that history is
	// damaged, not an advisory: deleting the highest-sequence entry leaves HEAD
	// pointing at an entry that is no longer on the chain, and a report that stays
	// valid would let a spent mutation capability come back. An ABSENT HEAD is its
	// own fact and must be stated too -- treating absence as nothing to say leaves
	// the identical truncation available for the cost of one more deletion. Only a
	// chain with no entries at all may have no HEAD; that is a task that has not
	// started, not a task whose tail was removed.
	head, err := readHead(s.headPath())
	switch {
	case err == nil:
		if head.EntryDigestSHA256 != out.Head.EntryDigestSHA256 || head.Sequence != out.Head.Sequence || head.EntryPath != out.Head.EntryPath {
			report.Errors = append(report.Errors, VerificationError{Code: "ledger.head_stale", Detail: "HEAD does not match verified last entry", Path: filepath.ToSlash(s.headPath())})
		}
	case os.IsNotExist(err):
		if len(out.Entries) > 0 {
			report.Errors = append(report.Errors, VerificationError{Code: "ledger.head_missing", Detail: "chain has entries but no HEAD", Path: filepath.ToSlash(s.headPath())})
		}
	default:
		report.Errors = append(report.Errors, VerificationError{Code: "ledger.head_unreadable", Detail: err.Error(), Path: filepath.ToSlash(s.headPath())})
	}

	artifactRoot := filepath.Join(taskDir, "artifacts", "sha256")
	artifactEntries, err := os.ReadDir(artifactRoot)
	if err == nil {
		for _, entry := range artifactEntries {
			if entry.IsDir() {
				continue
			}
			rel := filepath.ToSlash(filepath.Join("artifacts", "sha256", entry.Name()))
			if !usedPaths[rel] {
				report.OrphanArtifacts = append(report.OrphanArtifacts, rel)
			}
		}
		sort.Strings(report.OrphanArtifacts)
	}
	return out, report, nil
}
