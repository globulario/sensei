// SPDX-License-Identifier: AGPL-3.0-only

package governancepack

import (
	"fmt"
	"strings"
	"time"
)

// VerifyPublisherSignature authenticates arbitrary document bytes as having been
// issued by a trusted publisher.
//
// This is the pack verifier's trust model — out-of-band trust store, publisher
// and key-ID lookup, key-state policy, Ed25519 over the exact bytes — separated
// from the pack schema so other governed documents can reuse it without
// pretending to be packs. There must be one trust store, one key-state policy
// and one signature implementation; a second copy of publisher trust is a second
// thing to get wrong.
//
// It answers the question a trusted_issuers string cannot: not "does this
// document claim a trusted issuer" but "did that issuer actually sign these
// bytes". The distinction is the whole point — a caller can write any issuer
// name it likes, but cannot produce a signature for a key it does not hold.
//
// documentBytes must be the EXACT bytes the signature covers. Callers must parse
// their document from those same bytes and never from a separately supplied
// value, or the signature stops covering what they act on.
func VerifyPublisherSignature(documentBytes, signature []byte, publisherID, keyID, algorithm string, store TrustStore, evaluatedAt time.Time) (TrustedKey, string, error) {
	if len(documentBytes) == 0 {
		return TrustedKey{}, "", fmt.Errorf("%w: no document bytes to verify", ErrSignatureInvalid)
	}
	if len(signature) == 0 {
		return TrustedKey{}, "", fmt.Errorf("%w: no signature supplied", ErrSignatureInvalid)
	}
	if strings.TrimSpace(publisherID) == "" {
		return TrustedKey{}, "", fmt.Errorf("%w: no publisher id supplied", ErrUnknownPublisher)
	}
	if strings.TrimSpace(keyID) == "" {
		return TrustedKey{}, "", fmt.Errorf("%w: no key id supplied", ErrUnknownPublisher)
	}
	key, warning, err := lookupTrustedKey(store, strings.TrimSpace(publisherID), strings.TrimSpace(keyID), strings.TrimSpace(algorithm), evaluatedAt)
	if err != nil {
		return TrustedKey{}, "", err
	}
	if err := verifyManifestSignature(documentBytes, signature, key); err != nil {
		return TrustedKey{}, "", err
	}
	return key, warning, nil
}
