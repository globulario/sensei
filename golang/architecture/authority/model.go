// SPDX-License-Identifier: AGPL-3.0-only

package authority

import "github.com/globulario/sensei/golang/architecture/closureprotocol"

type ActorBinding = closureprotocol.ActorBinding
type ChangeOperation = closureprotocol.ChangeOperation
type AuthorityResolution = closureprotocol.AuthorityResolution
type AuthorityResolutionOperation = closureprotocol.AuthorityResolutionOperation

type ActorRole struct {
	ID                string
	Status            string
	AllowedActorKinds []closureprotocol.ActorKind
	TrustedIssuers    []string
	Aliases           []string
}

type MutationPath struct {
	ID            string
	Status        string
	MechanismKind closureprotocol.MechanismKind
	TargetKinds   []string
}

type ObservationPath struct {
	ID            string
	Status        string
	MechanismKind closureprotocol.MechanismKind
	TargetKinds   []string
}

type DelegationPolicy struct {
	ID                  string
	Status              string
	MaximumDepth        int
	MaximumDuration     string
	AllowSubdelegation  bool
	AllowedActions      []closureprotocol.OperationKind
	AllowedMechanismIDs []string
}

type AuthorityGrant struct {
	ID                   string
	Status               string
	ActorRoleIDs         []string
	AuthorityDomainIDs   []string
	Actions              []closureprotocol.OperationKind
	TargetKinds          []string
	RequiredMechanismIDs []string
	MaximumRiskClass     string
	ValidFrom            string
	ValidUntil           string
	Delegable            bool
	DelegationPolicyID   string
}

type AuthorityDomain struct {
	ID                  string
	Status              string
	MayWriteRoleIDs     []string
	MayReadRoleIDs      []string
	MustMutateViaIDs    []string
	MustReadViaIDs      []string
	ObservesViaIDs      []string
	LegacyMayWrite      []string
	LegacyMayRead       []string
	LegacyMustMutateVia []string
	LegacyMustReadVia   []string
	LegacyObservesVia   []string
}

type PolicyIndex struct {
	ActorRoles         map[string]ActorRole
	MutationPaths      map[string]MutationPath
	ObservationPaths   map[string]ObservationPath
	DelegationPolicies map[string]DelegationPolicy
	AuthorityGrants    map[string]AuthorityGrant
	AuthorityDomains   map[string]AuthorityDomain
}

type VerifiedActor struct {
	PrincipalID                       string
	ActorKind                         closureprotocol.ActorKind
	Issuer                            string
	Status                            closureprotocol.ReceiptStatus
	AuthenticationReceiptDigestSHA256 string
	VerifiedRoleIDs                   []string
	DelegationReceipts                []closureprotocol.DelegationReceipt
	Limitations                       []string
}

type AuthorityApplicability struct {
	OperationID                 string
	TargetFile                  string
	TargetSymbol                string
	AuthorityDomainIDs          []string
	RequiredRuntimeMechanismIDs []string
	RelationPaths               [][]string
}

type ResolveRequest struct {
	ActorBindingDigestSHA256         string
	BaseBindingDigestSHA256          string
	ClosureAssessmentDigestSHA256    string
	Actor                            VerifiedActor
	Operations                       []closureprotocol.ChangeOperation
	Applicability                    []AuthorityApplicability
	PolicyID                         string
	EvaluatedAt                      string
	AuthorityPolicyGraphDigestSHA256 string
}

type ArtifactResolver interface {
	ResolveByDigest(digest string) ([]byte, error)
	ResolveArtifact(ref closureprotocol.LedgerPayloadRef) ([]byte, error)
}
