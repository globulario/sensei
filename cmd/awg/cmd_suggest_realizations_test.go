// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

// fixtures mirroring the real demo surfaces (same-dir bridges, like the gateway).
func demoGraph() (map[string]*implC, map[string]*archC, map[string][]string, map[string]bool) {
	impls := map[string]*implC{
		"contract.http.api_save_config": {
			id: "contract.http.api_save_config", kind: "http", name: "HTTP /api/save-config", rw: "write",
			files: []string{"internal/gateway/handlers/config/config.go"},
		},
		"contract.http.uploads": {
			id: "contract.http.uploads", kind: "http", name: "HTTP /uploads", rw: "write",
			files: []string{"internal/gateway/handlers/files/files.go"},
		},
		"contract.http.api_get_config": { // read-only sibling in the same dir — must NOT bridge a write contract
			id: "contract.http.api_get_config", kind: "http", name: "HTTP /api/get-config", rw: "read",
			files: []string{"internal/gateway/handlers/config/config.go"},
		},
		"contract.grpc.unrelated": { // different dir, no name overlap — must produce nothing
			id: "contract.grpc.unrelated", kind: "grpc", name: "Unrelated", rw: "write",
			files: []string{"proto/unrelated.proto"},
		},
	}
	archs := map[string]*archC{
		"contract.config_mutation_requires_valid_token": {
			id: "contract.config_mutation_requires_valid_token", name: "Config mutation requires a valid token", rw: "write",
			files: []string{"internal/gateway/handlers/config/save_config.go"},
		},
		"contract.served_path_must_be_anchored_and_confined": {
			id: "contract.served_path_must_be_anchored_and_confined", name: "Served file paths must be anchored and confined", rw: "read_write",
			files: []string{"internal/gateway/handlers/files/serve.go"},
		},
	}
	return impls, archs, map[string][]string{}, map[string]bool{}
}

func find(cands []realizationCandidate, impl, arch string) (realizationCandidate, bool) {
	for _, c := range cands {
		if c.Implementation == impl && c.Realizes == arch {
			return c, true
		}
	}
	return realizationCandidate{}, false
}

func TestSuggest_DemoPairsGenerated(t *testing.T) {
	cands := suggestCandidates(demoGraph())

	if _, ok := find(cands, "contract.http.api_save_config", "contract.config_mutation_requires_valid_token"); !ok {
		t.Error("expected candidate api_save_config -> config_mutation_requires_valid_token")
	}
	if _, ok := find(cands, "contract.http.uploads", "contract.served_path_must_be_anchored_and_confined"); !ok {
		t.Error("expected candidate uploads -> served_path_must_be_anchored_and_confined")
	}
}

func TestSuggest_ReadOnlyDoesNotRealizeWriteContract(t *testing.T) {
	cands := suggestCandidates(demoGraph())
	if _, ok := find(cands, "contract.http.api_get_config", "contract.config_mutation_requires_valid_token"); ok {
		t.Error("read-only /api/get-config must NOT realize a write-only mutation contract (no strong signal)")
	}
}

func TestSuggest_PathOverlapAloneIsLowConfidence(t *testing.T) {
	cands := suggestCandidates(demoGraph())
	// uploads <-> served_path bridges only by directory (no name token overlap, no
	// exact file) — must be low confidence.
	c, ok := find(cands, "contract.http.uploads", "contract.served_path_must_be_anchored_and_confined")
	if !ok {
		t.Fatal("missing uploads candidate")
	}
	if c.Confidence != "low" {
		t.Errorf("dir-overlap-only candidate confidence = %q, want low", c.Confidence)
	}
	// save-config has dir + name("config") overlap → medium.
	sc, _ := find(cands, "contract.http.api_save_config", "contract.config_mutation_requires_valid_token")
	if sc.Confidence != "medium" {
		t.Errorf("dir+name candidate confidence = %q, want medium", sc.Confidence)
	}
}

func TestSuggest_NoUnrelatedPairs(t *testing.T) {
	cands := suggestCandidates(demoGraph())
	for _, c := range cands {
		if c.Implementation == "contract.grpc.unrelated" {
			t.Errorf("unrelated impl (different dir, no name overlap) produced a candidate: %+v", c)
		}
	}
}

func TestSuggest_OutputIsCandidatesOnly_NeverAuthoritative(t *testing.T) {
	cands := suggestCandidates(demoGraph())
	out, err := renderCandidates(cands)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "candidates:") {
		t.Error("output must contain a candidates: list")
	}
	if strings.Contains(s, "realizesContract") || strings.Contains(s, "realizedByContract") {
		t.Error("generator output must never reference realizesContract / realizedByContract")
	}
	// realizations: must be present-but-empty (authoritative is human-only).
	if !strings.Contains(s, "realizations: []") {
		t.Error("output realizations: list must be empty (no auto-promotion)")
	}
}

func TestSuggest_SkipsAlreadyAuthoritative(t *testing.T) {
	impls, archs, fm, _ := demoGraph()
	auth := map[string]bool{"contract.http.api_save_config|contract.config_mutation_requires_valid_token": true}
	cands := suggestCandidates(impls, archs, fm, auth)
	if _, ok := find(cands, "contract.http.api_save_config", "contract.config_mutation_requires_valid_token"); ok {
		t.Error("a pair already linked by authoritative realizesContract must not be re-suggested")
	}
}

func TestSuggest_Deterministic(t *testing.T) {
	a := suggestCandidates(demoGraph())
	b := suggestCandidates(demoGraph())
	if len(a) != len(b) {
		t.Fatalf("non-deterministic length: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Implementation != b[i].Implementation || a[i].Realizes != b[i].Realizes {
			t.Fatalf("non-deterministic order at %d", i)
		}
	}
}
