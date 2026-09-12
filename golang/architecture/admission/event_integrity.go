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

// EventIntegrityError is a typed refusal about a governed record: its occurrence
// set, its payload, or its artifact. It is NEVER absence.
//
// The distinction is the whole of Family B. A reader that returns a bare decode
// error, or (false, nil), leaves its caller unable to tell "this task never
// consumed a capability" from "this task's consumption record is unusable" -- and
// the guards downstream are written as `if err == nil { refuse }`, so an
// unreadable record stops them refusing.
type EventIntegrityError struct {
	Code      string
	EventType closureprotocol.LedgerEventType
	Detail    string
}

func (e *EventIntegrityError) Error() string { return e.Code + ": " + e.Detail }

const (
	codeIntegrityPrefix = "admission.event_integrity"
	// CodeMultipleOccurrences: more than one occurrence of a singleton fact.
	// There is no selection algorithm; the SET is the failure.
	CodeMultipleOccurrences = codeIntegrityPrefix + ".multiple_occurrences"
	// CodeArtifactMissing: the occurrence exists but omits the artifact its
	// meaning depends on. Not absence -- the event is right there.
	CodeArtifactMissing = codeIntegrityPrefix + ".artifact_missing"
	// CodeUndecodable: present and unreadable.
	CodeUndecodable = codeIntegrityPrefix + ".undecodable"
	// CodeUnratifiedEvent: a token the vocabulary knows with no ratified producer.
	CodeUnratifiedEvent = codeIntegrityPrefix + ".unratified_event"
)

// singletonArtifactFromChain reads a SINGLETON fact by enumerating its whole
// occurrence set and applying the declared count predicate (B-INV):
//
//	count == 0                          genuinely absent      -> (false, nil)
//	count == 1 && valid                 the fact              -> (true, nil)
//	count >  1                          INTEGRITY FAILURE
//	count == 1 && malformed/undecodable INTEGRITY FAILURE
//
// It does not scan backward and it does not choose. Scanning further back on a
// malformed latest occurrence would treat an unusable authoritative record as
// transparent; choosing at all is what let a receipt bound to nothing shadow the
// real one.
func singletonArtifactFromChain(taskDir string, chain ledger.VerifiedChain, eventType closureprotocol.LedgerEventType, artifactKey string, out any) (bool, error) {
	if closureprotocol.EventOccurrence[eventType] == closureprotocol.OccurrenceUnratified {
		for _, ve := range chain.Entries {
			if ve.Entry.EventType == eventType {
				return false, &EventIntegrityError{Code: CodeUnratifiedEvent, EventType: eventType,
					Detail: fmt.Sprintf("%s occurs at sequence %d but has no ratified producer contract", eventType, ve.Entry.Sequence)}
			}
		}
		return false, nil
	}

	var occurrences []ledger.VerifiedEntry
	for _, ve := range chain.Entries {
		if ve.Entry.EventType == eventType {
			occurrences = append(occurrences, ve)
		}
	}
	switch {
	case len(occurrences) == 0:
		return false, nil // genuine absence, and the ONLY absence
	case len(occurrences) > 1:
		return false, &EventIntegrityError{Code: CodeMultipleOccurrences, EventType: eventType,
			Detail: fmt.Sprintf("%d occurrences of a singleton fact; once one exists a second cannot supersede it", len(occurrences))}
	}

	ve := occurrences[0]
	data, err := os.ReadFile(ve.PayloadPath)
	if err != nil {
		return false, &EventIntegrityError{Code: CodeUndecodable, EventType: eventType, Detail: "payload unreadable: " + err.Error()}
	}
	payload, err := ledger.ParseTaskEventPayload(data)
	if err != nil {
		return false, &EventIntegrityError{Code: CodeUndecodable, EventType: eventType, Detail: "payload unparseable: " + err.Error()}
	}
	ref, ok := payload.Artifacts[artifactKey]
	if !ok {
		return false, &EventIntegrityError{Code: CodeArtifactMissing, EventType: eventType,
			Detail: fmt.Sprintf("the %s occurrence carries no %q artifact; the record exists and its meaning cannot be reconstructed", eventType, artifactKey)}
	}
	artifactData, err := os.ReadFile(filepath.Join(taskDir, filepath.FromSlash(ref.Path)))
	if err != nil {
		return false, &EventIntegrityError{Code: CodeUndecodable, EventType: eventType, Detail: "artifact unreadable: " + err.Error()}
	}
	if err := json.Unmarshal(artifactData, out); err != nil {
		return false, &EventIntegrityError{Code: CodeUndecodable, EventType: eventType, Detail: "artifact undecodable: " + err.Error()}
	}
	return true, nil
}
