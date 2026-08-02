// SPDX-License-Identifier: AGPL-3.0-only

package candidateapply

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/admissioncomposition"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
)

type stagedReplacement struct {
	path        string
	destination string
	temporary   string
	backup      string
	before      runnercomposition.CandidateManifestEntry
	after       runnercomposition.CandidateManifestEntry
	applied     bool
}

func ComposeRequest(in ApplyInput) (Request, error) {
	decision, err := validateInput(in)
	if err != nil {
		return Request{}, err
	}
	paths := make([]string, 0, len(in.AdmissionRequest.DerivedScope.Files))
	for _, op := range in.AdmissionRequest.DerivedScope.Files {
		paths = append(paths, op.Path)
	}
	sort.Strings(paths)
	r := NormalizeRequest(Request{
		SchemaVersion:                           RequestSchemaVersion,
		RequestID:                               "o5b." + in.CandidateArtifact.CandidateArtifactDigestSHA256[:12] + "." + in.AdmissionReceipt.ReceiptDigestSHA256[:12],
		GeneratedBy:                             GeneratedBy,
		AdmissionCompositionRequestDigestSHA256: in.AdmissionRequest.RequestDigestSHA256,
		AdmissionCompositionReceiptDigestSHA256: in.AdmissionReceipt.ReceiptDigestSHA256,
		AdmissionDecisionDigestSHA256:           decision.DecisionDigestSHA256,
		CandidateArtifactDigestSHA256:           in.CandidateArtifact.CandidateArtifactDigestSHA256,
		RepositoryDomain:                        in.CandidateArtifact.RepositoryDomain,
		BaseRevision:                            in.CandidateArtifact.BaseRevision,
		InputCandidateDigestSHA256:              in.CandidateArtifact.InputCandidateDigestSHA256,
		FinalCandidateContentDigestSHA256:       in.CandidateArtifact.FinalCandidateContentDigestSHA256,
		ProposedChangeDigestSHA256:              in.CandidateArtifact.ProposedChangeDigestSHA256,
		ModifyPaths:                             paths,
	})
	digest, err := RequestDigest(r)
	if err != nil {
		return Request{}, err
	}
	r.RequestDigestSHA256 = digest
	return r, ValidateRequest(r)
}

func Apply(ctx context.Context, in ApplyInput, completedAt string) (Request, Receipt, error) {
	req, err := ComposeRequest(in)
	if err != nil {
		return Request{}, Receipt{}, err
	}
	root, err := validateTargetRoot(ctx, in.TargetRoot, req.BaseRevision)
	if err != nil {
		return req, Receipt{}, err
	}
	baseByPath, finalByPath, err := indexedManifests(in.BaseManifest, in.CandidateArtifact.Manifest)
	if err != nil {
		return req, Receipt{}, err
	}

	staged := make([]*stagedReplacement, 0, len(req.ModifyPaths))
	cleanup := func() {
		for _, item := range staged {
			_ = os.Remove(item.temporary)
			if item.backup != "" {
				_ = os.Remove(item.backup)
			}
		}
	}
	defer cleanup()

	for _, path := range req.ModifyPaths {
		before, okBefore := baseByPath[path]
		after, okAfter := finalByPath[path]
		if !okBefore || !okAfter {
			return req, Receipt{}, fmt.Errorf("candidateapply: modify path %q is missing from base or final manifest", path)
		}
		item, err := stageReplacement(root, before, after)
		if err != nil {
			return req, Receipt{}, err
		}
		staged = append(staged, item)
	}

	for i, item := range staged {
		if err := verifyFile(item.destination, item.before); err != nil {
			cause := fmt.Errorf("candidateapply: target changed during staging for %q: %w", item.path, err)
			return req, Receipt{}, withRollback(staged[:i], cause)
		}
		if err := os.Rename(item.destination, item.backup); err != nil {
			cause := fmt.Errorf("candidateapply: backup %q: %w", item.path, err)
			return req, Receipt{}, withRollback(staged[:i], cause)
		}
		item.applied = true
		if err := os.Rename(item.temporary, item.destination); err != nil {
			cause := fmt.Errorf("candidateapply: replace %q: %w", item.path, err)
			return req, Receipt{}, withRollback(staged[:i+1], cause)
		}
	}

	for _, item := range staged {
		if err := verifyFile(item.destination, item.after); err != nil {
			cause := fmt.Errorf("candidateapply: post-apply verification for %q: %w", item.path, err)
			return req, Receipt{}, withRollback(staged, cause)
		}
	}

	// Backup files are transaction-local evidence. Remove them before the
	// admission owner observes the worktree. Rollback remains possible from
	// the immutable base manifest if capture or scope verification fails.
	for _, item := range staged {
		if err := os.Remove(item.backup); err != nil {
			cause := fmt.Errorf("candidateapply: remove backup for %q: %w", item.path, err)
			return req, Receipt{}, withRollback(staged, cause)
		}
		item.backup = ""
	}

	changes, patchDigest, err := admission.CaptureChanges(root, req.BaseRevision)
	if err != nil {
		return req, Receipt{}, withRollback(staged, fmt.Errorf("candidateapply: capture changes: %w", err))
	}
	if err := verifyCapturedChanges(req.ModifyPaths, changes); err != nil {
		return req, Receipt{}, withRollback(staged, err)
	}
	for _, item := range staged {
		item.applied = false
	}

	receipt := NormalizeReceipt(Receipt{
		SchemaVersion:                           ReceiptSchemaVersion,
		ReceiptID:                               "o5b-receipt." + req.RequestDigestSHA256[:16],
		GeneratedBy:                             GeneratedBy,
		RequestDigestSHA256:                     req.RequestDigestSHA256,
		AdmissionCompositionReceiptDigestSHA256: req.AdmissionCompositionReceiptDigestSHA256,
		AdmissionDecisionDigestSHA256:           req.AdmissionDecisionDigestSHA256,
		CandidateArtifactDigestSHA256:           req.CandidateArtifactDigestSHA256,
		InputCandidateDigestSHA256:              req.InputCandidateDigestSHA256,
		FinalCandidateContentDigestSHA256:       req.FinalCandidateContentDigestSHA256,
		PatchDigestSHA256:                       patchDigest,
		AppliedPaths:                            append([]string{}, req.ModifyPaths...),
		Disposition:                             DispositionApplied,
		Detail:                                  "admitted sealed candidate applied to clean pinned worktree",
		CompletedAt:                             completedAt,
	})
	digest, err := ReceiptDigest(receipt)
	if err != nil {
		return req, Receipt{}, err
	}
	receipt.ReceiptDigestSHA256 = digest
	return req, receipt, ValidateReceipt(receipt)
}

func AttachVerification(receipt Receipt, decision admission.Decision, verification admission.Verification, completedAt string) (Receipt, error) {
	if err := ValidateReceipt(receipt); err != nil {
		return Receipt{}, err
	}
	if receipt.Disposition != DispositionApplied {
		return Receipt{}, errors.New("candidateapply: verification requires an applied receipt")
	}
	canonicalDecision, err := canonicalDecision(decision)
	if err != nil {
		return Receipt{}, err
	}
	if canonicalDecision.DecisionDigestSHA256 != receipt.AdmissionDecisionDigestSHA256 {
		return Receipt{}, errors.New("candidateapply: admission decision changed before verification")
	}
	canonicalVerification, err := canonicalVerification(verification)
	if err != nil {
		return Receipt{}, err
	}
	if canonicalVerification.AdmissionID != canonicalDecision.AdmissionID || canonicalVerification.DecisionDigestSHA256 != canonicalDecision.DecisionDigestSHA256 || canonicalVerification.PatchDigestSHA256 != receipt.PatchDigestSHA256 || !reflect.DeepEqual(canonicalVerification.Binding, canonicalDecision.Binding) {
		return Receipt{}, errors.New("candidateapply: verification is not bound to the applied candidate")
	}
	status := canonicalVerification.Status
	verificationDigest := canonicalVerification.VerificationDigestSHA256
	receipt.AdmissionVerificationStatus = &status
	receipt.AdmissionVerificationDigestSHA256 = &verificationDigest
	receipt.Disposition = DispositionVerificationRecorded
	receipt.CompletedAt = completedAt
	digest, err := ReceiptDigest(receipt)
	if err != nil {
		return Receipt{}, err
	}
	receipt.ReceiptDigestSHA256 = digest
	return receipt, ValidateReceipt(receipt)
}

func validateInput(in ApplyInput) (admission.Decision, error) {
	if err := admissioncomposition.ValidateRequest(in.AdmissionRequest); err != nil {
		return admission.Decision{}, fmt.Errorf("candidateapply: O5A request: %w", err)
	}
	if err := admissioncomposition.ValidateReceipt(in.AdmissionReceipt); err != nil {
		return admission.Decision{}, fmt.Errorf("candidateapply: O5A receipt: %w", err)
	}
	if !in.AdmissionRequest.AdmissionEligible || in.AdmissionReceipt.Disposition != admissioncomposition.DispositionAdmissionDecided || in.AdmissionReceipt.AdmissionDecisionDigestSHA256 == nil {
		return admission.Decision{}, errors.New("candidateapply: candidate has no admission decision authorizing application")
	}
	decision, err := canonicalDecision(in.Decision)
	if err != nil {
		return admission.Decision{}, err
	}
	if decision.Decision != admission.DecisionAdmitted && decision.Decision != admission.DecisionAdmittedWithConditions {
		return admission.Decision{}, fmt.Errorf("candidateapply: decision %q does not authorize mutation", decision.Decision)
	}
	if decision.MutationCapability != admission.CapabilityAdmitted && decision.MutationCapability != admission.CapabilityAdmittedWithConditions {
		return admission.Decision{}, errors.New("candidateapply: mutation capability is not admitted")
	}
	if decision.DecisionDigestSHA256 != *in.AdmissionReceipt.AdmissionDecisionDigestSHA256 || in.AdmissionReceipt.RequestDigestSHA256 != in.AdmissionRequest.RequestDigestSHA256 {
		return admission.Decision{}, errors.New("candidateapply: O5A request/receipt/decision lineage mismatch")
	}
	if err := runnercomposition.ValidateCandidateArtifact(in.CandidateArtifact); err != nil {
		return admission.Decision{}, fmt.Errorf("candidateapply: candidate artifact: %w", err)
	}
	if in.CandidateArtifact.CandidateArtifactDigestSHA256 != in.AdmissionRequest.CandidateArtifactDigestSHA256 || in.CandidateArtifact.CandidateArtifactDigestSHA256 != in.AdmissionReceipt.CandidateArtifactDigestSHA256 || in.CandidateArtifact.RepositoryDomain != in.AdmissionRequest.RepositoryDomain || in.CandidateArtifact.BaseRevision != in.AdmissionRequest.BaseRevision {
		return admission.Decision{}, errors.New("candidateapply: O5A/candidate lineage mismatch")
	}
	baseDigest, err := runnercomposition.ManifestDigest(in.BaseManifest)
	if err != nil {
		return admission.Decision{}, err
	}
	if baseDigest != in.CandidateArtifact.InputCandidateDigestSHA256 {
		return admission.Decision{}, errors.New("candidateapply: base manifest does not match candidate input")
	}
	if len(in.AdmissionRequest.UnsupportedOperations) != 0 || len(in.AdmissionRequest.DerivedScope.Files) == 0 {
		return admission.Decision{}, errors.New("candidateapply: application requires only supported modify operations")
	}
	return decision, nil
}

func validateTargetRoot(ctx context.Context, root, baseRevision string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(abs)
	info, err := os.Lstat(clean)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("candidateapply: target root must be a real directory")
	}
	head, err := gitOutput(ctx, clean, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("candidateapply: target is not a usable Git worktree: %w", err)
	}
	if strings.TrimSpace(head) != baseRevision {
		return "", fmt.Errorf("candidateapply: target HEAD %s does not match admitted base %s", strings.TrimSpace(head), baseRevision)
	}
	status, err := gitOutput(ctx, clean, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(status) != "" {
		return "", errors.New("candidateapply: target worktree is not clean")
	}
	return clean, nil
}

func indexedManifests(base, final []runnercomposition.CandidateManifestEntry) (map[string]runnercomposition.CandidateManifestEntry, map[string]runnercomposition.CandidateManifestEntry, error) {
	canonicalBase, err := runnercomposition.CanonicalizeManifest(base)
	if err != nil {
		return nil, nil, err
	}
	canonicalFinal, err := runnercomposition.CanonicalizeManifest(final)
	if err != nil {
		return nil, nil, err
	}
	baseByPath := make(map[string]runnercomposition.CandidateManifestEntry, len(canonicalBase))
	finalByPath := make(map[string]runnercomposition.CandidateManifestEntry, len(canonicalFinal))
	for _, entry := range canonicalBase {
		baseByPath[entry.Path] = entry
	}
	for _, entry := range canonicalFinal {
		finalByPath[entry.Path] = entry
	}
	return baseByPath, finalByPath, nil
}

func stageReplacement(root string, before, after runnercomposition.CandidateManifestEntry) (*stagedReplacement, error) {
	if before.Path != after.Path || before.Mode != after.Mode || (after.Mode != runnercomposition.ModeRegular && after.Mode != runnercomposition.ModeExecutable) {
		return nil, fmt.Errorf("candidateapply: %q is not a supported same-mode file modification", before.Path)
	}
	destination, err := secureDestination(root, before.Path)
	if err != nil {
		return nil, err
	}
	if err := verifyFile(destination, before); err != nil {
		return nil, fmt.Errorf("candidateapply: target base mismatch for %q: %w", before.Path, err)
	}
	dir := filepath.Dir(destination)
	stage, err := os.CreateTemp(dir, ".sensei-candidateapply-stage-*")
	if err != nil {
		return nil, err
	}
	stageName := stage.Name()
	removeStage := true
	defer func() {
		_ = stage.Close()
		if removeStage {
			_ = os.Remove(stageName)
		}
	}()
	if _, err := stage.Write(after.Content); err != nil {
		return nil, err
	}
	if err := stage.Sync(); err != nil {
		return nil, err
	}
	if err := stage.Close(); err != nil {
		return nil, err
	}
	if err := os.Chmod(stageName, fileMode(after.Mode)); err != nil {
		return nil, err
	}
	backupFile, err := os.CreateTemp(dir, ".sensei-candidateapply-backup-*")
	if err != nil {
		return nil, err
	}
	backupName := backupFile.Name()
	if err := backupFile.Close(); err != nil {
		return nil, err
	}
	if err := os.Remove(backupName); err != nil {
		return nil, err
	}
	removeStage = false
	return &stagedReplacement{path: before.Path, destination: destination, temporary: stageName, backup: backupName, before: before, after: after}, nil
}

func secureDestination(root, candidatePath string) (string, error) {
	if err := runnercomposition.ValidateCandidatePath(candidatePath); err != nil {
		return "", err
	}
	current := root
	parts := strings.Split(candidatePath, "/")
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("candidateapply: parent %q is not a real directory", current)
		}
	}
	return filepath.Join(root, filepath.FromSlash(candidatePath)), nil
}

func verifyFile(path string, expected runnercomposition.CandidateManifestEntry) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(content, expected.Content) || executable(info.Mode()) != (expected.Mode == runnercomposition.ModeExecutable) {
		return errors.New("content or executable mode mismatch")
	}
	return nil
}

func withRollback(items []*stagedReplacement, cause error) error {
	if err := rollback(items); err != nil {
		return fmt.Errorf("%w; rollback failed: %v", cause, err)
	}
	return cause
}

func rollback(items []*stagedReplacement) error {
	var failures []string
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if !item.applied {
			continue
		}
		_ = os.Remove(item.destination)
		if item.backup != "" {
			if err := os.Rename(item.backup, item.destination); err == nil {
				item.backup = ""
				item.applied = false
				continue
			}
		}
		if err := restoreManifestEntry(item.destination, item.before); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", item.path, err))
		}
		item.applied = false
	}
	if len(failures) != 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func restoreManifestEntry(destination string, entry runnercomposition.CandidateManifestEntry) error {
	dir := filepath.Dir(destination)
	tmp, err := os.CreateTemp(dir, ".sensei-candidateapply-restore-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(entry.Content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, fileMode(entry.Mode)); err != nil {
		return err
	}
	_ = os.Remove(destination)
	return os.Rename(tmpName, destination)
}

func verifyCapturedChanges(wantPaths []string, changes []admission.ChangeReceipt) error {
	want := append([]string{}, wantPaths...)
	sort.Strings(want)
	got := make([]string, 0, len(changes))
	for _, change := range changes {
		if change.ChangeType != admission.ChangeModified || (change.OldPath != "" && change.OldPath != change.Path) {
			return fmt.Errorf("candidateapply: observed unsupported change %s:%s", change.ChangeType, change.Path)
		}
		got = append(got, change.Path)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("candidateapply: observed changed paths %v do not match admitted paths %v", got, want)
	}
	return nil
}

func canonicalDecision(in admission.Decision) (admission.Decision, error) {
	data, err := admission.MarshalCanonicalDecisionJSON(in)
	if err != nil {
		return admission.Decision{}, err
	}
	var env struct {
		ArchitectureAdmissionDecision admission.Decision `json:"architecture_admission_decision"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return admission.Decision{}, err
	}
	out := env.ArchitectureAdmissionDecision
	if out.DecisionDigestSHA256 != in.DecisionDigestSHA256 || out.CorrectnessCertified || !out.ScopeOnly {
		return admission.Decision{}, errors.New("candidateapply: invalid admission decision evidence")
	}
	return out, nil
}

func canonicalVerification(in admission.Verification) (admission.Verification, error) {
	data, err := admission.MarshalCanonicalVerificationJSON(in)
	if err != nil {
		return admission.Verification{}, err
	}
	var env struct {
		ArchitectureAdmissionVerification admission.Verification `json:"architecture_admission_verification"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return admission.Verification{}, err
	}
	out := env.ArchitectureAdmissionVerification
	if out.VerificationDigestSHA256 != in.VerificationDigestSHA256 || out.CorrectnessCertified || !out.ScopeOnly {
		return admission.Verification{}, errors.New("candidateapply: invalid admission verification evidence")
	}
	return out, nil
}

func gitOutput(ctx context.Context, root string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	data, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(data)))
	}
	return string(data), nil
}

func fileMode(mode runnercomposition.CandidateFileMode) os.FileMode {
	if mode == runnercomposition.ModeExecutable {
		return 0o755
	}
	return 0o644
}

func executable(mode os.FileMode) bool { return mode&0o111 != 0 }
