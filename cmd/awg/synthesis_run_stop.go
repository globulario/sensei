// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Typed pre-driver stops.
//
// Issue #149 requires `sensei synthesis-run` to distinguish its degraded
// worlds and forbids collapsing them "into generic success or failure". Every
// refusal that happens BEFORE the governed driver starts used to return one
// code (exitResolutionFailure) with a free-text stderr line, so a caller could
// not tell "there is no task checkpoint" from "the graph is stale" from "the
// generation provider is missing" without parsing prose — and prose is not a
// contract. Worse, the three call for different operator actions: run
// prepare-change, rebuild the graph, install a provider.
//
// This file gives those states a closed vocabulary, one distinct exit code
// each, and a machine-readable record that honours --format. Codes start at 10
// so they cannot collide with the driver's disposition codes (0-7), and the
// unclassified fallback keeps exitResolutionFailure, which means an existing
// caller testing "non-zero means it did not produce a candidate" is unaffected.
type resolutionStopReason string

const (
	// The five worlds issue #149 names for the pre-driver phase.
	stopNoTaskCheckpoint          resolutionStopReason = "no-task-checkpoint"
	stopTaskAwaitingAnswer        resolutionStopReason = "task-awaiting-answer"
	stopGraphIdentityUnusable     resolutionStopReason = "graph-identity-unusable"
	stopClosureUnavailable        resolutionStopReason = "closure-digest-unavailable"
	stopInterpretationUnavailable resolutionStopReason = "interpretation-unavailable"
	// The two provider worlds it names separately, because "the model CLI is
	// missing" and "the generation runner could not be built" are different
	// repairs by different people.
	stopCognitiveProviderUnavailable  resolutionStopReason = "cognitive-provider-unavailable"
	stopGenerationProviderUnavailable resolutionStopReason = "generation-provider-unavailable"
	// Two worlds where the inputs are PRESENT and REFUSED, which is not the
	// same as unavailable and does not have the same repair. Folding either
	// into interpretation-unavailable would report a missing artifact when the
	// artifact is there and disagrees, sending an operator to regenerate
	// something that already exists.
	stopInterpretationObjectiveMismatch resolutionStopReason = "interpretation-objective-mismatch"
	stopProofObligationsOutstanding     resolutionStopReason = "proof-obligations-outstanding"
	// Durable resume. "The store cannot be opened" and "that boundary is not
	// in it" are different repairs -- one is a filesystem/permissions problem,
	// the other means the operator named a checkpoint this session never
	// wrote -- and neither is the third case, where the boundary IS found and
	// O7 refuses to continue from it because identity moved.
	stopCheckpointStoreUnusable resolutionStopReason = "checkpoint-store-unusable"
	stopCheckpointUnavailable   resolutionStopReason = "checkpoint-unavailable"
	stopResumeRefused           resolutionStopReason = "resume-refused"
	// Unclassified. Deliberately retained rather than removed: a resolution
	// failure nobody has typed yet must stay visible as untyped instead of
	// being forced into whichever named world looks closest, which would make
	// the vocabulary lie.
	stopUnclassified resolutionStopReason = "resolution-failure"
)

// stopExitCodes binds each reason to its own code. Kept as data, not a switch,
// so the "every reason is distinct" test can range over the whole vocabulary
// and a reason added without a code is a missing map entry rather than a
// silent fallthrough to someone else's code.
var stopExitCodes = map[resolutionStopReason]int{
	stopUnclassified:                    exitResolutionFailure,
	stopNoTaskCheckpoint:                10,
	stopTaskAwaitingAnswer:              11,
	stopGraphIdentityUnusable:           12,
	stopClosureUnavailable:              13,
	stopInterpretationUnavailable:       14,
	stopCognitiveProviderUnavailable:    15,
	stopGenerationProviderUnavailable:   16,
	stopInterpretationObjectiveMismatch: 17,
	stopProofObligationsOutstanding:     18,
	stopCheckpointStoreUnusable:         19,
	stopCheckpointUnavailable:           20,
	stopResumeRefused:                   21,
}

// stopMeanings is what an operator should DO about each state. The exit code
// says which world it is; this says why it stopped there.
var stopMeanings = map[resolutionStopReason]string{
	stopUnclassified:                    "resolution failed before the governed run began",
	stopNoTaskCheckpoint:                "no verified task checkpoint is bound; run 'sensei prepare-change' first",
	stopTaskAwaitingAnswer:              "the task carries an unanswered primary blocker and is not ready to synthesize",
	stopGraphIdentityUnusable:           "workspace/graph identity could not be composed completely",
	stopClosureUnavailable:              "the task's closure proof could not be resolved or digested",
	stopInterpretationUnavailable:       "a grounded interpretation could not be constructed",
	stopCognitiveProviderUnavailable:    "the cognitive provider is unavailable or unsupported",
	stopGenerationProviderUnavailable:   "the generation provider could not be constructed",
	stopInterpretationObjectiveMismatch: "the interpretation's objective does not match the task's",
	stopProofObligationsOutstanding:     "proof obligations are recorded and undischarged; this run may not proceed past them",
	stopCheckpointStoreUnusable:         "the durable checkpoint store could not be opened",
	stopCheckpointUnavailable:           "the named checkpoint is not in this store",
	stopResumeRefused:                   "the checkpoint is valid but the session may not continue under the current boundary",
}

// synthesisRunStop is the machine-readable record of a pre-driver refusal. It
// is deliberately NOT a synthesisRunReport: there is no receipt, no
// disposition and no candidate, and emitting a report-shaped object with those
// fields empty would invite a caller to read "no candidate" as "the run
// completed and produced nothing".
type synthesisRunStop struct {
	Stop       string `json:"stop"`
	Reason     string `json:"reason"`
	Meaning    string `json:"meaning"`
	Detail     string `json:"detail,omitempty"`
	ExitCode   int    `json:"exit_code"`
	RanDriver  bool   `json:"ran_governed_driver"`
	TaskID     string `json:"task_id,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

func stopExitCode(reason resolutionStopReason) int {
	if code, ok := stopExitCodes[reason]; ok {
		return code
	}
	// A reason with no code is a defect in this file, not a caller error, and
	// must not masquerade as one of the typed worlds.
	return exitInternalDefect
}

// resolutionStop emits the typed refusal and returns the exit code to use.
//
// RanDriver is always false here by construction: this path exists only for
// refusals that happen before the governed driver is entered, and stating that
// explicitly keeps a reader from having to infer it from the absence of a
// receipt.
func resolutionStop(format string, reason resolutionStopReason, detail string, suggestion string) int {
	code := stopExitCode(reason)
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(synthesisRunStop{
			Stop:       "pre-run",
			Reason:     string(reason),
			Meaning:    stopMeanings[reason],
			Detail:     detail,
			ExitCode:   code,
			RanDriver:  false,
			Suggestion: suggestion,
		})
		return code
	}
	fmt.Fprintf(os.Stderr, "sensei synthesis-run: stop: %s (exit %d)\n", reason, code)
	fmt.Fprintf(os.Stderr, "  meaning: %s\n", stopMeanings[reason])
	if detail != "" {
		fmt.Fprintf(os.Stderr, "  detail:  %s\n", detail)
	}
	if suggestion != "" {
		fmt.Fprintf(os.Stderr, "  next:    %s\n", suggestion)
	}
	return code
}
