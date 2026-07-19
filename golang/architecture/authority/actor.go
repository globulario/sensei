// SPDX-License-Identifier: Apache-2.0

package authority

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"gopkg.in/yaml.v3"
)

type LocalBundleResolver struct {
	root string
}

func NewLocalBundleResolver(root string) *LocalBundleResolver {
	return &LocalBundleResolver{root: root}
}

func (r *LocalBundleResolver) ResolveByDigest(digest string) ([]byte, error) {
	matches, err := filepath.Glob(filepath.Join(r.root, "artifacts", "sha256", digest+".*"))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("artifact digest %s not found", digest)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("artifact digest %s is ambiguous", digest)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		return nil, err
	}
	mediaType := "application/octet-stream"
	switch filepath.Ext(matches[0]) {
	case ".yaml", ".yml":
		return data, nil
	case ".json":
		return data, nil
	}
	computed, err := ledgerSemanticDigest(mediaType, data)
	if err != nil {
		return nil, err
	}
	if computed != strings.TrimSpace(digest) {
		return nil, fmt.Errorf("artifact digest mismatch for %s", digest)
	}
	return data, nil
}

func (r *LocalBundleResolver) ResolveArtifact(ref closureprotocol.LedgerPayloadRef) ([]byte, error) {
	path := filepath.Clean(filepath.Join(r.root, filepath.FromSlash(ref.Path)))
	if !strings.HasPrefix(path, filepath.Clean(r.root)+string(filepath.Separator)) {
		return nil, fmt.Errorf("artifact path escapes bundle root: %s", ref.Path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != strings.TrimSpace(ref.DigestSHA256) {
		return nil, fmt.Errorf("artifact digest mismatch for %s", ref.Path)
	}
	return data, nil
}

func VerifyActorBinding(binding closureprotocol.ActorBinding, resolver ArtifactResolver, index PolicyIndex, evaluatedAt time.Time) (VerifiedActor, error) {
	if err := closureprotocol.ValidateActorBinding(binding); err != nil {
		return VerifiedActor{}, err
	}
	result := VerifiedActor{
		PrincipalID: binding.PrincipalID,
		ActorKind:   binding.ActorKind,
		Issuer:      strings.TrimSpace(binding.Issuer),
		Status:      closureprotocol.ReceiptUnknown,
	}
	authn, err := loadAuthnReceipt(resolver, binding.AuthenticationReceiptDigestSHA256)
	if err != nil {
		return result, err
	}
	if err := closureprotocol.ValidateAuthenticationReceipt(authn); err != nil {
		return result, err
	}
	if authn.PrincipalID != binding.PrincipalID {
		return result, fmt.Errorf("authentication principal %q does not match actor %q", authn.PrincipalID, binding.PrincipalID)
	}
	if strings.TrimSpace(binding.Issuer) != "" && strings.TrimSpace(binding.Issuer) != strings.TrimSpace(authn.Issuer) {
		return result, fmt.Errorf("actor issuer %q does not match authentication issuer %q", binding.Issuer, authn.Issuer)
	}
	if authn.Status != closureprotocol.ReceiptValid {
		return result, fmt.Errorf("authentication receipt status is %s", authn.Status)
	}
	if authn.ExpiresAt != "" {
		expiresAt, err := time.Parse(time.RFC3339, authn.ExpiresAt)
		if err != nil {
			return result, err
		}
		if !evaluatedAt.Before(expiresAt) {
			return result, fmt.Errorf("authentication receipt expired")
		}
	}
	if authn.ReceiptDigestSHA256 != "" {
		d, err := closureprotocol.AuthenticationReceiptDigest(authn)
		if err != nil {
			return result, err
		}
		if d != strings.TrimSpace(binding.AuthenticationReceiptDigestSHA256) {
			return result, fmt.Errorf("authentication receipt digest mismatch")
		}
	}
	if _, err := resolver.ResolveArtifact(authn.AuthenticationArtifact); err != nil {
		return result, err
	}
	result.AuthenticationReceiptDigestSHA256 = strings.TrimSpace(binding.AuthenticationReceiptDigestSHA256)

	claimedRoles := map[string]bool{}
	for _, role := range binding.Roles {
		roleID, ok, err := index.ResolveRoleIDOrAlias(role)
		if err != nil {
			return result, err
		}
		if ok {
			claimedRoles[roleID] = true
		} else {
			claimedRoles[strings.TrimSpace(role)] = true
		}
	}
	for _, digest := range binding.RoleAttestationReceiptDigests {
		receipt, err := loadRoleAttestationReceipt(resolver, digest)
		if err != nil {
			return result, err
		}
		if err := closureprotocol.ValidateRoleAttestationReceipt(receipt); err != nil {
			return result, err
		}
		if receipt.PrincipalID != binding.PrincipalID {
			return result, fmt.Errorf("role attestation principal %q does not match actor %q", receipt.PrincipalID, binding.PrincipalID)
		}
		if receipt.ActorKind != binding.ActorKind {
			return result, fmt.Errorf("role attestation actor kind %q does not match actor %q", receipt.ActorKind, binding.ActorKind)
		}
		if receipt.Status != closureprotocol.ReceiptValid {
			return result, fmt.Errorf("role attestation %s status is %s", receipt.ReceiptID, receipt.Status)
		}
		if receipt.AuthenticationReceiptDigestSHA256 != "" && receipt.AuthenticationReceiptDigestSHA256 != result.AuthenticationReceiptDigestSHA256 {
			return result, fmt.Errorf("role attestation %s references a different authentication receipt", receipt.ReceiptID)
		}
		if receipt.ValidUntil != "" {
			validUntil, err := time.Parse(time.RFC3339, receipt.ValidUntil)
			if err != nil {
				return result, err
			}
			if !evaluatedAt.Before(validUntil) {
				return result, fmt.Errorf("role attestation %s expired", receipt.ReceiptID)
			}
		}
		for _, roleID := range receipt.RoleIDs {
			role, ok := index.ActorRoles[roleID]
			if !ok {
				return result, fmt.Errorf("role %s is unknown", roleID)
			}
			if role.Status != "active" {
				return result, fmt.Errorf("role %s is not active", roleID)
			}
			if !containsActorKind(role.AllowedActorKinds, binding.ActorKind) {
				return result, fmt.Errorf("actor kind %s is not allowed for role %s", binding.ActorKind, roleID)
			}
			if !containsString(role.TrustedIssuers, receipt.Issuer) {
				return result, fmt.Errorf("issuer %s is not trusted for role %s", receipt.Issuer, roleID)
			}
			if claimedRoles[roleID] {
				result.VerifiedRoleIDs = append(result.VerifiedRoleIDs, roleID)
			}
		}
	}
	result.VerifiedRoleIDs = cleanSet(result.VerifiedRoleIDs)
	for _, digest := range binding.DelegationReceiptDigests {
		receipt, err := loadDelegationReceipt(resolver, digest)
		if err != nil {
			return result, err
		}
		if err := closureprotocol.ValidateDelegationReceipt(receipt); err != nil {
			return result, err
		}
		if receipt.DelegatedPrincipalID != binding.PrincipalID {
			return result, fmt.Errorf("delegation %s is not delegated to actor %s", receipt.DelegationID, binding.PrincipalID)
		}
		if receipt.Status != closureprotocol.ReceiptValid {
			return result, fmt.Errorf("delegation %s status is %s", receipt.DelegationID, receipt.Status)
		}
		validFrom, err := time.Parse(time.RFC3339, receipt.ValidFrom)
		if err != nil {
			return result, err
		}
		if evaluatedAt.Before(validFrom) {
			return result, fmt.Errorf("delegation %s is not active yet", receipt.DelegationID)
		}
		if receipt.ValidUntil != "" {
			validUntil, err := time.Parse(time.RFC3339, receipt.ValidUntil)
			if err != nil {
				return result, err
			}
			if !evaluatedAt.Before(validUntil) {
				return result, fmt.Errorf("delegation %s expired", receipt.DelegationID)
			}
		}
		result.DelegationReceipts = append(result.DelegationReceipts, receipt)
	}
	if len(result.VerifiedRoleIDs) == 0 {
		return result, fmt.Errorf("no claimed roles were verified")
	}
	result.Status = closureprotocol.ReceiptValid
	return result, nil
}

func loadAuthnReceipt(resolver ArtifactResolver, digest string) (closureprotocol.AuthenticationReceipt, error) {
	data, err := resolver.ResolveByDigest(strings.TrimSpace(digest))
	if err != nil {
		return closureprotocol.AuthenticationReceipt{}, err
	}
	var receipt closureprotocol.AuthenticationReceipt
	if err := yaml.Unmarshal(data, &receipt); err != nil {
		return closureprotocol.AuthenticationReceipt{}, err
	}
	return receipt, nil
}

func loadRoleAttestationReceipt(resolver ArtifactResolver, digest string) (closureprotocol.RoleAttestationReceipt, error) {
	data, err := resolver.ResolveByDigest(strings.TrimSpace(digest))
	if err != nil {
		return closureprotocol.RoleAttestationReceipt{}, err
	}
	var receipt closureprotocol.RoleAttestationReceipt
	if err := yaml.Unmarshal(data, &receipt); err != nil {
		return closureprotocol.RoleAttestationReceipt{}, err
	}
	return receipt, nil
}

func loadDelegationReceipt(resolver ArtifactResolver, digest string) (closureprotocol.DelegationReceipt, error) {
	data, err := resolver.ResolveByDigest(strings.TrimSpace(digest))
	if err != nil {
		return closureprotocol.DelegationReceipt{}, err
	}
	var receipt closureprotocol.DelegationReceipt
	if err := yaml.Unmarshal(data, &receipt); err != nil {
		return closureprotocol.DelegationReceipt{}, err
	}
	return receipt, nil
}

func containsActorKind(in []closureprotocol.ActorKind, want closureprotocol.ActorKind) bool {
	for _, item := range in {
		if item == want {
			return true
		}
	}
	return false
}

func containsString(in []string, want string) bool {
	for _, item := range in {
		if item == want {
			return true
		}
	}
	return false
}

func ledgerSemanticDigest(mediaType string, data []byte) (string, error) {
	switch strings.TrimSpace(mediaType) {
	case "application/yaml", "text/yaml", "application/x-yaml":
		var value any
		if err := yaml.Unmarshal(data, &value); err != nil {
			return "", err
		}
		return closureprotocol.SemanticDigest(value)
	case "application/json":
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return "", err
		}
		return closureprotocol.SemanticDigest(value)
	default:
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:]), nil
	}
}
