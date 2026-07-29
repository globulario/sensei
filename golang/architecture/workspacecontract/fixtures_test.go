// SPDX-License-Identifier: AGPL-3.0-only

package workspacecontract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func fixtureRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "docs", "fixtures", "workspace", "v1")
}

func loadFixtureBytes(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureRoot(t), rel))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestValidIdentityFixturesPassSchemaAndProducerValidation proves every
// committed identity fixture passes both the real, canonical JSON Schema
// and this producer's cross-field validation.
func TestValidIdentityFixturesPassSchemaAndProducerValidation(t *testing.T) {
	for _, rel := range []string{"identity/complete.json", "identity/partial.json", "identity/unavailable.json"} {
		t.Run(rel, func(t *testing.T) {
			data := loadFixtureBytes(t, rel)
			if err := ValidateIdentitySchema(data); err != nil {
				t.Fatalf("JSON Schema validation: %v", err)
			}
			var id Identity
			if err := json.Unmarshal(data, &id); err != nil {
				t.Fatal(err)
			}
			if errs := ValidateIdentity(id); len(errs) != 0 {
				t.Fatalf("expected 0 producer-validation errors, got %v", errs)
			}
		})
	}
}

func TestIdentityFixturesMatchTheirLabel(t *testing.T) {
	load := func(rel string) Identity {
		var id Identity
		if err := json.Unmarshal(loadFixtureBytes(t, rel), &id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	complete := load("identity/complete.json")
	if complete.CompositionState != CompositionComplete {
		t.Fatalf("complete.json composition_state = %q, want complete", complete.CompositionState)
	}
	if complete.RepositoryDomainSource != RepositoryDomainConfigured {
		t.Fatalf("complete.json repository_domain_source = %q, want configured", complete.RepositoryDomainSource)
	}
	if complete.GraphAuthority == nil || !complete.GraphAuthority.Authoritative {
		t.Fatal("complete.json must carry an authoritative graph_authority")
	}

	partial := load("identity/partial.json")
	if partial.CompositionState != CompositionPartial {
		t.Fatalf("partial.json composition_state = %q, want partial", partial.CompositionState)
	}

	unavailable := load("identity/unavailable.json")
	if unavailable.CompositionState != CompositionUnavailable {
		t.Fatalf("unavailable.json composition_state = %q, want unavailable", unavailable.CompositionState)
	}
	if unavailable.RepositoryDomainSource != RepositoryDomainUnbound {
		t.Fatalf("unavailable.json repository_domain_source = %q, want unbound", unavailable.RepositoryDomainSource)
	}
	if unavailable.Binding.RepositoryDomain != "" {
		t.Fatalf("unavailable.json binding.repository_domain = %q, want empty (no guessed domain)", unavailable.Binding.RepositoryDomain)
	}
	if unavailable.GraphAuthority != nil {
		t.Fatal("unavailable.json must carry graph_authority: null")
	}
}

// TestValidAdmissionFixturesPassSchemaAndProducerValidation proves every
// committed admission fixture passes both the real, canonical JSON Schema
// and this producer's cross-field validation.
func TestValidAdmissionFixturesPassSchemaAndProducerValidation(t *testing.T) {
	for _, rel := range []string{
		"admission/admitted.json",
		"admission/admitted-with-conditions.json",
		"admission/refused.json",
		"admission/verification-compliant.json",
		"admission/verification-violated.json",
		"admission/verification-stale.json",
	} {
		t.Run(rel, func(t *testing.T) {
			data := loadFixtureBytes(t, rel)
			if err := ValidateAdmissionSchema(data); err != nil {
				t.Fatalf("JSON Schema validation: %v", err)
			}
			var a Admission
			if err := json.Unmarshal(data, &a); err != nil {
				t.Fatal(err)
			}
			if errs := ValidateAdmission(a); len(errs) != 0 {
				t.Fatalf("expected 0 producer-validation errors, got %v", errs)
			}
		})
	}
}

func TestAdmissionFixturesMatchTheirLabel(t *testing.T) {
	load := func(rel string) Admission {
		var a Admission
		if err := json.Unmarshal(loadFixtureBytes(t, rel), &a); err != nil {
			t.Fatal(err)
		}
		return a
	}

	admitted := load("admission/admitted.json")
	if admitted.RecordKind != RecordKindDecision || admitted.Decision != DecisionAdmitted || admitted.Verification != nil {
		t.Fatalf("admitted.json unexpected shape: %+v", admitted)
	}

	withConditions := load("admission/admitted-with-conditions.json")
	if withConditions.Decision != DecisionAdmittedWithConditions {
		t.Fatalf("admitted-with-conditions.json decision = %q, want admitted_with_conditions", withConditions.Decision)
	}

	refused := load("admission/refused.json")
	if refused.Decision != DecisionRefused {
		t.Fatalf("refused.json decision = %q, want refused", refused.Decision)
	}

	compliant := load("admission/verification-compliant.json")
	if compliant.RecordKind != RecordKindVerification || compliant.Verification == nil || compliant.Verification.Status != VerificationScopeCompliant {
		t.Fatalf("verification-compliant.json unexpected shape: %+v", compliant)
	}
	if errs := VerificationBoundToDecision(admitted, compliant); len(errs) != 0 {
		t.Fatalf("expected verification-compliant.json to be bound to admitted.json's decision, got: %v", errs)
	}

	violated := load("admission/verification-violated.json")
	if violated.Verification == nil || violated.Verification.Status != VerificationScopeViolated || len(violated.Verification.Violations) == 0 {
		t.Fatalf("verification-violated.json unexpected shape: %+v", violated)
	}

	stale := load("admission/verification-stale.json")
	if stale.Verification == nil || stale.Verification.Status != VerificationStale {
		t.Fatalf("verification-stale.json unexpected shape: %+v", stale)
	}
}

func TestFixturesDirectoryHasNoUnexpectedContent(t *testing.T) {
	root := fixtureRoot(t)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"identity": true, "admission": true}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !want[e.Name()] {
			t.Errorf("unexpected fixture directory %q; update this test's allowlist if it's intentional", e.Name())
		}
	}
}

// --- Adversarial proof (contract §6) ---

// mutateJSON decodes base, applies mutate to the resulting map, and
// re-encodes it, so adversarial cases start from a known-good instance and
// change exactly one thing.
func mutateJSON(t *testing.T, data []byte, mutate func(map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	mutate(m)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAdversarial_UnknownRootProperty(t *testing.T) {
	base := loadFixtureBytes(t, "identity/complete.json")
	mutated := mutateJSON(t, base, func(m map[string]any) { m["repository_root_identity"] = "/tmp/whatever" })
	if err := ValidateIdentitySchema(mutated); err == nil {
		t.Fatal("expected an unknown root property (repository_root_identity) to fail schema validation")
	}
}

func TestAdversarial_UnknownNestedProperty(t *testing.T) {
	base := loadFixtureBytes(t, "identity/complete.json")
	mutated := mutateJSON(t, base, func(m map[string]any) {
		binding := m["binding"].(map[string]any)
		binding["worktree_id"] = "wt-1"
	})
	if err := ValidateIdentitySchema(mutated); err == nil {
		t.Fatal("expected an unknown nested property (binding.worktree_id) to fail schema validation")
	}
}

func TestAdversarial_MalformedSHA256Digest(t *testing.T) {
	base := loadFixtureBytes(t, "identity/complete.json")
	mutated := mutateJSON(t, base, func(m map[string]any) {
		binding := m["binding"].(map[string]any)
		binding["tree_digest_sha256"] = "not-a-sha256"
	})
	if err := ValidateIdentitySchema(mutated); err == nil {
		t.Fatal("expected a malformed tree_digest_sha256 to fail schema validation")
	}
}

func TestAdversarial_MalformedRepositoryDomainRepresentedAsComplete(t *testing.T) {
	// unbound source must never carry a non-empty domain, even one that
	// looks canonical — the producer's cross-field check catches this
	// because JSON Schema alone cannot express "these two fields agree."
	base := loadFixtureBytes(t, "identity/unavailable.json")
	mutated := mutateJSON(t, base, func(m map[string]any) {
		binding := m["binding"].(map[string]any)
		binding["repository_domain"] = "github.com/globulario/sensei"
	})
	var id Identity
	if err := json.Unmarshal(mutated, &id); err != nil {
		t.Fatal(err)
	}
	errs := ValidateIdentity(id)
	if !hasRule(errs, "unbound_source_carries_domain") {
		t.Fatalf("expected rule unbound_source_carries_domain, got %v", errs)
	}
}

func TestAdversarial_CompleteWithUnresolvedBinding(t *testing.T) {
	base := loadFixtureBytes(t, "identity/complete.json")
	mutated := mutateJSON(t, base, func(m map[string]any) {
		binding := m["binding"].(map[string]any)
		binding["revision_status"] = "unavailable"
		binding["revision"] = nil
	})
	var id Identity
	if err := json.Unmarshal(mutated, &id); err != nil {
		t.Fatal(err)
	}
	errs := ValidateIdentity(id)
	if !hasRule(errs, "composition_state_mismatch") {
		t.Fatalf("expected rule composition_state_mismatch for complete claimed over an unresolved revision, got %v", errs)
	}
}

func TestAdversarial_CompleteWithUnavailableGraphAuthority(t *testing.T) {
	base := loadFixtureBytes(t, "identity/complete.json")
	mutated := mutateJSON(t, base, func(m map[string]any) { m["graph_authority"] = nil })
	var id Identity
	if err := json.Unmarshal(mutated, &id); err != nil {
		t.Fatal(err)
	}
	errs := ValidateIdentity(id)
	if !hasRule(errs, "composition_state_mismatch") {
		t.Fatalf("expected rule composition_state_mismatch for complete claimed with graph_authority: null, got %v", errs)
	}
}

func TestAdversarial_InventedRunnerOwnedFields(t *testing.T) {
	base := loadFixtureBytes(t, "identity/complete.json")
	for _, field := range []string{"server_session", "provider_session", "job_id", "runner_instance_id"} {
		t.Run(field, func(t *testing.T) {
			mutated := mutateJSON(t, base, func(m map[string]any) { m[field] = "invented" })
			if err := ValidateIdentitySchema(mutated); err == nil {
				t.Fatalf("expected invented field %q to fail schema validation", field)
			}
		})
	}
}

func TestAdversarial_UnknownGraphFreshnessEnumValue(t *testing.T) {
	base := loadFixtureBytes(t, "identity/complete.json")
	mutated := mutateJSON(t, base, func(m map[string]any) {
		ga := m["graph_authority"].(map[string]any)
		ga["graph_freshness_state"] = "GRAPH_FRESHNESS_STATE_MADE_UP"
	})
	if err := ValidateIdentitySchema(mutated); err == nil {
		t.Fatal("expected an unknown graph_freshness_state enum value to fail schema validation")
	}
}

func TestAdversarial_UnknownAdmissionOutcomeEnumValue(t *testing.T) {
	base := loadFixtureBytes(t, "admission/admitted.json")
	mutated := mutateJSON(t, base, func(m map[string]any) { m["decision"] = "approved" })
	if err := ValidateAdmissionSchema(mutated); err == nil {
		t.Fatal("expected an unknown decision enum value to fail schema validation")
	}
}

func TestAdversarial_UnknownVerificationStatusEnumValue(t *testing.T) {
	base := loadFixtureBytes(t, "admission/verification-compliant.json")
	mutated := mutateJSON(t, base, func(m map[string]any) {
		v := m["verification"].(map[string]any)
		v["status"] = "looks_fine"
	})
	if err := ValidateAdmissionSchema(mutated); err == nil {
		t.Fatal("expected an unknown verification status enum value to fail schema validation")
	}
}

func TestAdversarial_DecisionRecordWithNonNullVerification(t *testing.T) {
	decision := loadFixtureBytes(t, "admission/admitted.json")
	compliant := loadFixtureBytes(t, "admission/verification-compliant.json")
	var compliantMap map[string]any
	if err := json.Unmarshal(compliant, &compliantMap); err != nil {
		t.Fatal(err)
	}
	mutated := mutateJSON(t, decision, func(m map[string]any) { m["verification"] = compliantMap["verification"] })
	if err := ValidateAdmissionSchema(mutated); err == nil {
		t.Fatal("expected a decision record with non-null verification to fail schema validation")
	}
}

func TestAdversarial_VerificationRecordWithNullVerification(t *testing.T) {
	base := loadFixtureBytes(t, "admission/verification-compliant.json")
	mutated := mutateJSON(t, base, func(m map[string]any) { m["verification"] = nil })
	if err := ValidateAdmissionSchema(mutated); err == nil {
		t.Fatal("expected a verification record with null verification to fail schema validation")
	}
}

func TestAdversarial_MismatchedDecisionAndVerificationIdentity(t *testing.T) {
	decision := loadFixtureBytes(t, "admission/admitted.json")
	verification := loadFixtureBytes(t, "admission/verification-compliant.json")
	var d, v Admission
	if err := json.Unmarshal(decision, &d); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(verification, &v); err != nil {
		t.Fatal(err)
	}
	v.AdmissionID = "admission.someone-elses-decision"
	if errs := VerificationBoundToDecision(d, v); !hasRule(errs, "admission_id_mismatch") {
		t.Fatalf("expected rule admission_id_mismatch, got %v", errs)
	}

	v2 := v
	v2.AdmissionID = d.AdmissionID
	v2.DecisionDigestSHA256 = "9999999999999999999999999999999999999999999999999999999999999999"
	if errs := VerificationBoundToDecision(d, v2); !hasRule(errs, "decision_digest_mismatch") {
		t.Fatalf("expected rule decision_digest_mismatch, got %v", errs)
	}

	v3 := v
	v3.AdmissionID = d.AdmissionID
	v3.DecisionDigestSHA256 = d.DecisionDigestSHA256
	v3.Binding.RepositoryDomain = "github.com/someone/else"
	if errs := VerificationBoundToDecision(d, v3); !hasRule(errs, "binding_mismatch") {
		t.Fatalf("expected rule binding_mismatch, got %v", errs)
	}
}

func TestAdversarial_ScopeCompliantNeverManufacturesCorrectnessCertified(t *testing.T) {
	// ProjectVerification must copy CorrectnessCertified verbatim from the
	// owner's admission.Verification — never set it true merely because
	// Status is scope_compliant.
	var compliant Admission
	if err := json.Unmarshal(loadFixtureBytes(t, "admission/verification-compliant.json"), &compliant); err != nil {
		t.Fatal(err)
	}
	if compliant.Verification.Status != VerificationScopeCompliant {
		t.Fatalf("fixture setup error: expected scope_compliant, got %q", compliant.Verification.Status)
	}
	if compliant.Verification.CorrectnessCertified {
		t.Fatal("scope_compliant fixture must not carry correctness_certified: true unless the owner actually reported it")
	}
}

func TestAdversarial_MissingDecisionOrPolicyIdentity(t *testing.T) {
	base := loadFixtureBytes(t, "admission/admitted.json")
	for _, field := range []string{"admission_id", "policy_id"} {
		t.Run(field, func(t *testing.T) {
			mutated := mutateJSON(t, base, func(m map[string]any) { m[field] = "" })
			var a Admission
			if err := json.Unmarshal(mutated, &a); err != nil {
				t.Fatal(err)
			}
			errs := ValidateAdmission(a)
			wantRule := "missing_" + field
			if !hasRule(errs, wantRule) {
				t.Fatalf("expected rule %q, got %v", wantRule, errs)
			}
		})
	}
}

func hasRule(errs []ValidationError, rule string) bool {
	for _, e := range errs {
		if e.Rule == rule {
			return true
		}
	}
	return false
}
