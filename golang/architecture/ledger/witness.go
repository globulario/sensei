// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// A HISTORY WITNESS, and why it cannot live inside the task.
//
// A chain that has lost its tail is a valid PREFIX of itself: every per-entry
// check passes, the digest links are intact, and the only record that disagrees
// is HEAD. So the verifier could confirm that what remains is consistent while
// being unable to say whether anything is missing -- and foldGovernance then
// reads the absence of admission_consumed as permission to grant mutation again.
//
// The repair is not more checking of the chain. No witness derived from the
// chain can establish that chain's completeness; HEAD could not do it, and
// ReconcileDerivedState made that concrete by rewriting HEAD from the recomputed
// SHORTER chain, destroying the last trace of the truncation.
//
// This witness is therefore monotonic and lives OUTSIDE the task directory, as a
// sibling of it. Destroying a task's ledger -- or the whole task directory --
// does not destroy the record that the task once reached sequence N.
//
// TWO PROHIBITIONS are the operative content, per the ruling in
// docs/audit/2026-09-11-ledger-resurrection-repair-contracts.md (98bc9ad):
// the witness may be validated against, but NEVER LOWERED and NEVER REGENERATED
// FROM FEWER ENTRIES.
const WitnessSchemaVersion = "1"

// HistoryWitness records how far a task's chain has provably advanced.
type HistoryWitness struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`
	TaskID        string `json:"task_id" yaml:"task_id"`
	// HighestSequence only ever increases. It is the completeness claim.
	HighestSequence  int    `json:"highest_sequence" yaml:"highest_sequence"`
	HeadDigestSHA256 string `json:"head_digest_sha256" yaml:"head_digest_sha256"`
	// EstablishedAtSequence is where this witness's guarantee BEGINS. A witness
	// established at 1 covers the chain from genesis. One established later was
	// bootstrapped over a chain that already existed, and can vouch only from
	// that point forward -- it is not evidence about what came before.
	EstablishedAtSequence int    `json:"established_at_sequence" yaml:"established_at_sequence"`
	Bootstrapped          bool   `json:"bootstrapped" yaml:"bootstrapped"`
	ObservedAt            string `json:"observed_at" yaml:"observed_at"`
}

// CoversFromGenesis reports whether the witness can speak for the whole chain.
// A bootstrapped witness cannot: it says "this task has reached at least N",
// never "this task has never been longer than N".
func (w HistoryWitness) CoversFromGenesis() bool {
	return !w.Bootstrapped && w.EstablishedAtSequence <= 1
}

// witnessPath places the witness as a SIBLING of the task directory. Deliberately
// not inside it: a witness that is deleted by the same `rm -rf` as the evidence it
// guards witnesses nothing.
func witnessPath(taskDir string) string {
	clean := filepath.Clean(taskDir)
	return filepath.Join(filepath.Dir(clean), ".ledger-witness", filepath.Base(clean)+".yaml")
}

// readWitness returns the witness and whether one exists. A witness that exists
// but cannot be read is an error, never "no witness" -- the distinction this whole
// repair is about.
func readWitness(taskDir string) (HistoryWitness, bool, error) {
	data, err := os.ReadFile(witnessPath(taskDir))
	if os.IsNotExist(err) {
		return HistoryWitness{}, false, nil
	}
	if err != nil {
		return HistoryWitness{}, false, err
	}
	var w HistoryWitness
	if err := yaml.Unmarshal(data, &w); err != nil {
		return HistoryWitness{}, false, err
	}
	return w, true, nil
}

// advanceWitness records that the chain has reached head. It NEVER lowers the
// recorded sequence and never rewrites it from a shorter chain: an advance to a
// sequence at or below the recorded one is a no-op, not a correction.
func advanceWitness(taskDir, taskID string, head Head, at time.Time) error {
	existing, ok, err := readWitness(taskDir)
	if err != nil {
		return err
	}
	if ok && head.Sequence <= existing.HighestSequence {
		return nil
	}
	next := HistoryWitness{
		SchemaVersion:    WitnessSchemaVersion,
		TaskID:           taskID,
		HighestSequence:  head.Sequence,
		HeadDigestSHA256: head.EntryDigestSHA256,
		ObservedAt:       at.UTC().Format(time.RFC3339),
	}
	if ok {
		next.EstablishedAtSequence = existing.EstablishedAtSequence
		next.Bootstrapped = existing.Bootstrapped
		if next.TaskID == "" {
			next.TaskID = existing.TaskID
		}
	} else {
		// First sight of this task. Established at genesis is a full guarantee;
		// established later means a chain was already here, and this witness is
		// honest about covering only what follows.
		next.EstablishedAtSequence = head.Sequence
		next.Bootstrapped = head.Sequence > 1
	}
	path := witnessPath(taskDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(next)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}
