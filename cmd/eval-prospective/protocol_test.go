// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The protocol document is the ruler. A run against an edited ruler that still
// reported the unedited ruler's name would produce a number nobody could trace
// back to a rule, and the mismatch would surface only when somebody tried to
// reproduce it.
func TestAnEditedProtocolIsRefused(t *testing.T) {
	root := t.TempDir()
	full := filepath.Join(root, filepath.FromSlash(prospectiveV1.Path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("a protocol that is not the frozen one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := prospectiveV1.verify(root)
	if err == nil {
		t.Fatal("an edited protocol document was accepted")
	}
	for _, want := range []string{prospectiveV1.ID, prospectiveV1.DigestSHA256, "mismatch"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not name %q: %v", want, err)
		}
	}
}

// A missing document is refused too. Defaulting to "no document, no check"
// would make the digest binding advisory exactly when it matters.
func TestAMissingProtocolIsRefused(t *testing.T) {
	if err := prospectiveV1.verify(t.TempDir()); err == nil {
		t.Fatal("a checkout without the protocol document was accepted")
	}
}

// The real repository must satisfy its own registered digest, or the constant
// in this binary has drifted from the merged document.
func TestTheRegisteredDigestMatchesTheRepository(t *testing.T) {
	if err := prospectiveV1.verify("../.."); err != nil {
		t.Fatalf("the registered protocol digest no longer matches the repository: %v", err)
	}
}

func TestAnUnregisteredProtocolIsRefused(t *testing.T) {
	if _, err := protocolByID("prospective-recall-protocol-v99"); err == nil {
		t.Fatal("an unregistered protocol was resolved, so a run could claim a compliance nobody checked")
	}
}
