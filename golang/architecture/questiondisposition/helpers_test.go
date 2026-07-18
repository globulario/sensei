// SPDX-License-Identifier: Apache-2.0

package questiondisposition_test

import (
	"testing"

	yaml "gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

func ledgerHead(taskDir string) (string, error) {
	return admission.TaskLedgerHead(taskDir)
}

func verifiedChain(t *testing.T, taskDir string) ledger.VerifiedChain {
	t.Helper()
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(func(et closureprotocol.LedgerEventType, mt string, data []byte) error {
		return ledger.ValidateTaskEventPayload(et, data)
	}))
	chain, err := store.VerifyChain()
	if err != nil {
		t.Fatalf("verify chain: %v", err)
	}
	return chain
}

func countDispositionEvents(t *testing.T, taskDir string) int {
	t.Helper()
	n := 0
	for _, ve := range verifiedChain(t, taskDir).Entries {
		if ve.Entry.EventType == closureprotocol.LedgerEventQuestionDispositionRecorded {
			n++
		}
	}
	return n
}

func hasEventType(t *testing.T, taskDir, eventType string) bool {
	t.Helper()
	for _, ve := range verifiedChain(t, taskDir).Entries {
		if string(ve.Entry.EventType) == eventType {
			return true
		}
	}
	return false
}

// assertDispositionEventShape proves the disposition event records an outcome
// only — it never advances the task lifecycle.
func assertDispositionEventShape(t *testing.T, taskDir string) {
	t.Helper()
	for _, ve := range verifiedChain(t, taskDir).Entries {
		if ve.Entry.EventType != closureprotocol.LedgerEventQuestionDispositionRecorded {
			continue
		}
		data, err := ledger.ReadVerifiedPayload(ve)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := ledger.ParseTaskEventPayload(data)
		if err != nil {
			t.Fatal(err)
		}
		if payload.TaskPhase != "" || payload.Status != "" || payload.ResultBinding != nil {
			t.Fatalf("disposition event advanced lifecycle: phase=%q status=%q result=%v",
				payload.TaskPhase, payload.Status, payload.ResultBinding)
		}
		return
	}
	t.Fatal("no disposition event found")
}

// stripGrant removes one grant id from an authority_grants.yaml document.
func stripGrant(t *testing.T, data []byte, grantID string) []byte {
	t.Helper()
	var doc struct {
		AuthorityGrants []map[string]any `yaml:"authority_grants"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal grants: %v", err)
	}
	kept := doc.AuthorityGrants[:0]
	for _, g := range doc.AuthorityGrants {
		if id, _ := g["id"].(string); id == grantID {
			continue
		}
		kept = append(kept, g)
	}
	doc.AuthorityGrants = kept
	out, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal grants: %v", err)
	}
	return out
}
