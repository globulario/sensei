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
	chain, report, head, err := verifyAndLoadChain(ctx, taskDir, validator)
	if err != nil {
		return VerificationReport{}, err
	}
	if len(chain.Entries) > 0 {
		report.TaskID = chain.TaskID
	}
	// The report says what HEAD PUBLISHED, never what it ought to publish. Naming
	// the derived digest here let a stale pointer read as current and made generic
	// verification a silent recovery authority.
	report.HeadDigestSHA256 = head.digest
	report.Errors = append(report.Errors, head.reportErrors...)
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
	// The head observation is deliberately discarded: HEAD is DERIVED state, and a
	// stale pointer does not make the entry chain unreadable. Refusing here would
	// leave ReconcileDerivedState unable to repair HEAD from the very chain that
	// proves what HEAD should be -- the condition would be permanent.
	chain, report, _, err := verifyAndLoadChain(ctx, taskDir, validator)
	if err != nil {
		return VerifiedChain{}, err
	}
	if len(report.Errors) > 0 {
		return VerifiedChain{}, fmt.Errorf("invalid ledger chain")
	}
	return chain, nil
}

// headObservation is what the published HEAD pointer actually says.
//
// It separates the two ways a pointer can be wrong, because they have different
// authorities. A readable-but-disagreeing pointer, or an absent one, is DERIVED
// state that the entries themselves prove how to repair. Corruption is not: a
// pointer that cannot be parsed is not a ledger a caller may load.
type headObservation struct {
	digest string
	// reportErrors are derived-state defects: stale or missing. They invalidate a
	// verification REPORT -- no caller may act on a pointer the ledger cannot vouch
	// for -- but leave the chain loadable, so derived-state repair can still read
	// the chain that proves what HEAD should be.
	reportErrors []VerificationError
	// chainErrors are corruption. They travel with the entry-chain findings and
	// make the chain itself unloadable.
	chainErrors []VerificationError
}

func verifyAndLoadChain(ctx context.Context, taskDir string, validator PayloadValidator) (VerifiedChain, VerificationReport, headObservation, error) {
	s := NewStore(taskDir, WithPayloadValidator(validator))
	files, err := listLedgerEntryFiles(s.ledgerDir())
	if err != nil {
		return VerifiedChain{}, VerificationReport{}, headObservation{}, err
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
	observed := observePublishedHead(s.headPath(), out)
	report.Errors = append(report.Errors, observed.chainErrors...)

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
	return out, report, observed, nil
}

// observePublishedHead reads the published HEAD pointer and compares it against the
// head derived from the verified entries. It REPORTS; it never repairs.
//
// All three non-matching outcomes fail closed, because a pointer that disagrees
// with the entries is the residue of an interrupted publication, and a caller that
// proceeds on it acts on a task state no entry supports.
//
// Recovery belongs to the store that owns the ledger -- ReconcileDerivedState
// rebuilds the pointer from the verified chain under the append lock -- and never
// to verification. A verifier that silently answered "what HEAD should say" while
// reporting it as "what HEAD says" left no way to tell a published pointer from a
// derived one.
func observePublishedHead(path string, chain VerifiedChain) headObservation {
	derived := chain.Head
	head, err := readHead(path)
	switch {
	case err == nil:
		obs := headObservation{digest: head.EntryDigestSHA256}
		if head.EntryDigestSHA256 != derived.EntryDigestSHA256 || head.Sequence != derived.Sequence || head.EntryPath != derived.EntryPath {
			obs.reportErrors = append(obs.reportErrors, VerificationError{
				Code:   "ledger.head_stale",
				Detail: "HEAD does not match verified last entry",
				Path:   filepath.ToSlash(path),
			})
		}
		return obs
	case os.IsNotExist(err):
		// No entries means nothing to publish, so an absent HEAD is correct.
		if len(chain.Entries) == 0 {
			return headObservation{}
		}
		return headObservation{reportErrors: []VerificationError{{
			Code:   "ledger.head_missing",
			Detail: "entries are present but HEAD is not published",
			Path:   filepath.ToSlash(path),
		}}}
	default:
		return headObservation{chainErrors: []VerificationError{{
			Code:   "ledger.head_unreadable",
			Detail: err.Error(),
			Path:   filepath.ToSlash(path),
		}}}
	}
}
