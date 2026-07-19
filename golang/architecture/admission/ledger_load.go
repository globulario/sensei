// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"context"
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

// LoadLatestArtifactOptional loads the JSON artifact named artifactKey from the
// most recent ledger event of eventType into out, reporting whether it was
// present. It fails closed on real read/parse errors and when no event of
// eventType exists, but a latest event that simply omits artifactKey yields
// (false, nil) — for artifacts that are only written on some code paths.
func LoadLatestArtifactOptional(taskDir string, eventType closureprotocol.LedgerEventType, artifactKey string, out any) (bool, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(admissionValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		return false, err
	}
	return latestArtifactFromChain(taskDir, chain, eventType, artifactKey, out)
}

// latestArtifactFromChain reads a named JSON artifact from the most recent event of
// eventType in an already-verified chain, without re-verifying it. It is the shared
// core of the artifact loaders, so a caller that needs several artifacts from one
// chain (LoadRecordedAuthority) can verify the ledger once and read each artifact
// from the same verified snapshot instead of reopening the world per artifact. A
// latest event that omits artifactKey yields (false, nil); an absent event fails
// closed with the same error the per-artifact loaders return.
func latestArtifactFromChain(taskDir string, chain ledger.VerifiedChain, eventType closureprotocol.LedgerEventType, artifactKey string, out any) (bool, error) {
	for i := len(chain.Entries) - 1; i >= 0; i-- {
		ve := chain.Entries[i]
		if ve.Entry.EventType != eventType {
			continue
		}
		data, err := os.ReadFile(ve.PayloadPath)
		if err != nil {
			return false, err
		}
		payload, err := ledger.ParseTaskEventPayload(data)
		if err != nil {
			return false, err
		}
		ref, ok := payload.Artifacts[artifactKey]
		if !ok {
			return false, nil
		}
		artifactData, err := os.ReadFile(filepath.Join(taskDir, filepath.FromSlash(ref.Path)))
		if err != nil {
			return false, err
		}
		if err := json.Unmarshal(artifactData, out); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, fmt.Errorf("no %s event found in task ledger", eventType)
}

// LoadLatestArtifactBytes returns the raw bytes of the artifact named artifactKey
// from the most recent ledger event of eventType, after verifying the ledger
// chain. Unlike LoadLatestArtifact it does not assume JSON, so a caller can decode
// a YAML-serialized artifact (e.g. the closure request). It fails closed when the
// event is absent; a latest event that omits artifactKey yields (nil, false, nil).
func LoadLatestArtifactBytes(taskDir string, eventType closureprotocol.LedgerEventType, artifactKey string) ([]byte, bool, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(admissionValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		return nil, false, err
	}
	for i := len(chain.Entries) - 1; i >= 0; i-- {
		ve := chain.Entries[i]
		if ve.Entry.EventType != eventType {
			continue
		}
		data, err := os.ReadFile(ve.PayloadPath)
		if err != nil {
			return nil, false, err
		}
		payload, err := ledger.ParseTaskEventPayload(data)
		if err != nil {
			return nil, false, err
		}
		ref, ok := payload.Artifacts[artifactKey]
		if !ok {
			return nil, false, nil
		}
		artifactData, err := os.ReadFile(filepath.Join(taskDir, filepath.FromSlash(ref.Path)))
		if err != nil {
			return nil, false, err
		}
		return artifactData, true, nil
	}
	return nil, false, fmt.Errorf("no %s event found in task ledger", eventType)
}

// LoadLatestArtifact loads the JSON artifact named artifactKey from the most
// recent ledger event of eventType into out. It fails closed when the event is
// absent or the artifact is missing.
func LoadLatestArtifact(taskDir string, eventType closureprotocol.LedgerEventType, artifactKey string, out any) error {
	found, err := LoadLatestArtifactOptional(taskDir, eventType, artifactKey, out)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("ledger event %s has no artifact %q", eventType, artifactKey)
	}
	return nil
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

// RecordedAuthority is the bundle an authority_resolved event carries. When the
// resolution consumed delegated authority, DelegationReceipts holds the concrete
// delegation records the resolver verified, so consumers can independently
// re-verify the chain rather than trust the resolution's claimed delegation_chain.
type RecordedAuthority struct {
	Resolution         closureprotocol.AuthorityResolution
	Actor              closureprotocol.ActorBinding
	ChangePlan         closureprotocol.ChangePlan
	Base               closureprotocol.BaseBinding
	DelegationReceipts []closureprotocol.DelegationReceipt
}

// LoadRecordedAuthority loads the latest authority_resolved bundle. The
// delegation receipts are loaded only when the event recorded them (delegated
// resolutions); a non-delegated resolution leaves DelegationReceipts nil.
func LoadRecordedAuthority(taskDir string) (RecordedAuthority, error) {
	return LoadRecordedAuthorityCtx(context.Background(), taskDir)
}

// LoadRecordedAuthorityCtx loads the latest authority_resolved bundle, verifying the
// task ledger exactly once and reading all five artifacts from that single verified
// chain instead of re-verifying per artifact. When ctx carries an evaluation scope
// (ledger.WithVerificationScope) the verification participates in that scope's digest
// memo. The result is byte-for-byte identical to loading each artifact separately:
// the four core artifacts fail closed when absent, and delegation receipts are read
// only when the event recorded them.
func LoadRecordedAuthorityCtx(ctx context.Context, taskDir string) (RecordedAuthority, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(admissionValidator))
	chain, err := store.VerifyChainCtx(ctx)
	if err != nil {
		return RecordedAuthority{}, err
	}
	var out RecordedAuthority
	required := []struct {
		key string
		dst any
	}{
		{"authority_resolution", &out.Resolution},
		{"actor_binding", &out.Actor},
		{"change_plan", &out.ChangePlan},
		{"base_binding", &out.Base},
	}
	for _, r := range required {
		found, err := latestArtifactFromChain(taskDir, chain, closureprotocol.LedgerEventAuthorityResolved, r.key, r.dst)
		if err != nil {
			return RecordedAuthority{}, err
		}
		if !found {
			return RecordedAuthority{}, fmt.Errorf("ledger event %s has no artifact %q", closureprotocol.LedgerEventAuthorityResolved, r.key)
		}
	}
	if _, err := latestArtifactFromChain(taskDir, chain, closureprotocol.LedgerEventAuthorityResolved, "delegation_receipts", &out.DelegationReceipts); err != nil {
		return RecordedAuthority{}, err
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

// LoadRecordedObservedChange loads the latest change_observed observed change
// set. It fails closed when no change_observed event has been recorded.
func LoadRecordedObservedChange(taskDir string) (ObservedChangeSet, error) {
	var c ObservedChangeSet
	err := LoadLatestArtifact(taskDir, closureprotocol.LedgerEventChangeObserved, "observed_change_set", &c)
	return c, err
}

// LoadRecordedScopeVerification loads the latest scope_verified verification.
func LoadRecordedScopeVerification(taskDir string) (ScopeVerification, error) {
	var v ScopeVerification
	err := LoadLatestArtifact(taskDir, closureprotocol.LedgerEventScopeVerified, "scope_verification", &v)
	return v, err
}

// LoadEventProducedAt returns the RFC3339 produced-at timestamp recorded on the
// latest ledger event of eventType. It is the stable, ledger-anchored clock a
// deterministic downstream computation (e.g. the result pipeline) uses instead of
// time.Now, so two executions for the same task result derive the same time. It
// fails closed when no such event exists.
func LoadEventProducedAt(taskDir string, eventType closureprotocol.LedgerEventType) (string, error) {
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(admissionValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		return "", err
	}
	for i := len(chain.Entries) - 1; i >= 0; i-- {
		ve := chain.Entries[i]
		if ve.Entry.EventType == eventType {
			if ve.Entry.ProducedAt == "" {
				return "", fmt.Errorf("ledger event %s has no produced_at", eventType)
			}
			return ve.Entry.ProducedAt, nil
		}
	}
	return "", fmt.Errorf("no %s event found in task ledger", eventType)
}
