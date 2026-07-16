// SPDX-License-Identifier: AGPL-3.0-only

package admission

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// Phase 3 admission v2, read side: load the typed records that the admission
// recorders wrote onto the task ledger, so a command can operate on verified
// task records rather than caller-supplied flags.

func admissionValidator(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	return ledger.ValidateTaskEventPayload(eventType, data)
}

// TaskLedgerHead returns the current head digest of a task ledger, for
// expected-head protection.
func TaskLedgerHead(taskDir string) (string, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(admissionValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		return "", err
	}
	return chain.Head.EntryDigestSHA256, nil
}

// LoadLatestArtifact loads the JSON artifact named artifactKey from the most
// recent ledger event of eventType into out. It fails closed when the event is
// absent or the artifact is missing.
func LoadLatestArtifact(taskDir string, eventType closureprotocol.LedgerEventType, artifactKey string, out any) error {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(admissionValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		return err
	}
	for i := len(chain.Entries) - 1; i >= 0; i-- {
		ve := chain.Entries[i]
		if ve.Entry.EventType != eventType {
			continue
		}
		data, err := os.ReadFile(ve.PayloadPath)
		if err != nil {
			return err
		}
		payload, err := ledger.ParseTaskEventPayload(data)
		if err != nil {
			return err
		}
		ref, ok := payload.Artifacts[artifactKey]
		if !ok {
			return fmt.Errorf("ledger event %s has no artifact %q", eventType, artifactKey)
		}
		artifactData, err := os.ReadFile(filepath.Join(taskDir, filepath.FromSlash(ref.Path)))
		if err != nil {
			return err
		}
		return json.Unmarshal(artifactData, out)
	}
	return fmt.Errorf("no %s event found in task ledger", eventType)
}

// LoadTaskBaseBinding returns the base binding recorded on the task_prepared
// event (stored inline in the payload, not as an artifact).
func LoadTaskBaseBinding(taskDir string) (closureprotocol.BaseBinding, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(admissionValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		return closureprotocol.BaseBinding{}, err
	}
	for i := len(chain.Entries) - 1; i >= 0; i-- {
		ve := chain.Entries[i]
		if ve.Entry.EventType != closureprotocol.LedgerEventTaskPrepared {
			continue
		}
		data, err := os.ReadFile(ve.PayloadPath)
		if err != nil {
			return closureprotocol.BaseBinding{}, err
		}
		payload, err := ledger.ParseTaskEventPayload(data)
		if err != nil {
			return closureprotocol.BaseBinding{}, err
		}
		if payload.BaseBinding == nil {
			return closureprotocol.BaseBinding{}, fmt.Errorf("task_prepared event carries no base binding")
		}
		return *payload.BaseBinding, nil
	}
	return closureprotocol.BaseBinding{}, fmt.Errorf("no task_prepared event found in task ledger")
}

// RecordedAuthority is the bundle an authority_resolved event carries.
type RecordedAuthority struct {
	Resolution closureprotocol.AuthorityResolution
	Actor      closureprotocol.ActorBinding
	ChangePlan closureprotocol.ChangePlan
	Base       closureprotocol.BaseBinding
}

// LoadRecordedAuthority loads the latest authority_resolved bundle.
func LoadRecordedAuthority(taskDir string) (RecordedAuthority, error) {
	var out RecordedAuthority
	for key, dst := range map[string]any{
		"authority_resolution": &out.Resolution,
		"actor_binding":        &out.Actor,
		"change_plan":          &out.ChangePlan,
		"base_binding":         &out.Base,
	} {
		if err := LoadLatestArtifact(taskDir, closureprotocol.LedgerEventAuthorityResolved, key, dst); err != nil {
			return RecordedAuthority{}, err
		}
	}
	return out, nil
}

// LoadRecordedDecision loads the latest admission_decided decision.
func LoadRecordedDecision(taskDir string) (closureprotocol.AdmissionDecision, error) {
	var d closureprotocol.AdmissionDecision
	err := LoadLatestArtifact(taskDir, closureprotocol.LedgerEventAdmissionDecided, "admission_decision", &d)
	return d, err
}

// LoadRecordedConsumption loads the latest admission_consumed consumption.
func LoadRecordedConsumption(taskDir string) (closureprotocol.CapabilityConsumption, error) {
	var c closureprotocol.CapabilityConsumption
	err := LoadLatestArtifact(taskDir, closureprotocol.LedgerEventAdmissionConsumed, "capability_consumption", &c)
	return c, err
}
