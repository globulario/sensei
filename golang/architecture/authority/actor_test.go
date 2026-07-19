// SPDX-License-Identifier: Apache-2.0

package authority

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"gopkg.in/yaml.v3"
)

type testBundle struct {
	root string
}

func newTestBundle(t *testing.T) *testBundle {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "artifacts", "sha256"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &testBundle{root: root}
}

func (b *testBundle) resolver() *LocalBundleResolver {
	return NewLocalBundleResolver(b.root)
}

func (b *testBundle) storeBytes(t *testing.T, data []byte, ext, mediaType string) closureprotocol.LedgerPayloadRef {
	t.Helper()
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	path := filepath.Join(b.root, "artifacts", "sha256", digest+"."+ext)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return closureprotocol.LedgerPayloadRef{
		Path:         filepath.ToSlash(filepath.Join("artifacts", "sha256", digest+"."+ext)),
		MediaType:    mediaType,
		DigestSHA256: digest,
	}
}

func (b *testBundle) storeYAMLByDigest(t *testing.T, digest string, value any) {
	t.Helper()
	data, err := yaml.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(b.root, "artifacts", "sha256", digest+".yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (b *testBundle) storeAuthenticationReceipt(t *testing.T, receipt closureprotocol.AuthenticationReceipt) string {
	t.Helper()
	digest, err := closureprotocol.AuthenticationReceiptDigest(receipt)
	if err != nil {
		t.Fatal(err)
	}
	receipt.ReceiptDigestSHA256 = digest
	b.storeYAMLByDigest(t, digest, receipt)
	return digest
}

func (b *testBundle) storeRoleAttestationReceipt(t *testing.T, receipt closureprotocol.RoleAttestationReceipt) string {
	t.Helper()
	digest, err := closureprotocol.RoleAttestationReceiptDigest(receipt)
	if err != nil {
		t.Fatal(err)
	}
	receipt.ReceiptDigestSHA256 = digest
	b.storeYAMLByDigest(t, digest, receipt)
	return digest
}

func (b *testBundle) storeDelegationReceipt(t *testing.T, receipt closureprotocol.DelegationReceipt) string {
	t.Helper()
	digest, err := closureprotocol.DelegationReceiptDigest(receipt)
	if err != nil {
		t.Fatal(err)
	}
	receipt.ReceiptDigestSHA256 = digest
	b.storeYAMLByDigest(t, digest, receipt)
	return digest
}

func testPolicyIndex() PolicyIndex {
	return PolicyIndex{
		ActorRoles: map[string]ActorRole{
			"role.repository_repair_agent": {
				ID:                "role.repository_repair_agent",
				Status:            "active",
				AllowedActorKinds: []closureprotocol.ActorKind{closureprotocol.ActorAgent, closureprotocol.ActorHuman},
				TrustedIssuers:    []string{"sensei.local"},
				Aliases:           []string{"repository repair agent"},
			},
			"role.repository_maintainer": {
				ID:                "role.repository_maintainer",
				Status:            "active",
				AllowedActorKinds: []closureprotocol.ActorKind{closureprotocol.ActorAgent, closureprotocol.ActorHuman},
				TrustedIssuers:    []string{"sensei.local"},
			},
		},
		MutationPaths: map[string]MutationPath{
			"mutation_path.repository_edit": {
				ID:            "mutation_path.repository_edit",
				Status:        "active",
				MechanismKind: closureprotocol.MechanismRepositoryEdit,
				TargetKinds:   []string{"source_file", "governed_source"},
			},
			"mutation_path.owner_rpc": {
				ID:            "mutation_path.owner_rpc",
				Status:        "active",
				MechanismKind: closureprotocol.MechanismOwnerRPC,
				TargetKinds:   []string{"runtime_state"},
			},
		},
		DelegationPolicies: map[string]DelegationPolicy{
			"delegation_policy.repository_repair": {
				ID:                  "delegation_policy.repository_repair",
				Status:              "active",
				MaximumDepth:        1,
				MaximumDuration:     "24h",
				AllowSubdelegation:  false,
				AllowedActions:      []closureprotocol.OperationKind{closureprotocol.OperationModify, closureprotocol.OperationRead, closureprotocol.OperationCreate},
				AllowedMechanismIDs: []string{"mutation_path.repository_edit"},
			},
		},
		AuthorityDomains: map[string]AuthorityDomain{
			"authority.sensei_closure": {
				ID:               "authority.sensei_closure",
				Status:           "active",
				MayWriteRoleIDs:  []string{"role.repository_repair_agent"},
				MustMutateViaIDs: []string{"mutation_path.owner_rpc"},
			},
		},
		AuthorityGrants: map[string]AuthorityGrant{
			"grant.sensei.closure_repository_edit": {
				ID:                   "grant.sensei.closure_repository_edit",
				Status:               "active",
				ActorRoleIDs:         []string{"role.repository_repair_agent"},
				AuthorityDomainIDs:   []string{"authority.sensei_closure"},
				Actions:              []closureprotocol.OperationKind{closureprotocol.OperationModify, closureprotocol.OperationRead, closureprotocol.OperationCreate},
				TargetKinds:          []string{"source_file"},
				RequiredMechanismIDs: []string{"mutation_path.repository_edit"},
				MaximumRiskClass:     "architecture_sensitive",
				ValidFrom:            "2026-07-15T00:00:00Z",
				Delegable:            true,
				DelegationPolicyID:   "delegation_policy.repository_repair",
			},
		},
	}
}

func verifiedActorFixture(t *testing.T) (VerifiedActor, PolicyIndex, ActorBinding) {
	t.Helper()
	bundle := newTestBundle(t)
	index := testPolicyIndex()
	artifact := bundle.storeBytes(t, []byte("signed-local-authn"), "bin", "application/octet-stream")
	authnDigest := bundle.storeAuthenticationReceipt(t, closureprotocol.AuthenticationReceipt{
		ReceiptID:              "authn.local.actor-1",
		PrincipalID:            "actor.codex.session-1",
		Issuer:                 "sensei.local",
		AuthenticationArtifact: artifact,
		AuthenticatedAt:        "2026-07-15T12:00:00Z",
		Status:                 closureprotocol.ReceiptValid,
	})
	roleDigest := bundle.storeRoleAttestationReceipt(t, closureprotocol.RoleAttestationReceipt{
		ReceiptID:                         "role.local.actor-1",
		PrincipalID:                       "actor.codex.session-1",
		ActorKind:                         closureprotocol.ActorAgent,
		Issuer:                            "sensei.local",
		RoleIDs:                           []string{"role.repository_repair_agent"},
		AuthenticationReceiptDigestSHA256: authnDigest,
		IssuedAt:                          "2026-07-15T12:00:00Z",
		ValidUntil:                        "2026-07-16T12:00:00Z",
		Status:                            closureprotocol.ReceiptValid,
	})
	binding := closureprotocol.ActorBinding{
		PrincipalID:                       "actor.codex.session-1",
		ActorKind:                         closureprotocol.ActorAgent,
		Roles:                             []string{"role.repository_repair_agent"},
		Issuer:                            "sensei.local",
		AuthenticationReceiptDigestSHA256: authnDigest,
		RoleAttestationReceiptDigests:     []string{roleDigest},
	}
	got, err := VerifyActorBinding(binding, bundle.resolver(), index, time.Date(2026, 7, 15, 13, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("VerifyActorBinding: %v", err)
	}
	return got, index, binding
}

func TestVerifyActorBindingValidTrustedReceipt(t *testing.T) {
	got, _, _ := verifiedActorFixture(t)
	if got.Status != closureprotocol.ReceiptValid {
		t.Fatalf("status = %s, want %s", got.Status, closureprotocol.ReceiptValid)
	}
	if len(got.VerifiedRoleIDs) != 1 || got.VerifiedRoleIDs[0] != "role.repository_repair_agent" {
		t.Fatalf("verified roles = %v", got.VerifiedRoleIDs)
	}
}

func TestVerifyActorBindingRejectsUnverifiedClaimedRole(t *testing.T) {
	bundle := newTestBundle(t)
	index := testPolicyIndex()
	artifact := bundle.storeBytes(t, []byte("signed-local-authn"), "bin", "application/octet-stream")
	authnDigest := bundle.storeAuthenticationReceipt(t, closureprotocol.AuthenticationReceipt{
		ReceiptID:              "authn.local.actor-1",
		PrincipalID:            "actor.codex.session-1",
		Issuer:                 "sensei.local",
		AuthenticationArtifact: artifact,
		AuthenticatedAt:        "2026-07-15T12:00:00Z",
		Status:                 closureprotocol.ReceiptValid,
	})
	roleDigest := bundle.storeRoleAttestationReceipt(t, closureprotocol.RoleAttestationReceipt{
		ReceiptID:                         "role.local.actor-1",
		PrincipalID:                       "actor.codex.session-1",
		ActorKind:                         closureprotocol.ActorAgent,
		Issuer:                            "sensei.local",
		RoleIDs:                           []string{"role.repository_repair_agent"},
		AuthenticationReceiptDigestSHA256: authnDigest,
		IssuedAt:                          "2026-07-15T12:00:00Z",
		ValidUntil:                        "2026-07-16T12:00:00Z",
		Status:                            closureprotocol.ReceiptValid,
	})
	binding := closureprotocol.ActorBinding{
		PrincipalID:                       "actor.codex.session-1",
		ActorKind:                         closureprotocol.ActorAgent,
		Roles:                             []string{"role.repository_maintainer"},
		Issuer:                            "sensei.local",
		AuthenticationReceiptDigestSHA256: authnDigest,
		RoleAttestationReceiptDigests:     []string{roleDigest},
	}
	_, err := VerifyActorBinding(binding, bundle.resolver(), index, time.Date(2026, 7, 15, 13, 0, 0, 0, time.UTC))
	if err == nil || err.Error() != "no claimed roles were verified" {
		t.Fatalf("err = %v, want no claimed roles were verified", err)
	}
}

func TestVerifyActorBindingRejectsUnknownIssuer(t *testing.T) {
	bundle := newTestBundle(t)
	index := testPolicyIndex()
	artifact := bundle.storeBytes(t, []byte("signed-local-authn"), "bin", "application/octet-stream")
	authnDigest := bundle.storeAuthenticationReceipt(t, closureprotocol.AuthenticationReceipt{
		ReceiptID:              "authn.local.actor-1",
		PrincipalID:            "actor.codex.session-1",
		Issuer:                 "sensei.local",
		AuthenticationArtifact: artifact,
		AuthenticatedAt:        "2026-07-15T12:00:00Z",
		Status:                 closureprotocol.ReceiptValid,
	})
	roleDigest := bundle.storeRoleAttestationReceipt(t, closureprotocol.RoleAttestationReceipt{
		ReceiptID:                         "role.local.actor-1",
		PrincipalID:                       "actor.codex.session-1",
		ActorKind:                         closureprotocol.ActorAgent,
		Issuer:                            "github.actions",
		RoleIDs:                           []string{"role.repository_repair_agent"},
		AuthenticationReceiptDigestSHA256: authnDigest,
		IssuedAt:                          "2026-07-15T12:00:00Z",
		ValidUntil:                        "2026-07-16T12:00:00Z",
		Status:                            closureprotocol.ReceiptValid,
	})
	binding := closureprotocol.ActorBinding{
		PrincipalID:                       "actor.codex.session-1",
		ActorKind:                         closureprotocol.ActorAgent,
		Roles:                             []string{"role.repository_repair_agent"},
		Issuer:                            "sensei.local",
		AuthenticationReceiptDigestSHA256: authnDigest,
		RoleAttestationReceiptDigests:     []string{roleDigest},
	}
	_, err := VerifyActorBinding(binding, bundle.resolver(), index, time.Date(2026, 7, 15, 13, 0, 0, 0, time.UTC))
	if err == nil || err.Error() != "issuer github.actions is not trusted for role role.repository_repair_agent" {
		t.Fatalf("err = %v", err)
	}
}
