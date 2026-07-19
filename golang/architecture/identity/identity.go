// SPDX-License-Identifier: AGPL-3.0-only

// Package identity materializes and loads a locally-trusted agent identity for
// admission v2. Enrollment authors a structurally valid receipt bundle
// (authentication artifact + authentication receipt + role-attestation receipt)
// as the already-trusted "sensei.local" issuer, content-addressed so
// authority.VerifyActorBinding accepts it. It does not expand the trusted-issuer
// set: sensei.local is blessed for role.repository_repair_agent by the
// human-authored docs/awareness/actor_roles.yaml. The trust act is explicit —
// enrollment is a separate, human-invoked step; task preparation only consumes
// an already-enrolled identity and never mints one itself.
package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"gopkg.in/yaml.v3"
)

const (
	// ManifestSchemaVersion identifies the on-disk agent identity manifest.
	ManifestSchemaVersion = "identity.agent.v1"
	// DefaultPrincipalID is the local machine agent principal.
	DefaultPrincipalID = "agent.sensei.local"
	// DefaultIssuer is the locally-trusted issuer already blessed in policy.
	DefaultIssuer = "sensei.local"
	// DefaultRoleID is the role a local repair agent enrolls for.
	DefaultRoleID = "role.repository_repair_agent"

	manifestFileName = "agent.yaml"
)

// AgentIdentity is the on-disk manifest describing an enrolled agent and the
// digests of the receipts that back it.
type AgentIdentity struct {
	SchemaVersion                     string                    `json:"schema_version" yaml:"schema_version"`
	PrincipalID                       string                    `json:"principal_id" yaml:"principal_id"`
	ActorKind                         closureprotocol.ActorKind `json:"actor_kind" yaml:"actor_kind"`
	Issuer                            string                    `json:"issuer" yaml:"issuer"`
	Roles                             []string                  `json:"roles" yaml:"roles"`
	AuthenticationReceiptDigestSHA256 string                    `json:"authentication_receipt_digest_sha256" yaml:"authentication_receipt_digest_sha256"`
	RoleAttestationReceiptDigests     []string                  `json:"role_attestation_receipt_digests_sha256" yaml:"role_attestation_receipt_digests_sha256"`
}

// Root returns the identity store root for a repository checkout. Machine
// identity lives under .sensei/identity/ so it is isolated from committed code.
func Root(repoRoot string) string {
	return filepath.Join(repoRoot, ".sensei", "identity")
}

// Resolver returns an artifact resolver over an identity store root.
func Resolver(root string) *authority.LocalBundleResolver {
	return authority.NewLocalBundleResolver(root)
}

// ActorBinding builds the actor binding the manifest describes.
func (id AgentIdentity) ActorBinding() closureprotocol.ActorBinding {
	return closureprotocol.ActorBinding{
		PrincipalID:                       id.PrincipalID,
		ActorKind:                         id.ActorKind,
		Roles:                             append([]string(nil), id.Roles...),
		Issuer:                            id.Issuer,
		AuthenticationReceiptDigestSHA256: id.AuthenticationReceiptDigestSHA256,
		RoleAttestationReceiptDigests:     append([]string(nil), id.RoleAttestationReceiptDigests...),
	}
}

// EnrollOptions parameterizes a local enrollment.
type EnrollOptions struct {
	Root        string        // identity store root, e.g. Root(repoRoot)
	PrincipalID string        // defaults to DefaultPrincipalID
	Issuer      string        // defaults to DefaultIssuer
	Roles       []string      // defaults to [DefaultRoleID]
	Now         time.Time     // enrollment timestamp (caller-supplied for determinism)
	ValidFor    time.Duration // 0 means no expiry
}

// authnArtifact is the authentication evidence blob the authn receipt points at.
type authnArtifact struct {
	PrincipalID string `json:"principal_id"`
	Issuer      string `json:"issuer"`
	Method      string `json:"method"`
}

// Enroll materializes the receipt bundle and manifest under opts.Root and
// returns the manifest. It overwrites any prior enrollment for the principal.
func Enroll(opts EnrollOptions) (AgentIdentity, error) {
	root := strings.TrimSpace(opts.Root)
	if root == "" {
		return AgentIdentity{}, errors.New("identity root is required")
	}
	principal := firstNonEmpty(opts.PrincipalID, DefaultPrincipalID)
	issuer := firstNonEmpty(opts.Issuer, DefaultIssuer)
	roles := opts.Roles
	if len(roles) == 0 {
		roles = []string{DefaultRoleID}
	}
	now := opts.Now.UTC()

	// 1. Authentication artifact (evidence), content-addressed by raw sha256 —
	//    the authentication receipt references it and VerifyActorBinding resolves
	//    and re-digests it.
	artifactBytes, err := json.Marshal(authnArtifact{PrincipalID: principal, Issuer: issuer, Method: "local_enrollment"})
	if err != nil {
		return AgentIdentity{}, err
	}
	artifactRel := filepath.ToSlash(filepath.Join("authn", principal+".json"))
	if err := writeUnder(root, artifactRel, artifactBytes); err != nil {
		return AgentIdentity{}, err
	}

	// 2. Authentication receipt.
	authn := closureprotocol.AuthenticationReceipt{
		ReceiptID:              "authn." + principal,
		PrincipalID:            principal,
		Issuer:                 issuer,
		AuthenticationArtifact: closureprotocol.LedgerPayloadRef{Path: artifactRel, MediaType: "application/json", DigestSHA256: rawSHA256(artifactBytes)},
		AuthenticatedAt:        now.Format(time.RFC3339),
		Status:                 closureprotocol.ReceiptValid,
	}
	if opts.ValidFor > 0 {
		authn.ExpiresAt = now.Add(opts.ValidFor).Format(time.RFC3339)
	}
	authnDigest, err := closureprotocol.AuthenticationReceiptDigest(authn)
	if err != nil {
		return AgentIdentity{}, err
	}
	authn.ReceiptDigestSHA256 = authnDigest
	if err := closureprotocol.ValidateAuthenticationReceipt(authn); err != nil {
		return AgentIdentity{}, fmt.Errorf("authentication receipt: %w", err)
	}
	if err := storeReceipt(root, authnDigest, authn); err != nil {
		return AgentIdentity{}, err
	}

	// 3. Role-attestation receipt, bound to the authentication receipt.
	role := closureprotocol.RoleAttestationReceipt{
		ReceiptID:                         "role." + principal,
		PrincipalID:                       principal,
		ActorKind:                         closureprotocol.ActorAgent,
		Issuer:                            issuer,
		RoleIDs:                           append([]string(nil), roles...),
		AuthenticationReceiptDigestSHA256: authnDigest,
		IssuedAt:                          now.Format(time.RFC3339),
		Status:                            closureprotocol.ReceiptValid,
	}
	if opts.ValidFor > 0 {
		role.ValidUntil = now.Add(opts.ValidFor).Format(time.RFC3339)
	}
	roleDigest, err := closureprotocol.RoleAttestationReceiptDigest(role)
	if err != nil {
		return AgentIdentity{}, err
	}
	role.ReceiptDigestSHA256 = roleDigest
	if err := closureprotocol.ValidateRoleAttestationReceipt(role); err != nil {
		return AgentIdentity{}, fmt.Errorf("role attestation receipt: %w", err)
	}
	if err := storeReceipt(root, roleDigest, role); err != nil {
		return AgentIdentity{}, err
	}

	// 4. Manifest.
	id := AgentIdentity{
		SchemaVersion:                     ManifestSchemaVersion,
		PrincipalID:                       principal,
		ActorKind:                         closureprotocol.ActorAgent,
		Issuer:                            issuer,
		Roles:                             append([]string(nil), roles...),
		AuthenticationReceiptDigestSHA256: authnDigest,
		RoleAttestationReceiptDigests:     []string{roleDigest},
	}
	if err := writeManifest(root, id); err != nil {
		return AgentIdentity{}, err
	}
	return id, nil
}

// LoadManifest reads the agent manifest under root. The boolean is false (with a
// nil error) when no identity has been enrolled — callers fail soft on that.
func LoadManifest(root string) (AgentIdentity, bool, error) {
	data, err := os.ReadFile(filepath.Join(root, manifestFileName))
	if errors.Is(err, os.ErrNotExist) {
		return AgentIdentity{}, false, nil
	}
	if err != nil {
		return AgentIdentity{}, false, err
	}
	var id AgentIdentity
	if err := yaml.Unmarshal(data, &id); err != nil {
		return AgentIdentity{}, false, err
	}
	return id, true, nil
}

func writeManifest(root string, id AgentIdentity) error {
	data, err := yaml.Marshal(id)
	if err != nil {
		return err
	}
	return writeUnder(root, manifestFileName, data)
}

// storeReceipt writes a receipt as artifacts/sha256/<digest>.yaml so
// LocalBundleResolver.ResolveByDigest can locate it.
func storeReceipt(root, digest string, receipt any) error {
	data, err := yaml.Marshal(receipt)
	if err != nil {
		return err
	}
	return writeUnder(root, filepath.ToSlash(filepath.Join("artifacts", "sha256", digest+".yaml")), data)
}

func writeUnder(root, rel string, data []byte) error {
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func rawSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
