// SPDX-License-Identifier: Apache-2.0

package questionresolution

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// certificatePath is the content-addressed location of a certificate, keyed by its
// self-excluding digest. Distinct from the promotion store and the task ledger.
func certificatePath(root, digest string) string {
	return filepath.Join(root, filepath.FromSlash(CertificationsRelDir), digest, "certificate.json")
}

// persistCertificate writes the certificate content-addressed with exists-check
// idempotency: if a byte-identical certificate already exists at the address, it
// reports a replay and writes nothing; a differing file at the same address is a
// digest collision (impossible for honest inputs) and is refused. Returns the
// repo-relative path and whether this was a replay.
func persistCertificate(root string, c QuestionResolutionCertificate) (relPath string, replay bool, err error) {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", false, err
	}
	abs := certificatePath(root, c.DigestSHA256)
	rel := filepath.ToSlash(filepath.Join(CertificationsRelDir, c.DigestSHA256, "certificate.json"))
	if existing, rerr := os.ReadFile(abs); rerr == nil {
		if !bytes.Equal(existing, data) {
			return rel, false, fmt.Errorf("certificate digest collision at %s", rel)
		}
		return rel, true, nil
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return rel, false, err
	}
	tmp := abs + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return rel, false, err
	}
	if err := os.Rename(tmp, abs); err != nil {
		_ = os.Remove(tmp)
		return rel, false, err
	}
	return rel, false, nil
}

// DiscoverCertificates lists the persisted question-resolution certificate digests
// present in the repository certificate store. Discovery is NON-AUTHORITATIVE: each
// digest must be loaded and validated (LoadCertificate) before it may be trusted.
func DiscoverCertificates(root string) ([]string, error) {
	base := filepath.Join(root, filepath.FromSlash(CertificationsRelDir))
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// CertificateClaim is the untrusted routing view of a persisted certificate: the
// task and ledger head it PURPORTS to bind. It carries no authority and is used
// only to decide relevance to a particular task world; a candidate must still pass
// LoadCertificate (full validation) before it may satisfy anything.
type CertificateClaim struct {
	Task                       closureprotocol.TaskBinding
	TaskLedgerHeadDigestSHA256 string
}

// ReadCertificateClaim reads a persisted certificate's claimed task and ledger head
// WITHOUT validation. It is untrusted routing metadata, never proof.
func ReadCertificateClaim(root, digest string) (CertificateClaim, error) {
	data, err := os.ReadFile(certificatePath(root, digest))
	if err != nil {
		return CertificateClaim{}, err
	}
	var c QuestionResolutionCertificate
	if err := json.Unmarshal(data, &c); err != nil {
		return CertificateClaim{}, err
	}
	return CertificateClaim{Task: c.Task, TaskLedgerHeadDigestSHA256: c.TaskLedgerHeadDigestSHA256}, nil
}

// LoadCertificate reads and validates a persisted certificate by its digest.
func LoadCertificate(root, digest string) (QuestionResolutionCertificate, error) {
	data, err := os.ReadFile(certificatePath(root, digest))
	if err != nil {
		return QuestionResolutionCertificate{}, err
	}
	var c QuestionResolutionCertificate
	if err := json.Unmarshal(data, &c); err != nil {
		return QuestionResolutionCertificate{}, err
	}
	if err := ValidateCertificate(c); err != nil {
		return QuestionResolutionCertificate{}, err
	}
	return c, nil
}
