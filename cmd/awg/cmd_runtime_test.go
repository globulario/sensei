// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestGlobularExampleAdapterValidates proves the boundary: AWG validates a real
// GLOBULAR runtime adapter (manifest + worked snapshot) with its generic Phase-1
// validators and zero Globular code in core. It also keeps the committed example
// honest — a drift in the example or the schema fails here.
func TestGlobularExampleAdapterValidates(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "globular-runtime-adapter")

	mraw, err := os.ReadFile(filepath.Join(dir, "globular-runtime-adapter.yaml"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m runtimeAdapterManifest
	if err := yaml.Unmarshal(mraw, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if fs := validateRuntimeAdapterManifest(m); hasErrors(fs) {
		t.Fatalf("Globular adapter manifest must validate, got: %+v", fs)
	}

	sraw, err := os.ReadFile(filepath.Join(dir, "sidekick-quorum-snapshot.example.yaml"))
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	var s runtimeEvidenceSnapshot
	if err := yaml.Unmarshal(sraw, &s); err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	if fs := validateRuntimeSnapshot(s); hasErrors(fs) {
		t.Fatalf("Globular example snapshot must validate, got: %+v", fs)
	}
}

func mustManifest(t *testing.T, src string) runtimeAdapterManifest {
	t.Helper()
	var m runtimeAdapterManifest
	if err := yaml.Unmarshal([]byte(src), &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	return m
}

func mustSnapshot(t *testing.T, src string) runtimeEvidenceSnapshot {
	t.Helper()
	var s runtimeEvidenceSnapshot
	if err := yaml.Unmarshal([]byte(src), &s); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	return s
}

// A well-formed, platform-agnostic adapter manifest passes — and AWG core never
// inspects the platform service names (provider/source are opaque to it).
func TestValidateRuntimeAdapterManifest_Valid(t *testing.T) {
	m := mustManifest(t, `
schema_version: runtime-adapter/v1
adapter:
  name: some-runtime-evidence-adapter
  platform: someplatform
lanes:
  desired_state:
    provider: control_plane
    source: ListDesired
    authority: owner
    freshness_required: true
  diagnosis:
    provider: doctor
    source: GetReport
    authority: diagnostic
    freshness_required: true
  action_trace:
    provider: workflow
    source: GetRun
    authority: derived
    freshness_required: when_remediation_executed
`)
	if fs := validateRuntimeAdapterManifest(m); hasErrors(fs) {
		t.Fatalf("expected valid manifest, got errors: %+v", fs)
	}
}

func TestValidateRuntimeAdapterManifest_Defects(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"wrong schema_version", `
schema_version: runtime-adapter/v2
adapter: {name: a, platform: p}
lanes: {desired_state: {provider: x, source: y, authority: owner, freshness_required: true}}`},
		{"missing adapter name", `
schema_version: runtime-adapter/v1
adapter: {platform: p}
lanes: {desired_state: {provider: x, source: y, authority: owner, freshness_required: true}}`},
		{"unknown lane (platform invented one)", `
schema_version: runtime-adapter/v1
adapter: {name: a, platform: p}
lanes: {made_up_lane: {provider: x, source: y, authority: owner, freshness_required: true}}`},
		{"invalid authority level", `
schema_version: runtime-adapter/v1
adapter: {name: a, platform: p}
lanes: {desired_state: {provider: x, source: y, authority: boss, freshness_required: true}}`},
		{"missing provider", `
schema_version: runtime-adapter/v1
adapter: {name: a, platform: p}
lanes: {desired_state: {source: y, authority: owner, freshness_required: true}}`},
		{"missing source", `
schema_version: runtime-adapter/v1
adapter: {name: a, platform: p}
lanes: {desired_state: {provider: x, authority: owner, freshness_required: true}}`},
		{"missing freshness_required", `
schema_version: runtime-adapter/v1
adapter: {name: a, platform: p}
lanes: {desired_state: {provider: x, source: y, authority: owner}}`},
		{"no lanes", `
schema_version: runtime-adapter/v1
adapter: {name: a, platform: p}
lanes: {}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if fs := validateRuntimeAdapterManifest(mustManifest(t, tc.src)); !hasErrors(fs) {
				t.Fatalf("expected an error for %q, got none", tc.name)
			}
		})
	}
}

func TestValidateRuntimeSnapshot_Valid(t *testing.T) {
	s := mustSnapshot(t, `
schema_version: runtime-evidence/v1
platform: someplatform
cluster_id: dev
generated_at: "2026-06-23T00:00:00Z"
subject: {type: service, id: sidekick, node: nuc}
lanes:
  observed_state:
    status: present
    freshness: fresh
    owner: node_agent
    source: GetServiceRuntimeProof
    observed_at: "2026-06-23T00:00:00Z"
  quorum:
    status: present
    freshness: fresh
    owner: object_store_status
    source: ObjectStoreStatus
    observed_at: "2026-06-23T00:00:00Z"
`)
	if fs := validateRuntimeSnapshot(s); hasErrors(fs) {
		t.Fatalf("expected valid snapshot, got errors: %+v", fs)
	}
}

func TestValidateRuntimeSnapshot_Defects(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"wrong schema_version", `
schema_version: runtime-evidence/v2
platform: p
generated_at: "t"
subject: {id: s}
lanes: {observed_state: {freshness: fresh, owner: o, source: x, observed_at: "t"}}`},
		{"invalid freshness mode", `
schema_version: runtime-evidence/v1
platform: p
generated_at: "t"
subject: {id: s}
lanes: {observed_state: {freshness: maybe, owner: o, source: x, observed_at: "t"}}`},
		{"missing owner (no authority anchor)", `
schema_version: runtime-evidence/v1
platform: p
generated_at: "t"
subject: {id: s}
lanes: {observed_state: {freshness: fresh, source: x, observed_at: "t"}}`},
		{"unknown lane", `
schema_version: runtime-evidence/v1
platform: p
generated_at: "t"
subject: {id: s}
lanes: {made_up: {freshness: fresh, owner: o, source: x, observed_at: "t"}}`},
		{"missing subject id", `
schema_version: runtime-evidence/v1
platform: p
generated_at: "t"
subject: {type: service}
lanes: {observed_state: {freshness: fresh, owner: o, source: x, observed_at: "t"}}`},
		{"no lanes", `
schema_version: runtime-evidence/v1
platform: p
generated_at: "t"
subject: {id: s}
lanes: {}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if fs := validateRuntimeSnapshot(mustSnapshot(t, tc.src)); !hasErrors(fs) {
				t.Fatalf("expected an error for %q, got none", tc.name)
			}
		})
	}
}

// observed_at missing is a WARNING (freshness cannot be re-derived), not an error.
func TestValidateRuntimeSnapshot_MissingObservedAtIsWarning(t *testing.T) {
	s := mustSnapshot(t, `
schema_version: runtime-evidence/v1
platform: p
generated_at: "t"
subject: {id: s}
lanes: {observed_state: {freshness: fresh, owner: o, source: x}}`)
	fs := validateRuntimeSnapshot(s)
	if hasErrors(fs) {
		t.Fatalf("missing observed_at should be a warning, not an error: %+v", fs)
	}
	var warned bool
	for _, f := range fs {
		if f.Severity == "warning" && f.Where == "lanes.observed_state.observed_at" {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("expected an observed_at warning, got %+v", fs)
	}
}

// freshness_required accepts bool and the "when_*" condition form.
func TestValidFreshnessRequired(t *testing.T) {
	for _, ok := range []interface{}{true, false, "true", "false", "when_remediation_executed"} {
		if !validFreshnessRequired(ok) {
			t.Errorf("expected %v to be a valid freshness_required", ok)
		}
	}
	for _, bad := range []interface{}{"", "sometimes", 3, nil} {
		if validFreshnessRequired(bad) {
			t.Errorf("expected %v to be invalid", bad)
		}
	}
}
