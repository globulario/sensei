// SPDX-License-Identifier: AGPL-3.0-only

package investigation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// SHA256String computes the SHA256 hash of a string and returns it as a hex string.
func SHA256String(data string) string {
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// SHA256Bytes computes the SHA256 hash of bytes and returns it as a hex string.
func SHA256Bytes(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// CalculateDocumentDigest computes a stable SHA256 digest of the Document
// after normalizing it, and with the Receipt.OutputDocumentDigestSHA256 field cleared.
func CalculateDocumentDigest(doc Document) (string, error) {
	// Clear ONLY the self-referential OutputDocumentDigestSHA256 field.
	doc.Receipt.OutputDocumentDigestSHA256 = ""

	normalized, err := Normalize(doc)
	if err != nil {
		return "", err
	}

	data, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}

	return SHA256Bytes(data), nil
}
