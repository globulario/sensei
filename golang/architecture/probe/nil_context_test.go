// SPDX-License-Identifier: AGPL-3.0-only

package probe

import (
	"strings"
	"testing"
)

// probeWithCommand builds a probe whose step carries a command claiming a
// source, which is exactly what convergence generates from a graph.
func probeWithCommand(sourceRef, command string) EvidenceProbe {
	return EvidenceProbe{
		Steps: []ProbeStep{{
			Kind:      StepRunExistingTest,
			SourceRef: sourceRef,
			Command:   command,
		}},
	}
}

// reportsSourcingViolation asks the real validator whether it complained about
// command sourcing specifically. A minimal probe fails other rules too, so the
// test must look for this rule rather than for any error at all -- otherwise it
// would pass on unrelated failures and prove nothing.
func reportsSourcingViolation(p EvidenceProbe, ctx *ValidationContext) bool {
	err := ValidateProbe(p, ProbeDocument{}, ctx)
	return err != nil && strings.Contains(err.Error(), "copied exactly from a sourced Evidence node")
}

// THE #198 regression. A command-bearing probe must remain loadable when no
// validation context is available.
//
// Convergence generates such a probe from the graph, persists it, and then
// every ordinary load path passes nil -- AdvanceTask and convergence have no
// graph to pass. Treating nil as a failed match made the probe permanently
// unloadable: the task bricked, and `sensei advance-task`, the remediation the
// error itself recommended, refused the same probe.
func TestCommandBearingProbeLoadsWithoutAValidationContext(t *testing.T) {
	p := probeWithCommand("evidence:evidence.ci_green", "go test ./...")
	if reportsSourcingViolation(p, nil) {
		t.Fatal("a nil validation context reported a sourcing violation it could not possibly have checked")
	}
}

// Absence must not become permission. A command claiming NO source is
// malformed regardless of what is available to check it against.
func TestCommandWithNoSourceRefIsStillRefusedWithoutAContext(t *testing.T) {
	p := probeWithCommand("", "go test ./...")
	if !reportsSourcingViolation(p, nil) {
		t.Fatal("a command claiming no source was accepted; absence of a context is not permission")
	}
}

// The guard must still fire wherever it CAN be evaluated: a context carrying a
// graph that disagrees is a real violation, and relaxing the nil case must not
// have relaxed this one.
func TestCommandIsStillRefusedWhenAContextDisagrees(t *testing.T) {
	ctx := &ValidationContext{Graph: BuildGraphIndex(nil)}
	p := probeWithCommand("evidence:evidence.does_not_exist", "rm -rf /")
	if !reportsSourcingViolation(p, ctx) {
		t.Fatal("a command with no matching Evidence node was accepted while a graph was available to contradict it")
	}
}
