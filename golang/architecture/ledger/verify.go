// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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

// headEntryStillOnDisk reports whether the entry HEAD names is present.
//
// It is the seam that gives a HEAD/chain disagreement its direction. An empty EntryPath is
// read as PRESENT: a HEAD that records no path makes no claim about a specific file, and a
// corrupted HEAD file is recoverable from the chain rather than evidence against it.
func headEntryStillOnDisk(taskDir string, head Head) bool {
	rel := strings.TrimSpace(head.EntryPath)
	if rel == "" {
		return true
	}
	_, err := os.Stat(filepath.Join(taskDir, filepath.FromSlash(rel)))
	return err == nil
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
		// The task id is taken from the first entry that could be READ, not from files[0]:
		// when the first file is corrupt it is skipped, and comparing every later entry
		// against an empty id reported "task id changes within one chain" for a chain in
		// which nothing changed.
		if out.TaskID == "" {
			out.TaskID = entry.Task.ID
		}
		if idx == 0 {
			if entry.PreviousEntryDigestSHA256 != "" {
				report.Errors = append(report.Errors, VerificationError{Code: "ledger.first_entry_previous_digest", Detail: "first entry must not carry previous digest", Path: filepath.ToSlash(path)})
			}
		} else if n := len(out.Entries); n > 0 {
			// out.Entries, NOT files: an entry that failed to read or validate was recorded
			// as an error and NOT appended, so after any skip the two slices diverge and
			// out.Entries[idx-1] indexes past the end. Every admission loader calls
			// VerifyChain and appendEntry verifies before every append, so that read took a
			// task's whole governance path down by panic instead of returning the typed
			// refusal the design calls for -- and a fail-closed system that panics has no
			// verdict to fail closed with.
			prev := out.Entries[n-1].Entry
			if entry.PreviousEntryDigestSHA256 != prev.EntryDigestSHA256 {
				report.Errors = append(report.Errors, VerificationError{Code: "ledger.previous_digest_mismatch", Detail: "previous digest does not match prior entry", Path: filepath.ToSlash(path)})
			}
			if entry.Task.ID != out.TaskID {
				report.Errors = append(report.Errors, VerificationError{Code: "ledger.task_id_changed", Detail: "task id changes within one chain", Path: filepath.ToSlash(path)})
			}
		}
		// n == 0 with idx > 0 means every preceding entry was unreadable. The link to a
		// predecessor that could not be read is not checkable, and NOT checking it asserts
		// nothing: the unreadable entry is already a recorded error, so the chain is invalid
		// either way, and inventing a second failure here would misname which entry is at
		// fault.
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
	head, err := readHead(s.headPath())
	if err == nil {
		if head.EntryDigestSHA256 != out.Head.EntryDigestSHA256 || head.Sequence != out.Head.Sequence || head.EntryPath != out.Head.EntryPath {
			// DISAGREEMENT BETWEEN HEAD AND THE CHAIN HAS A DIRECTION, and the two directions
			// are different facts with different remedies.
			//
			// The discriminator is whether THE ENTRY HEAD NAMES IS STILL ON DISK -- HEAD's own
			// claim, tested directly. Neither digest membership nor a length comparison works:
			//
			//   - membership: a corrupted HEAD also names a digest the chain lacks, so requiring
			//     membership turns every damaged HEAD into an unrecoverable task (caught by
			//     resultrecording's TestStaleHeadRecovery);
			//   - length: a reader that does not hold the append lock can list the directory
			//     BEFORE an entry lands and read HEAD AFTER it updates, so head.Sequence briefly
			//     exceeds the entries it saw. That torn read is transient and the entry is on
			//     disk the whole time -- counting cannot tell it from a deletion, and eight
			//     concurrent writers reproduce it.
			//
			// PRESENT: the entry HEAD names exists. Whatever the disagreement -- an interrupted
			// append, a corrupted HEAD, a listing taken a moment too early -- the chain holds
			// what HEAD attests to, so the chain is the authority and HEAD is rebuilt from it.
			// This stays a warning, or every crashed append becomes an unrecoverable task.
			//
			// GONE: HEAD names an entry that is not there. Nothing but a deletion produces that.
			// A truncated chain is a valid PREFIX -- every sequence link and previous-digest
			// check is satisfied -- so HEAD is the ONLY witness that the chain was ever longer,
			// and recording that witness as a warning made it invisible:
			// report.Valid is len(report.Errors) == 0, and no caller in the governance path
			// inspects warnings. One deletion then let a reader see genuine ABSENCE of an event
			// and reconstruct authority that event had already spent.
			//
			// Integrity checks verify LINKS, not LENGTH. This is the length check.
			if headEntryStillOnDisk(taskDir, head) {
				report.Warnings = append(report.Warnings, VerificationWarning{Code: "ledger.head_stale", Detail: "HEAD does not match verified last entry", Path: filepath.ToSlash(s.headPath())})
			} else {
				report.Errors = append(report.Errors, VerificationError{
					Code:   "ledger.head_not_in_chain",
					Detail: fmt.Sprintf("HEAD attests to entry %s at sequence %d, which is no longer on disk: history is incomplete, not merely stale", head.EntryDigestSHA256, head.Sequence),
					Path:   filepath.ToSlash(s.headPath()),
				})
			}
		}
	} else if !os.IsNotExist(err) {
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
