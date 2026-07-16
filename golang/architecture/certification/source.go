// SPDX-License-Identifier: AGPL-3.0-only

package certification

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/proofdischarge"
	"gopkg.in/yaml.v3"
)

// RequestFileName is the default location of the typed certification request
// inside a task directory.
const RequestFileName = "certification-request.yaml"

type requestEnvelope struct {
	CertificationRequest Request `json:"certification_request" yaml:"certification_request"`
}

// LoadRequest reads a typed certification request from disk. The file is a
// locator only: every record the request references is independently resolved
// and digest-verified; nothing in this file can assert an outcome.
func LoadRequest(path string) (Request, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Request{}, err
	}
	var env requestEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return Request{}, err
	}
	if env.CertificationRequest.TaskID == "" {
		return Request{}, fmt.Errorf("%w: %s has no certification_request", ErrRequestInvalid, path)
	}
	return env.CertificationRequest, nil
}

// DirSource resolves records from a task directory's content-addressed
// artifact store (artifacts/sha256/<digest>.json|.yaml — the same layout the
// task ledger uses for payload artifacts). The digest is the record's
// protocol digest; the loaded record must reproduce it, so the file path is a
// locator, never authority.
type DirSource struct {
	Dir string
}

func (s DirSource) read(digest string) ([]byte, error) {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return nil, fmt.Errorf("%w: empty digest reference", ErrRecordMissing)
	}
	base := filepath.Join(s.Dir, "artifacts", "sha256", digest)
	for _, ext := range []string{".json", ".yaml"} {
		data, err := os.ReadFile(base + ext)
		if err == nil {
			return data, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%w: no artifact for digest %s", ErrRecordMissing, digest)
}

// resolveInto decodes the artifact for digest into out and verifies that the
// decoded record reproduces the digest via recompute.
func (s DirSource) resolveInto(digest string, out any, recompute func() (string, error)) error {
	data, err := s.read(digest)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		return err
	}
	actual, err := recompute()
	if err != nil {
		return err
	}
	if actual != strings.TrimSpace(digest) {
		return fmt.Errorf("%w: artifact %s decodes to digest %s", ErrRecordDigestMismatch, digest, actual)
	}
	return nil
}

// ResolveRecords loads every record the request references from the source.
// Absent optional references stay zero; the lanes fail closed on them.
func ResolveRecords(src DirSource, req Request) (Records, error) {
	var rec Records

	if d := strings.TrimSpace(req.AdmissionRequestDigestSHA256); d != "" {
		if err := src.resolveInto(d, &rec.AdmissionRequest, func() (string, error) {
			return admissionRequestDigest(rec.AdmissionRequest)
		}); err != nil {
			return Records{}, err
		}
	}
	if d := strings.TrimSpace(req.AdmissionDecisionDigestSHA256); d != "" {
		if err := src.resolveInto(d, &rec.AdmissionDecision, func() (string, error) {
			return admissionDecisionDigest(rec.AdmissionDecision)
		}); err != nil {
			return Records{}, err
		}
	}
	if d := strings.TrimSpace(req.CapabilityConsumptionDigestSHA256); d != "" {
		if err := src.resolveInto(d, &rec.CapabilityConsumption, func() (string, error) {
			return capabilityConsumptionDigest(rec.CapabilityConsumption)
		}); err != nil {
			return Records{}, err
		}
	}
	if d := strings.TrimSpace(req.ScopeVerificationDigestSHA256); d != "" {
		if err := src.resolveInto(d, &rec.ScopeVerification, func() (string, error) {
			return scopeVerificationDigest(rec.ScopeVerification)
		}); err != nil {
			return Records{}, err
		}
	}
	if d := strings.TrimSpace(req.RuntimeTargetDigestSHA256); d != "" {
		var target closureprotocol.RuntimeTarget
		if err := src.resolveInto(d, &target, func() (string, error) {
			return closureprotocol.SemanticDigest(target)
		}); err != nil {
			return Records{}, err
		}
		rec.RuntimeTarget = &target
	}

	for _, d := range closureprotocol.NormalizeSet(req.AuthorityResolutionDigests) {
		var record closureprotocol.AuthorityResolution
		if err := src.resolveInto(d, &record, func() (string, error) {
			return closureprotocol.AuthorityResolutionDigest(record)
		}); err != nil {
			return Records{}, err
		}
		rec.AuthorityResolutions = append(rec.AuthorityResolutions, record)
	}
	for _, d := range closureprotocol.NormalizeSet(req.ProofDischargeDigests) {
		var record closureprotocol.ProofDischarge
		if err := src.resolveInto(d, &record, func() (string, error) {
			return closureprotocol.ProofDischargeDigest(record)
		}); err != nil {
			return Records{}, err
		}
		rec.ProofDischarges = append(rec.ProofDischarges, record)
	}
	for _, d := range closureprotocol.NormalizeSet(req.ProofObligationDigests) {
		var record proofdischarge.ProofObligation
		if err := src.resolveInto(d, &record, func() (string, error) {
			return closureprotocol.SemanticDigest(record)
		}); err != nil {
			return Records{}, err
		}
		rec.Obligations = append(rec.Obligations, record)
	}
	for _, d := range closureprotocol.NormalizeSet(req.EvidenceProfileDigests) {
		var record closureprotocol.EvidenceProfile
		if err := src.resolveInto(d, &record, func() (string, error) {
			return closureprotocol.SemanticDigest(record)
		}); err != nil {
			return Records{}, err
		}
		rec.EvidenceProfiles = append(rec.EvidenceProfiles, record)
	}
	for _, d := range closureprotocol.NormalizeSet(req.EvidenceReceiptDigests) {
		var record closureprotocol.EvidenceReceipt
		if err := src.resolveInto(d, &record, func() (string, error) {
			return closureprotocol.SemanticDigest(record)
		}); err != nil {
			return Records{}, err
		}
		rec.EvidenceReceipts = append(rec.EvidenceReceipts, record)
	}
	for _, d := range closureprotocol.NormalizeSet(req.ArtifactReceiptDigests) {
		var record ArtifactReceipt
		if err := src.resolveInto(d, &record, func() (string, error) {
			return closureprotocol.SemanticDigest(record)
		}); err != nil {
			return Records{}, err
		}
		rec.ArtifactReceipts = append(rec.ArtifactReceipts, record)
	}
	for _, d := range closureprotocol.NormalizeSet(req.WaiverDigests) {
		var record closureprotocol.WaiverReceipt
		if err := src.resolveInto(d, &record, func() (string, error) {
			return waiverReceiptDigest(record)
		}); err != nil {
			return Records{}, err
		}
		rec.Waivers = append(rec.Waivers, record)
	}
	for _, d := range closureprotocol.NormalizeSet(req.RevocationDigests) {
		var record closureprotocol.RevocationReceipt
		if err := src.resolveInto(d, &record, func() (string, error) {
			return closureprotocol.SemanticDigest(record)
		}); err != nil {
			return Records{}, err
		}
		rec.Revocations = append(rec.Revocations, record)
	}
	for _, d := range closureprotocol.NormalizeSet(req.ForbiddenMoveFindingDigests) {
		var record ForbiddenMoveFinding
		if err := src.resolveInto(d, &record, func() (string, error) {
			return closureprotocol.SemanticDigest(record)
		}); err != nil {
			return Records{}, err
		}
		rec.ForbiddenMoveFindings = append(rec.ForbiddenMoveFindings, record)
	}
	return rec, nil
}

// WriteRecordArtifact stores a typed record content-addressed under
// <dir>/artifacts/sha256/<digest>.json using the record's protocol digest and
// canonical JSON bytes, and returns the digest. It is how earlier phases (and
// tests/fixtures) publish records for certification to resolve.
func WriteRecordArtifact(dir string, record any) (string, error) {
	digest, err := recordDigest(record)
	if err != nil {
		return "", err
	}
	data, err := closureprotocol.CanonicalJSON(record)
	if err != nil {
		return "", err
	}
	target := filepath.Join(dir, "artifacts", "sha256", digest+".json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if existing, err := os.ReadFile(target); err == nil {
		if string(existing) != string(data) {
			return "", fmt.Errorf("artifact digest collision for %s", digest)
		}
		return digest, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return "", err
	}
	return digest, nil
}

// recordDigest computes the protocol digest for a record: self-digest fields
// are excluded via the frozen helpers; everything else is the semantic digest.
func recordDigest(record any) (string, error) {
	switch v := record.(type) {
	case closureprotocol.AuthorityResolution:
		return closureprotocol.AuthorityResolutionDigest(v)
	case closureprotocol.ProofDischarge:
		return closureprotocol.ProofDischargeDigest(v)
	case closureprotocol.WaiverReceipt:
		return waiverReceiptDigest(v)
	case closureprotocol.CertificationReceipt:
		return closureprotocol.CertificationReceiptDigest(v)
	default:
		return closureprotocol.SemanticDigest(record)
	}
}

// CanonicalReceiptBytes renders a receipt as canonical JSON for
// content-addressed persistence.
func CanonicalReceiptBytes(receipt closureprotocol.CertificationReceipt) ([]byte, error) {
	return closureprotocol.CanonicalJSON(receipt)
}

// CanonicalRequestYAML renders a request envelope as canonical YAML for
// authoring by earlier phases.
func CanonicalRequestYAML(req Request) ([]byte, error) {
	return binding.CanonicalYAML(requestEnvelope{CertificationRequest: req})
}
