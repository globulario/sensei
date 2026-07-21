// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"fmt"
	"os"
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
