// SPDX-License-Identifier: AGPL-3.0-only

package identity

import (
	"reflect"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

func indexTrusting(issuer string) authority.PolicyIndex {
	return authority.PolicyIndex{
		ActorRoles: map[string]authority.ActorRole{
			DefaultRoleID: {
				ID:                DefaultRoleID,
				Status:            "active",
				AllowedActorKinds: []closureprotocol.ActorKind{closureprotocol.ActorAgent},
				TrustedIssuers:    []string{issuer},
			},
		},
	}
}

func enrollAt(t *testing.T, issuer string) (string, AgentIdentity) {
	t.Helper()
	root := t.TempDir()
	id, err := Enroll(EnrollOptions{Root: root, Issuer: issuer, Now: time.Unix(1_700_000_000, 0).UTC()})
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	return root, id
}

func TestEnrollProducesVerifiableActor(t *testing.T) {
	root, id := enrollAt(t, DefaultIssuer)
	verified, err := authority.VerifyActorBinding(id.ActorBinding(), Resolver(root), indexTrusting(DefaultIssuer), time.Unix(1_700_000_100, 0).UTC())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verified.Status != closureprotocol.ReceiptValid {
		t.Fatalf("status = %s, want valid", verified.Status)
	}
	found := false
	for _, r := range verified.VerifiedRoleIDs {
		if r == DefaultRoleID {
			found = true
		}
	}
	if !found {
		t.Fatalf("role %s not verified, got %v", DefaultRoleID, verified.VerifiedRoleIDs)
	}
}

func TestEnrollForeignIssuerRejected(t *testing.T) {
	// Enrolled as an issuer the policy does not trust for the role.
	root, id := enrollAt(t, "evil.local")
	if _, err := authority.VerifyActorBinding(id.ActorBinding(), Resolver(root), indexTrusting(DefaultIssuer), time.Unix(1_700_000_100, 0).UTC()); err == nil {
		t.Fatal("expected verification to reject an untrusted issuer, got nil error")
	}
}

func TestLoadManifestAbsentFailsSoft(t *testing.T) {
	id, ok, err := LoadManifest(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for un-enrolled dir, got identity %+v", id)
	}
}

func TestManifestRoundTrips(t *testing.T) {
	root, id := enrollAt(t, DefaultIssuer)
	loaded, ok, err := LoadManifest(root)
	if err != nil || !ok {
		t.Fatalf("load manifest: ok=%v err=%v", ok, err)
	}
	if !reflect.DeepEqual(id, loaded) {
		t.Fatalf("manifest round-trip mismatch:\n enrolled %+v\n loaded   %+v", id, loaded)
	}
}
