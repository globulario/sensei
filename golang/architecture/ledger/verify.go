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
	head, err := readHead(s.headPath())
	if err == nil {
		if head.EntryDigestSHA256 != out.Head.EntryDigestSHA256 || head.Sequence != out.Head.Sequence || head.EntryPath != out.Head.EntryPath {
			report.Warnings = append(report.Warnings, VerificationWarning{Code: "ledger.head_stale", Detail: "HEAD does not match verified last entry", Path: filepath.ToSlash(s.headPath())})
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
