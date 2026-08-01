// SPDX-License-Identifier: AGPL-3.0-only

package admissioncomposition

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
)

func deriveScope(base, final []runnercomposition.CandidateManifestEntry) (admission.ChangeScope, []UnsupportedOperation, error) {
	baseCanonical, err := runnercomposition.CanonicalizeManifest(base)
	if err != nil {
		return admission.ChangeScope{}, nil, err
	}
	finalCanonical, err := runnercomposition.CanonicalizeManifest(final)
	if err != nil {
		return admission.ChangeScope{}, nil, err
	}
	baseByPath := make(map[string]runnercomposition.CandidateManifestEntry, len(baseCanonical))
	finalByPath := make(map[string]runnercomposition.CandidateManifestEntry, len(finalCanonical))
	paths := map[string]bool{}
	for _, entry := range baseCanonical {
		baseByPath[entry.Path] = entry
		paths[entry.Path] = true
	}
	for _, entry := range finalCanonical {
		finalByPath[entry.Path] = entry
		paths[entry.Path] = true
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	scope := admission.ChangeScope{
		Files:           []admission.FileOperation{},
		Symbols:         []string{},
		Components:      []string{},
		ClaimIDs:        []string{},
		PropositionKeys: []string{},
	}
	unsupported := []UnsupportedOperation{}
	for _, path := range ordered {
		before, hadBefore := baseByPath[path]
		after, hasAfter := finalByPath[path]
		switch {
		case hadBefore && !hasAfter:
			unsupported = append(unsupported, UnsupportedOperation{Path: path, Operation: admission.ChangeDeleted, Detail: "existing admission supports read/modify only"})
		case !hadBefore && hasAfter:
			unsupported = append(unsupported, UnsupportedOperation{Path: path, Operation: admission.ChangeAdded, Detail: "existing admission supports read/modify only"})
		case before.Mode != after.Mode:
			unsupported = append(unsupported, UnsupportedOperation{Path: path, Operation: admission.ChangeTypeChanged, Detail: "candidate changed the governed file mode/type"})
		case before.Mode == runnercomposition.ModeSymlink && (before.ContentDigestSHA256 != after.ContentDigestSHA256 || before.SymlinkTarget != after.SymlinkTarget):
			unsupported = append(unsupported, UnsupportedOperation{Path: path, Operation: "symlink_changed", Detail: "symlink mutation is outside the current admission operation vocabulary"})
		case before.ContentDigestSHA256 != after.ContentDigestSHA256 || !bytes.Equal(before.Content, after.Content):
			scope.Files = append(scope.Files, admission.FileOperation{Path: path, Operation: admission.OperationModify})
		}
	}
	return scope, unsupported, nil
}

func admissionRequestIdentityDigest(req admission.Request) (string, error) {
	req.RequestedBy = ""
	req.Note = ""
	data, err := admission.MarshalCanonicalRequestYAML(req)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func validateDecision(in admission.Decision) (admission.Decision, error) {
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
	if out.DecisionDigestSHA256 != in.DecisionDigestSHA256 {
		return admission.Decision{}, errors.New("admissioncomposition: admission decision digest invalid")
	}
	if out.CorrectnessCertified || !out.ScopeOnly {
		return admission.Decision{}, errors.New("admissioncomposition: admission decision exceeds scope-only authority")
	}
	return out, nil
}

func validateVerification(in admission.Verification) (admission.Verification, error) {
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
	if out.VerificationDigestSHA256 != in.VerificationDigestSHA256 {
		return admission.Verification{}, errors.New("admissioncomposition: admission verification digest invalid")
	}
	if out.CorrectnessCertified || !out.ScopeOnly {
		return admission.Verification{}, errors.New("admissioncomposition: admission verification exceeds scope-only authority")
	}
	return out, nil
}

func unsupportedDetail(ops []UnsupportedOperation) string {
	if len(ops) == 0 {
		return "candidate produced no supported modify operation"
	}
	parts := make([]string, 0, len(ops))
	for _, op := range ops {
		parts = append(parts, op.Operation+":"+op.Path)
	}
	return "unsupported candidate operations: " + strings.Join(parts, ",")
}
