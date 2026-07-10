// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func diagnoseSrc(t *testing.T, src string) diagnosis {
	t.Helper()
	var s runtimeEvidenceSnapshot
	if err := yaml.Unmarshal([]byte(src), &s); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	return diagnoseRuntime(s)
}

func TestDiagnoseRuntime_Verdicts(t *testing.T) {
	cases := []struct {
		name        string
		src         string
		wantVerdict string
		wantExit    int
	}{
		{
			// The spec's headline: a blocked state, NOT a bug.
			name:        "blocked by quorum",
			wantVerdict: vBlockedQuorum,
			wantExit:    0,
			src: `
schema_version: runtime-evidence/v1
platform: p
generated_at: t
subject: {type: service, id: sidekick, node: nuc}
lanes:
  desired_state: {freshness: fresh, owner: cc, facts: {desired_build_id: abc}}
  observed_state: {freshness: fresh, owner: na, facts: {installed: false, running: false}}
  quorum: {freshness: fresh, owner: doc, facts: {subsystem: object_store, required_members: 3, available_members: 2, quorum_met: false}}`,
		},
		{
			name:        "converged (all fresh)",
			wantVerdict: vConverged,
			wantExit:    0,
			src: `
schema_version: runtime-evidence/v1
platform: p
generated_at: t
subject: {type: service, id: echo, node: nuc}
lanes:
  desired_state: {freshness: fresh, owner: cc, facts: {desired_build_id: abc}}
  observed_state: {freshness: fresh, owner: na, facts: {installed: true, running: true}}
  runtime_identity: {freshness: fresh, owner: na, facts: {installed_build_id: abc}}`,
		},
		{
			// would-be-converged but a required lane is stale → NO false green.
			name:        "would-be-converged but stale evidence",
			wantVerdict: vEvidenceStale,
			wantExit:    1,
			src: `
schema_version: runtime-evidence/v1
platform: p
generated_at: t
subject: {type: service, id: echo, node: nuc}
lanes:
  desired_state: {freshness: fresh, owner: cc, facts: {desired_build_id: abc}}
  observed_state: {freshness: stale, owner: na, facts: {installed: true, running: true}}`,
		},
		{
			name:        "required lane missing",
			wantVerdict: vEvidenceMissing,
			wantExit:    1,
			src: `
schema_version: runtime-evidence/v1
platform: p
generated_at: t
subject: {type: service, id: echo, node: nuc}
lanes:
  desired_state: {freshness: fresh, owner: cc, facts: {desired_build_id: abc}}`,
		},
		{
			name:        "runtime identity mismatch",
			wantVerdict: vIdentityMismatch,
			wantExit:    0,
			src: `
schema_version: runtime-evidence/v1
platform: p
generated_at: t
subject: {type: service, id: echo, node: nuc}
lanes:
  desired_state: {freshness: fresh, owner: cc, facts: {desired_build_id: abc}}
  observed_state: {freshness: fresh, owner: na, facts: {installed: true, running: true}}
  runtime_identity: {freshness: fresh, owner: na, facts: {installed_build_id: xyz}}`,
		},
		{
			name:        "blocked by dependency (diagnosis finding)",
			wantVerdict: vBlockedDependency,
			wantExit:    0,
			src: `
schema_version: runtime-evidence/v1
platform: p
generated_at: t
subject: {type: service, id: sidekick, node: nuc}
lanes:
  desired_state: {freshness: fresh, owner: cc, facts: {desired_build_id: abc}}
  observed_state: {freshness: fresh, owner: na, facts: {installed: false}}
  diagnosis:
    freshness: fresh
    owner: doc
    findings:
      - {id: held_by_dependency_scylla, severity: blocking, summary: install held by an unmet dependency}`,
		},
		{
			name:        "desired state absent",
			wantVerdict: vBlockedDesiredMiss,
			wantExit:    0,
			src: `
schema_version: runtime-evidence/v1
platform: p
generated_at: t
subject: {type: service, id: ghost, node: nuc}
lanes:
  desired_state: {freshness: fresh, owner: cc, facts: {desired: absent}}
  observed_state: {freshness: fresh, owner: na, facts: {installed: false}}`,
		},
		{
			name:        "not converged (desired, not installed, no block)",
			wantVerdict: vNotConverged,
			wantExit:    0,
			src: `
schema_version: runtime-evidence/v1
platform: p
generated_at: t
subject: {type: service, id: echo, node: nuc}
lanes:
  desired_state: {freshness: fresh, owner: cc, facts: {desired_build_id: abc}}
  observed_state: {freshness: fresh, owner: na, facts: {installed: false}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := diagnoseSrc(t, tc.src)
			if d.Verdict != tc.wantVerdict {
				t.Fatalf("verdict = %q, want %q (reason: %s)", d.Verdict, tc.wantVerdict, d.Reason)
			}
			if ec := exitCodeForDiagnosis(d.Verdict); ec != tc.wantExit {
				t.Fatalf("exit = %d, want %d for verdict %s", ec, tc.wantExit, d.Verdict)
			}
		})
	}
}

// stale evidence is allowed to DIAGNOSE a block (just labelled low-confidence) —
// a blocked verdict is not a green claim.
func TestDiagnoseRuntime_StaleStillDiagnosesBlock(t *testing.T) {
	d := diagnoseSrc(t, `
schema_version: runtime-evidence/v1
platform: p
generated_at: t
subject: {type: service, id: sidekick, node: nuc}
lanes:
  desired_state: {freshness: stale, owner: cc, facts: {desired_build_id: abc}}
  observed_state: {freshness: stale, owner: na, facts: {installed: false}}
  quorum: {freshness: stale, owner: doc, facts: {quorum_met: false}}`)
	if d.Verdict != vBlockedQuorum {
		t.Fatalf("verdict = %q, want %q", d.Verdict, vBlockedQuorum)
	}
	if d.Confidence != "low_stale_evidence" {
		t.Fatalf("confidence = %q, want low_stale_evidence (stale must be labelled)", d.Confidence)
	}
}

// The committed Globular example (Phase 2a) must diagnose to blocked_by_quorum —
// ties the diagnosis engine to the worked adapter example end to end.
func TestDiagnoseRuntime_GlobularExampleIsBlockedByQuorum(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "globular-runtime-adapter", "sidekick-quorum-snapshot.example.yaml"))
	if err != nil {
		t.Fatalf("read example: %v", err)
	}
	var s runtimeEvidenceSnapshot
	if err := yaml.Unmarshal(raw, &s); err != nil {
		t.Fatalf("parse example: %v", err)
	}
	if d := diagnoseRuntime(s); d.Verdict != vBlockedQuorum {
		t.Fatalf("Globular example verdict = %q, want %q (must not be release_install_failed)", d.Verdict, vBlockedQuorum)
	}
}
