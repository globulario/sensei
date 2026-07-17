// SPDX-License-Identifier: Apache-2.0

package ledger

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// ReadVerifiedPayload reads the payload bytes for a verified entry and revalidates
// them against the entry's recorded payload digest before returning. Chain
// verification freezes which entries exist, but a mutation of a payload file
// between verification and reuse would otherwise cross the boundary unnoticed; this
// re-checks the bytes so a reconstruction reads only content that still matches the
// verified entry, failing closed on any drift.
func ReadVerifiedPayload(ve VerifiedEntry) ([]byte, error) {
	data, err := os.ReadFile(ve.PayloadPath)
	if err != nil {
		return nil, err
	}
	digest, err := semanticDigestForBytes(ve.Entry.Payload.MediaType, data)
	if err != nil {
		return nil, err
	}
	if digest != ve.Entry.Payload.DigestSHA256 {
		return nil, fmt.Errorf("ledger.payload_digest_mismatch: payload for entry %s changed after verification", ve.Entry.EntryDigestSHA256)
	}
	return data, nil
}

// RewriteLatestPayloadForTest re-stores a replacement payload for the latest entry
// and rebuilds that entry and HEAD consistently (recomputed payload ref and entry
// digest), so the ledger chain stays VALID while the payload content is changed.
// It exists only for adversarial tests that must reach the strict recorded-transition
// binding validator rather than a generic broken-chain rejection.
func RewriteLatestPayloadForTest(taskDir string, payload any, mediaType string, validator PayloadValidator) error {
	s := NewStore(taskDir, WithPayloadValidator(validator))
	chain, err := loadVerifiedChain(taskDir, validator)
	if err != nil {
		return err
	}
	if len(chain.Entries) == 0 {
		return fmt.Errorf("no entries to rewrite")
	}
	last := chain.Entries[len(chain.Entries)-1]
	rendered, err := renderPayload(payload, mediaType)
	if err != nil {
		return err
	}
	if err := storePayloadArtifacts(taskDir, rendered); err != nil {
		return err
	}
	entry := last.Entry
	entry.Payload = closureprotocol.LedgerPayloadRef{Path: rendered.path, MediaType: rendered.mediaType, DigestSHA256: rendered.semanticDigest}
	entry.EntryDigestSHA256 = ""
	digest, err := closureprotocol.LedgerEntryDigest(entry)
	if err != nil {
		return err
	}
	entry.EntryDigestSHA256 = digest
	// Remove the old entry file (EntryPath is absolute) and write the rebuilt one.
	if err := os.Remove(filepath.FromSlash(last.EntryPath)); err != nil {
		return err
	}
	newPath := filepath.Join(s.ledgerDir(), ledgerEntryFilename(entry.Sequence, entry.EventType, digest))
	if err := writeEntry(newPath, entry); err != nil {
		return err
	}
	head := Head{SchemaVersion: HeadSchemaVersion, TaskID: entry.Task.ID, Sequence: entry.Sequence, EntryDigestSHA256: digest,
		EntryPath: filepath.ToSlash(filepath.Join("ledger", ledgerEntryFilename(entry.Sequence, entry.EventType, digest)))}
	return writeHead(s.headPath(), head)
}
