// SPDX-License-Identifier: Apache-2.0

package closureprotocol

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type fixtureBundleEnvelope struct {
	ClosureProtocolFixture fixtureBundle `yaml:"closure_protocol_fixture"`
}

type fixtureBundle struct {
	Name             string            `yaml:"name"`
	CompletionPolicy CompletionPolicy  `yaml:"completion_policy"`
	Dimensions       []DimensionResult `yaml:"dimensions"`
	Expected         fixtureExpected   `yaml:"expected"`
	Records          fixtureRecords    `yaml:"records"`
}

type fixtureExpected struct {
	TerminallyClosed        bool   `yaml:"terminally_closed"`
	TerminalStatus          string `yaml:"terminal_status,omitempty"`
	BlockingDimension       string `yaml:"blocking_dimension,omitempty"`
	ValidationErrorContains string `yaml:"validation_error_contains,omitempty"`
}

type fixtureRecords struct {
	ActorBinding              *ActorBinding              `yaml:"actor_binding,omitempty"`
	ChangePlan                *ChangePlan                `yaml:"change_plan,omitempty"`
	AuthorityResolution       *AuthorityResolution       `yaml:"authority_resolution,omitempty"`
	AdmissionRequest          *AdmissionRequest          `yaml:"admission_request,omitempty"`
	AdmissionDecision         *AdmissionDecision         `yaml:"admission_decision,omitempty"`
	CapabilityConsumption     *CapabilityConsumption     `yaml:"capability_consumption,omitempty"`
	EvidenceProfile           *EvidenceProfile           `yaml:"evidence_profile,omitempty"`
	EvidenceReceipt           *EvidenceReceipt           `yaml:"evidence_receipt,omitempty"`
	ProofDischarge            *ProofDischarge            `yaml:"proof_discharge,omitempty"`
	CertificationReceipt      *CertificationReceipt      `yaml:"certification_receipt,omitempty"`
	CompletionReceipt         *CompletionReceipt         `yaml:"completion_receipt,omitempty"`
	WaiverReceipt             *WaiverReceipt             `yaml:"waiver_receipt,omitempty"`
	RevocationReceipt         *RevocationReceipt         `yaml:"revocation_receipt,omitempty"`
	MigrationExecutionReceipt *MigrationExecutionReceipt `yaml:"migration_execution_receipt,omitempty"`
	ResultTransitionReceipt   *ResultTransitionReceipt   `yaml:"result_transition_receipt,omitempty"`
}

func TestFixtures(t *testing.T) {
	root := filepath.Join("..", "..", "..", "docs", "fixtures", "architectural-closure", "v1")
	paths := []string{
		"completed/bundle.yaml",
		"blocked/bundle.yaml",
		"stale/bundle.yaml",
		"uncertifiable/bundle.yaml",
		"completed-with-exception/bundle.yaml",
		"revoked/bundle.yaml",
		"migration-in-progress/bundle.yaml",
		"result-transition/bundle.yaml",
	}
	for _, rel := range paths {
		t.Run(rel, func(t *testing.T) {
			fix := loadFixture(t, filepath.Join(root, rel))
			validateFixtureRecords(t, fix)
			eval := EvaluateClosure(fix.Dimensions, fix.CompletionPolicy)
			if eval.TerminallyClosed != fix.Expected.TerminallyClosed {
				t.Fatalf("terminally_closed mismatch: got %v want %v", eval.TerminallyClosed, fix.Expected.TerminallyClosed)
			}
			if fix.Expected.BlockingDimension != "" && eval.TerminallyClosed {
				t.Fatalf("expected blocking dimension %s", fix.Expected.BlockingDimension)
			}
		})
	}
	invalids, err := filepath.Glob(filepath.Join(root, "invalid", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range invalids {
		t.Run(filepath.Base(path), func(t *testing.T) {
			fix := loadFixture(t, path)
			err := validateFixtureError(fix)
			if err == nil || !strings.Contains(err.Error(), fix.Expected.ValidationErrorContains) {
				t.Fatalf("expected error containing %q, got %v", fix.Expected.ValidationErrorContains, err)
			}
		})
	}
}

func loadFixture(t *testing.T, path string) fixtureBundle {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var env fixtureBundleEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	return env.ClosureProtocolFixture
}

func validateFixtureRecords(t *testing.T, fix fixtureBundle) {
	t.Helper()
	if fix.Records.ActorBinding != nil {
		if err := ValidateActorBinding(*fix.Records.ActorBinding); err != nil {
			t.Fatal(err)
		}
	}
	if fix.Records.ChangePlan != nil {
		if err := ValidateChangePlan(*fix.Records.ChangePlan); err != nil {
			t.Fatal(err)
		}
	}
	if fix.Records.AuthorityResolution != nil {
		if err := ValidateAuthorityResolution(*fix.Records.AuthorityResolution); err != nil {
			t.Fatal(err)
		}
	}
	if fix.Records.AdmissionRequest != nil {
		if err := ValidateAdmissionRequest(*fix.Records.AdmissionRequest); err != nil {
			t.Fatal(err)
		}
	}
	if fix.Records.AdmissionDecision != nil {
		if err := ValidateAdmissionDecision(*fix.Records.AdmissionDecision); err != nil {
			t.Fatal(err)
		}
	}
	if fix.Records.CapabilityConsumption != nil {
		if err := ValidateCapabilityConsumption(*fix.Records.CapabilityConsumption); err != nil {
			t.Fatal(err)
		}
	}
	if fix.Records.EvidenceProfile != nil {
		if err := ValidateEvidenceProfile(*fix.Records.EvidenceProfile); err != nil {
			t.Fatal(err)
		}
	}
	if fix.Records.EvidenceReceipt != nil {
		if err := ValidateEvidenceReceipt(*fix.Records.EvidenceReceipt); err != nil {
			t.Fatal(err)
		}
		if fix.Records.EvidenceProfile != nil {
			if err := ValidateEvidenceReceiptAgainstProfile(*fix.Records.EvidenceProfile, *fix.Records.EvidenceReceipt); err != nil {
				t.Fatal(err)
			}
		}
	}
	if fix.Records.ProofDischarge != nil {
		if err := ValidateProofDischarge(*fix.Records.ProofDischarge); err != nil {
			t.Fatal(err)
		}
	}
	if fix.Records.CertificationReceipt != nil {
		if err := ValidateCertificationReceipt(*fix.Records.CertificationReceipt); err != nil {
			t.Fatal(err)
		}
	}
	if fix.Records.CompletionReceipt != nil {
		if err := ValidateCompletionReceipt(*fix.Records.CompletionReceipt); err != nil {
			t.Fatal(err)
		}
		if fix.Records.WaiverReceipt != nil {
			if err := ValidateCompletionWaivers(*fix.Records.CompletionReceipt, []WaiverReceipt{*fix.Records.WaiverReceipt}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if fix.Records.WaiverReceipt != nil {
		if err := ValidateWaiverReceipt(*fix.Records.WaiverReceipt); err != nil {
			t.Fatal(err)
		}
	}
	if fix.Records.RevocationReceipt != nil {
		if err := ValidateRevocationReceipt(*fix.Records.RevocationReceipt); err != nil {
			t.Fatal(err)
		}
	}
	if fix.Records.MigrationExecutionReceipt != nil {
		if err := ValidateMigrationExecutionReceipt(*fix.Records.MigrationExecutionReceipt); err != nil {
			t.Fatal(err)
		}
	}
	if fix.Records.ResultTransitionReceipt != nil {
		if err := ValidateResultTransitionReceipt(*fix.Records.ResultTransitionReceipt); err != nil {
			t.Fatal(err)
		}
	}
}

func validateFixtureError(fix fixtureBundle) error {
	if fix.Records.ActorBinding != nil {
		if err := ValidateActorBinding(*fix.Records.ActorBinding); err != nil {
			return err
		}
	}
	if fix.Records.ChangePlan != nil {
		if err := ValidateChangePlan(*fix.Records.ChangePlan); err != nil {
			return err
		}
	}
	if fix.Records.AdmissionRequest != nil {
		if err := ValidateAdmissionRequest(*fix.Records.AdmissionRequest); err != nil {
			return err
		}
	}
	if fix.Records.CapabilityConsumption != nil {
		if err := ValidateCapabilityConsumption(*fix.Records.CapabilityConsumption); err != nil {
			return err
		}
	}
	if fix.Records.EvidenceProfile != nil {
		if err := ValidateEvidenceProfile(*fix.Records.EvidenceProfile); err != nil {
			return err
		}
	}
	if fix.Records.EvidenceReceipt != nil {
		if err := ValidateEvidenceReceipt(*fix.Records.EvidenceReceipt); err != nil {
			return err
		}
		if fix.Records.EvidenceProfile != nil {
			if err := ValidateEvidenceReceiptAgainstProfile(*fix.Records.EvidenceProfile, *fix.Records.EvidenceReceipt); err != nil {
				return err
			}
		}
	}
	if fix.Records.ProofDischarge != nil {
		if err := ValidateProofDischarge(*fix.Records.ProofDischarge); err != nil {
			return err
		}
	}
	if fix.Records.CompletionReceipt != nil {
		if err := ValidateCompletionReceipt(*fix.Records.CompletionReceipt); err != nil {
			return err
		}
		if fix.Records.WaiverReceipt != nil {
			if err := ValidateWaiverReceipt(*fix.Records.WaiverReceipt); err != nil {
				return err
			}
			if err := ValidateCompletionWaivers(*fix.Records.CompletionReceipt, []WaiverReceipt{*fix.Records.WaiverReceipt}); err != nil {
				return err
			}
		}
	}
	if fix.Records.RevocationReceipt != nil {
		if err := ValidateRevocationReceipt(*fix.Records.RevocationReceipt); err != nil {
			return err
		}
	}
	if fix.Records.ResultTransitionReceipt != nil {
		if err := ValidateResultTransitionReceipt(*fix.Records.ResultTransitionReceipt); err != nil {
			return err
		}
	}
	return nil
}
