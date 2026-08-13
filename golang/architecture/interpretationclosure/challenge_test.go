// SPDX-License-Identifier: AGPL-3.0-only

package interpretationclosure

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadChallengePlanRejectsAuthoredOutcomesAndDuplicateKeys(t *testing.T) {
	root := t.TempDir()
	withOutcome := filepath.Join(root, "outcome.json")
	if err := os.WriteFile(withOutcome, []byte(`{
  "schema_version":"sensei.interpretation-closure.challenge.v1",
  "go_probes":[{
    "claim_id":"invariant.one",
    "kind":"go_type_exists",
    "package_pattern":"example.com/p",
    "type_name":"T",
    "expected":"true",
    "status":"supported"
  }]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadChallengePlan(withOutcome); err == nil {
		t.Fatal("challenge plan accepted caller-authored status")
	}

	duplicate := filepath.Join(root, "duplicate.json")
	if err := os.WriteFile(duplicate, []byte(`{
  "schema_version":"sensei.interpretation-closure.challenge.v1",
  "go_probes":[{
    "claim_id":"invariant.one",
    "kind":"go_type_exists",
    "package_pattern":"example.com/p",
    "type_name":"T",
    "expected":"true",
    "expected":"false"
  }]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadChallengePlan(duplicate); err == nil {
		t.Fatal("challenge plan accepted duplicate expected key")
	}
}

func TestChallengePlanDigestIsOrderIndependentAfterNormalization(t *testing.T) {
	one := ChallengePlan{SchemaVersion: ChallengePlanSchemaVersion, GoProbes: []GoProbe{
		{ClaimID: "invariant.b", Kind: GoProbeTypeExists, PackagePattern: "example.com/p", TypeName: "B", Expected: "true"},
		{ClaimID: "invariant.a", Kind: GoProbeUnderlyingTypeEquals, PackagePattern: "example.com/p", TypeName: "A", Expected: "string"},
	}}
	two := ChallengePlan{SchemaVersion: ChallengePlanSchemaVersion, GoProbes: []GoProbe{one.GoProbes[1], one.GoProbes[0]}}
	first, err := ChallengePlanDigest(one)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ChallengePlanDigest(two)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("challenge plan ordering changed identity: %s != %s", first, second)
	}
}
