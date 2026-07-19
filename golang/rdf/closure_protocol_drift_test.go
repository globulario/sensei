// SPDX-License-Identifier: Apache-2.0

package rdf

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var ttlDeclRE = regexp.MustCompile(`(?m)^aw:([A-Za-z][A-Za-z0-9]*)\s+a\s+owl:(Class|ObjectProperty|DatatypeProperty)\s*;`)

func TestClosureProtocolClassesExistInOntology(t *testing.T) {
	classes, _, err := loadOntologyTermSets()
	if err != nil {
		t.Fatal(err)
	}
	required := map[string]string{
		"ActorRole":                 ClassActorRole,
		"AuthorityGrant":            ClassAuthorityGrant,
		"DelegationPolicy":          ClassDelegationPolicy,
		"RuntimeTargetKind":         ClassRuntimeTargetKind,
		"EvidenceProfile":           ClassEvidenceProfile,
		"GovernanceException":       ClassGovernanceException,
		"MigrationPlan":             ClassMigrationPlan,
		"CertificationPolicy":       ClassCertificationPolicy,
		"CompletionPolicy":          ClassCompletionPolicy,
		"RevocationPolicy":          ClassRevocationPolicy,
		"Task":                      ClassTask,
		"TaskSession":               ClassTaskSession,
		"RepositorySnapshot":        ClassRepositorySnapshot,
		"GraphSnapshot":             ClassGraphSnapshot,
		"RuntimeTarget":             ClassRuntimeTarget,
		"ActorBinding":              ClassActorBinding,
		"AuthenticationReceipt":     ClassAuthenticationReceipt,
		"RoleAttestationReceipt":    ClassRoleAttestationReceipt,
		"DelegationReceipt":         ClassDelegationReceipt,
		"ClosureAssessment":         ClassClosureAssessment,
		"ClosureBlocker":            ClassClosureBlocker,
		"Abstention":                ClassAbstention,
		"ChangePlan":                ClassChangePlan,
		"ChangeOperation":           ClassChangeOperation,
		"AuthorityResolution":       ClassAuthorityResolution,
		"AdmissionRequest":          ClassAdmissionRequest,
		"AdmissionDecision":         ClassAdmissionDecision,
		"CapabilityConsumption":     ClassCapabilityConsumption,
		"MutationReceipt":           ClassMutationReceipt,
		"ProbeResult":               ClassProbeResult,
		"EvidenceReceipt":           ClassEvidenceReceipt,
		"TestReceipt":               ClassTestReceipt,
		"RuntimeObservationReceipt": ClassRuntimeObservationReceipt,
		"ArtifactReceipt":           ClassArtifactReceipt,
		"ProofDischarge":            ClassProofDischarge,
		"CertificationReceipt":      ClassCertificationReceipt,
		"CompletionReceipt":         ClassCompletionReceipt,
		"WaiverReceipt":             ClassWaiverReceipt,
		"RevocationReceipt":         ClassRevocationReceipt,
		"MigrationExecutionReceipt": ClassMigrationExecutionReceipt,
	}
	for token, iri := range required {
		if classes[token] != iri {
			t.Fatalf("class %s missing or mismatched in ontology", token)
		}
	}
	if classes["RuntimeEvidence"] != ClassRuntimeEvidence {
		t.Fatal("RuntimeEvidence must remain declared for legacy compatibility")
	}
}

func TestClosureProtocolPropertiesExistInOntology(t *testing.T) {
	_, props, err := loadOntologyTermSets()
	if err != nil {
		t.Fatal(err)
	}
	required := map[string]string{
		"bindsToRepositorySnapshot": PropBindsToRepositorySnapshot,
		"bindsToGraphSnapshot":      PropBindsToGraphSnapshot,
		"bindsToRuntimeTarget":      PropBindsToRuntimeTarget,
		"performedBy":               PropPerformedBy,
		"actsUnderRole":             PropActsUnderRole,
		"usesDelegation":            PropUsesDelegation,
		"resolvesAuthorityDomain":   PropResolvesAuthorityDomain,
		"permitsAction":             PropPermitsAction,
		"requiresMutationPath":      PropRequiresMutationPath,
		"requiresObservationPath":   PropRequiresObservationPath,
		"plansOperation":            PropPlansOperation,
		"admitsOperation":           PropAdmitsOperation,
		"consumesAdmission":         PropConsumesAdmission,
		"observesChangeSet":         PropObservesChangeSet,
		"performedVia":              PropPerformedVia,
		"producesArtifact":          PropProducesArtifact,
		"producesEvidenceReceipt":   PropProducesEvidenceReceipt,
		"satisfiesEvidenceProfile":  PropSatisfiesEvidenceProfile,
		"supportsClaim":             PropSupportsClaim,
		"refutesClaim":              PropRefutesClaim,
		"conflictsWithReceipt":      PropConflictsWithReceipt,
		"requiresProofSlot":         PropRequiresProofSlot,
		"dischargesProofSlot":       PropDischargesProofSlot,
		"dischargesProofObligation": PropDischargesProofObligation,
		"certifiesChangeSet":        PropCertifiesChangeSet,
		"certifiedUnderPolicy":      PropCertifiedUnderPolicy,
		"completesTask":             PropCompletesTask,
		"revokesReceipt":            PropRevokesReceipt,
		"supersedesReceipt":         PropSupersedesReceipt,
		"executesMigrationPlan":     PropExecutesMigrationPlan,
		"reopensBecause":            PropReopensBecause,
	}
	for token, iri := range required {
		if props[token] != iri {
			t.Fatalf("property %s missing or mismatched in ontology", token)
		}
	}
}

func TestClosureProtocolNoClassPropertyTypeCollision(t *testing.T) {
	classes, props, err := loadOntologyTermSets()
	if err != nil {
		t.Fatal(err)
	}
	for token := range classes {
		if _, ok := props[token]; ok {
			t.Fatalf("ontology token %s declared as both class and property", token)
		}
	}
}

func loadOntologyTermSets() (map[string]string, map[string]string, error) {
	path := filepath.Join("..", "..", "ontology", "awareness.ttl")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	classes := map[string]string{}
	props := map[string]string{}
	for _, match := range ttlDeclRE.FindAllStringSubmatch(string(data), -1) {
		token := match[1]
		kind := match[2]
		iri := AwNS + token
		switch kind {
		case "Class":
			classes[token] = iri
		case "ObjectProperty", "DatatypeProperty":
			props[token] = iri
		}
	}
	return classes, props, nil
}

func TestClosureProtocolIriShape(t *testing.T) {
	for _, iri := range []string{
		ClassEvidenceProfile,
		ClassCompletionReceipt,
		PropBindsToRepositorySnapshot,
		PropCompletesTask,
	} {
		if !strings.HasPrefix(iri, AwNS) {
			t.Fatalf("expected awareness namespace IRI, got %s", iri)
		}
	}
}
