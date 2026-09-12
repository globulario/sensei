// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"gopkg.in/yaml.v3"
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

	// READ THE LOWER BOUNDS FIRST. Both comparisons below ask whether the entries
	// fall SHORT of another record, and both are read from a live directory that a
	// concurrent appender may be extending.
	//
	// Reading HEAD or the witness AFTER listing the entries lets an append land in
	// between, so the reader compares a stale entry list against a fresh claim and
	// reports damage where there was only concurrency -- which is precisely the
	// confusion this whole repair exists to remove, committed by the repair itself.
	//
	// Read first, and a concurrent append can only ADD entries afterwards: the
	// comparison can then err toward "complete", never toward "lost". A real
	// truncation is persistent and is still caught, here or on the next read; a
	// concurrent append is transient and must never be reported as evidence loss.
	priorHead, headErr := readHead(s.headPath())
	witness, haveWitness, werr := readWitness(taskDir)

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
				verifyReferencedArtifacts(ctx, taskDir, data, &report, usedPaths)
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
	// DISAGREEMENT HAS DIRECTION, and the two directions mean opposite things.
	//
	// Observed history may legitimately be NEWER than its projection: append.go
	// commits the entry first and writes HEAD second, so a crash in between leaves
	// HEAD one behind, and that is the recoverable ErrEntryDurable condition.
	//
	// A projection claiming history that no longer exists is the opposite fact:
	// evidence was lost. Both used to be one warning, which is what let a
	// tail-truncated chain verify as valid.
	head, err := priorHead, headErr
	if err == nil {
		switch {
		case head.Sequence > out.Head.Sequence:
			report.Errors = append(report.Errors, VerificationError{
				Code:   "ledger.head_leads_entries",
				Detail: fmt.Sprintf("HEAD records sequence %d but only %d entries exist: history was lost", head.Sequence, out.Head.Sequence),
				Path:   filepath.ToSlash(s.headPath()),
			})
		case head.EntryDigestSHA256 != out.Head.EntryDigestSHA256 || head.Sequence != out.Head.Sequence || head.EntryPath != out.Head.EntryPath:
			report.Warnings = append(report.Warnings, VerificationWarning{Code: "ledger.head_stale", Detail: "HEAD does not match verified last entry", Path: filepath.ToSlash(s.headPath())})
		}
	} else if !os.IsNotExist(err) {
		report.Errors = append(report.Errors, VerificationError{Code: "ledger.head_unreadable", Detail: err.Error(), Path: filepath.ToSlash(s.headPath())})
	}

	// COMPLETENESS, which the chain cannot establish about itself. The witness is
	// monotonic and lives outside the task directory, so destroying the ledger --
	// or the whole task -- does not destroy the record of how far it got.
	switch {
	case werr != nil:
		// Present but unreadable is never "no witness".
		report.Errors = append(report.Errors, VerificationError{Code: "ledger.witness_unreadable", Detail: werr.Error(), Path: filepath.ToSlash(witnessPath(taskDir))})
	case haveWitness && out.Head.Sequence < witness.HighestSequence:
		report.Errors = append(report.Errors, VerificationError{
			Code:   "ledger.history_truncated",
			Detail: fmt.Sprintf("this task reached sequence %d; the chain now holds %d", witness.HighestSequence, out.Head.Sequence),
			Path:   filepath.ToSlash(witnessPath(taskDir)),
		})
	case !haveWitness && len(out.Entries) > 0:
		// A chain with no witness predates this repair. Its consistency is
		// verified; its COMPLETENESS is not established, and a reader that is
		// about to interpret an absence must be able to tell the difference.
		report.Warnings = append(report.Warnings, VerificationWarning{Code: "ledger.completeness_unwitnessed", Detail: "no history witness exists for this task; absence of an event is not evidence it never occurred", Path: filepath.ToSlash(witnessPath(taskDir))})
	}
	report.CompletenessEstablished = haveWitness && werr == nil && witness.CoversFromGenesis()

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

// verifyReferencedArtifacts brings the artifacts a verified entry REFERENCES
// inside the integrity boundary.
//
// Only entry.Payload.Path was digest-checked, so the governed records themselves
// -- the admission decision, the capability consumption, the scope verification --
// lived outside verification. Rewriting one changed what the system believed
// while the chain still reported valid, and deleting one made the record
// unreadable, which a caller keyed on `err == nil` reads as "no such record".
//
// These refs also account for their own paths now: previously usedPaths held only
// entry payload paths, so every live governed record was reported as an ORPHAN,
// and a real orphan was indistinguishable from a record in use.
func verifyReferencedArtifacts(ctx context.Context, taskDir string, payloadData []byte, report *VerificationReport, usedPaths map[string]bool) {
	var tp TaskEventPayload
	if err := yaml.Unmarshal(payloadData, &tp); err != nil || len(tp.Artifacts) == 0 {
		return
	}
	keys := make([]string, 0, len(tp.Artifacts))
	for k := range tp.Artifacts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		ref := tp.Artifacts[key]
		rel := filepath.ToSlash(strings.TrimSpace(ref.Path))
		if rel == "" {
			report.Errors = append(report.Errors, VerificationError{Code: "ledger.artifact_ref_incomplete", Detail: "referenced artifact " + key + " has no path"})
			continue
		}
		if !isLocalArtifactPath(rel) {
			report.Errors = append(report.Errors, VerificationError{Code: "ledger.artifact_path_escapes", Detail: "referenced artifact " + key + " leaves the task directory", Path: rel})
			continue
		}
		usedPaths[rel] = true
		full := filepath.Join(taskDir, filepath.FromSlash(rel))
		info, err := os.Lstat(full)
		if err != nil {
			report.Errors = append(report.Errors, VerificationError{Code: "ledger.artifact_missing", Detail: err.Error(), Path: rel})
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			report.Errors = append(report.Errors, VerificationError{Code: "ledger.artifact_symlink", Detail: "referenced artifact must not be a symlink", Path: rel})
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			report.Errors = append(report.Errors, VerificationError{Code: "ledger.artifact_unreadable", Detail: err.Error(), Path: rel})
			continue
		}
		if !artifactDigestMatches(ctx, ref, data) {
			report.Errors = append(report.Errors, VerificationError{Code: "ledger.artifact_digest_mismatch", Detail: "referenced artifact " + key + " does not match its recorded digest", Path: rel})
		}
	}
}

// artifactDigestMatches accepts EITHER digest kind, because LedgerPayloadRef
// carries both without a discriminator: renderPayload digests a []byte payload by
// its bytes and a structured payload semantically, and the field records whichever
// the producer used. Accepting either is not a weakness against tampering -- both
// are collision-resistant, and altered content matches neither -- but it does mean
// the ref's KIND is still unstated. That ambiguity is a separate defect in
// LedgerPayloadRef's own contract and is deliberately not resolved here.
func artifactDigestMatches(ctx context.Context, ref closureprotocol.LedgerPayloadRef, data []byte) bool {
	want := strings.TrimSpace(ref.DigestSHA256)
	if want == "" {
		return false
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) == want {
		return true
	}
	if semantic, err := semanticDigestForBytesCtx(ctx, ref.MediaType, data); err == nil && semantic == want {
		return true
	}
	return false
}

// isLocalArtifactPath keeps a referenced artifact inside the task it belongs to.
// Narrow on purpose: this is the containment an integrity check needs in order to
// be checking the right file, not the general path-containment repair.
func isLocalArtifactPath(rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "/") || filepath.IsAbs(filepath.FromSlash(rel)) {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(rel))
	return clean != ".." && !strings.HasPrefix(clean, "../") && !strings.Contains(clean, "/../")
}

// STRUCTURAL vs REFERENTIAL integrity.
//
// A structural failure means the chain is lying about its own shape -- a broken
// digest link, a sequence gap, a lost tail. Nothing in it can be trusted and it
// must not be loaded.
//
// A referential failure means a record the chain POINTS TO is missing or altered.
// The chain's shape is sound; one referenced artifact is untrustworthy.
//
// The difference matters because domain layers diagnose referential damage BETTER
// than the ledger can. completion, for example, reports a completed event whose
// receipt artifact is gone as event_without_valid_receipt, which recovery handles
// as broken_completion. Failing the whole chain load replaced that precise refusal
// with "unsupported" -- a worse refusal, not a safer one.
//
// Both still set Valid=false. Referential damage never grants anything; it only
// stays DIAGNOSABLE.
func isReferentialCode(code string) bool {
	switch code {
	case "ledger.artifact_ref_incomplete", "ledger.artifact_path_escapes",
		"ledger.artifact_missing", "ledger.artifact_symlink",
		"ledger.artifact_unreadable", "ledger.artifact_digest_mismatch":
		return true
	}
	return false
}

func hasStructuralError(report VerificationReport) bool {
	for _, e := range report.Errors {
		if !isReferentialCode(e.Code) {
			return true
		}
	}
	return false
}

// VerifyChainForDiagnosisCtx returns the chain when only REFERENTIAL integrity
// failed, together with the report, so a caller can report a precise state
// instead of "unavailable". It never returns a structurally invalid chain, and
// the report it returns is still Valid=false -- a caller that needs authority
// must consult that, exactly as before.
//
// Deliberately a separate entry point. Widening VerifyChain itself would hand
// every existing caller a chain it currently refuses, which is how a repair for
// failing open fails open.
func (s *Store) VerifyChainForDiagnosisCtx(ctx context.Context) (VerifiedChain, VerificationReport, error) {
	chain, report, err := verifyAndLoadChain(ctx, s.taskDir, s.payloadValidator)
	if err != nil {
		return VerifiedChain{}, VerificationReport{}, err
	}
	report.EntryCount = len(chain.Entries)
	report.Valid = len(report.Errors) == 0
	if len(chain.Entries) > 0 {
		report.HeadDigestSHA256 = chain.Head.EntryDigestSHA256
		report.TaskID = chain.TaskID
	}
	if hasStructuralError(report) {
		return VerifiedChain{}, report, fmt.Errorf("invalid ledger chain")
	}
	if chain.TaskDir != "" && len(chain.Entries) > 0 && !hasStructuralError(report) {
		if set, perr := Project(chain); perr == nil {
			report.ProjectionState = ProjectionState(chain.TaskDir, set)
		}
	}
	return chain, report, nil
}

// VerifyChainForDiagnosis is VerifyChainForDiagnosisCtx with a background context.
func (s *Store) VerifyChainForDiagnosis() (VerifiedChain, VerificationReport, error) {
	return s.VerifyChainForDiagnosisCtx(context.Background())
}
