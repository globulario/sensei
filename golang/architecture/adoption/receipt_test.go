// SPDX-License-Identifier: AGPL-3.0-only

package adoption_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/adoption"
	"gopkg.in/yaml.v3"
)

func completeReceipt() adoption.Receipt {
	return adoption.Receipt{
		Status:              "machine_adopted",
		PromotionStatus:     "machine_adopted",
		AssertionOrigin:     "history_inferred",
		EpistemicStatus:     "supported",
		ArchitecturalPlane:  "historical",
		DecisionActor:       "sensei.knowledge_adoption",
		DecisionContext:     "project_reconstruction",
		DecisionPolicy:      "adoption.invariant.corroborated.v1",
		DecisionTimestamp:   "2026-07-14T05:00:00-04:00",
		ValidForRevision:    "34dac209ffb6ef85cc78c5d217bbb7ad001d68fd",
		ValidForGraphDigest: strings.Repeat("a", 64),
		ReviewStatus:        "not_human_reviewed",
	}
}

func TestAdoptionReceiptIsReusableAcrossKnowledgeClasses(t *testing.T) {
	type invariant struct {
		ID               string `yaml:"id"`
		adoption.Receipt `yaml:",inline"`
	}
	type decision struct {
		ID               string `yaml:"id"`
		adoption.Receipt `yaml:",inline"`
	}
	for _, value := range []any{
		invariant{ID: "invariant.writer_monotonic", Receipt: completeReceipt()},
		decision{ID: "decision.writer_path", Receipt: completeReceipt()},
	} {
		raw, err := yaml.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "decision_policy: adoption.invariant.corroborated.v1") {
			t.Fatalf("shared receipt did not serialize inline:\n%s", raw)
		}
	}
}

func TestAdoptionReceiptRequiresActorContextPolicy(t *testing.T) {
	r := completeReceipt()
	r.DecisionActor, r.DecisionContext, r.DecisionPolicy = "", "", ""
	err := adoption.ValidateMachineAdoption(r)
	if err == nil || !strings.Contains(err.Error(), "decision_actor") || !strings.Contains(err.Error(), "decision_context") || !strings.Contains(err.Error(), "decision_policy") {
		t.Fatalf("expected actor/context/policy errors, got %v", err)
	}
}

func TestAdoptionReceiptRequiresRevisionAndGraphBinding(t *testing.T) {
	r := completeReceipt()
	r.ValidForRevision, r.ValidForGraphDigest = "", ""
	err := adoption.ValidateMachineAdoption(r)
	if err == nil || !strings.Contains(err.Error(), "valid_for_revision") || !strings.Contains(err.Error(), "valid_for_graph_digest") {
		t.Fatalf("expected snapshot binding errors, got %v", err)
	}
}

func TestAdoptionReceiptPreservesReviewStatus(t *testing.T) {
	got := adoption.Normalize(completeReceipt())
	if got.ReviewStatus != "not_human_reviewed" {
		t.Fatalf("review status=%q", got.ReviewStatus)
	}
}

func TestAdoptionReceiptDoesNotAffectStableKnowledgeID(t *testing.T) {
	type knowledge struct {
		ID      string
		Receipt adoption.Receipt
	}
	before := knowledge{ID: "invariant.writer_monotonic", Receipt: completeReceipt()}
	after := before
	after.Receipt.ReviewStatus = "human_reviewed"
	if before.ID != after.ID {
		t.Fatalf("receipt review changed stable ID: %q != %q", before.ID, after.ID)
	}
}

func TestAdoptionReceiptIsDeterministicWithExplicitTimestamp(t *testing.T) {
	r := completeReceipt()
	first := adoption.Normalize(r)
	second := adoption.Normalize(r)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("normalization is not deterministic:\nfirst=%+v\nsecond=%+v", first, second)
	}
	if first.DecisionTimestamp != "2026-07-14T09:00:00Z" {
		t.Fatalf("timestamp=%q, want canonical UTC", first.DecisionTimestamp)
	}
}
