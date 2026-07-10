// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func crWithCandidates(cs ...realizationCandidate) *crFile {
	f := &crFile{}
	f.ContractRealizations.Candidates = cs
	return f
}

func cand(impl, arch, conf string) realizationCandidate {
	return realizationCandidate{Implementation: impl, Realizes: arch, Source: "generated_evidence_scoring", Confidence: conf, Evidence: []string{"same directory x"}}
}

func hasRealization(f *crFile, impl, arch string) bool {
	for _, r := range f.ContractRealizations.Realizations {
		if r.Implementation == impl && r.Realizes == arch {
			return true
		}
	}
	return false
}
func hasCandidate(f *crFile, impl, arch string) bool {
	for _, c := range f.ContractRealizations.Candidates {
		if c.Implementation == impl && c.Realizes == arch {
			return true
		}
	}
	return false
}

func TestPromote_MovesCandidateIntoRealization(t *testing.T) {
	authored := &crFile{}
	gen := crWithCandidates(
		cand("contract.http.api_save_config", "contract.config_mutation_requires_valid_token", "medium"),
		cand("contract.http.uploads", "contract.served_path_must_be_anchored_and_confined", "low"),
	)
	rz, fromGen, err := promoteCandidate(authored, gen, "contract.http.api_save_config", "contract.config_mutation_requires_valid_token", "obligation: handler validates token before mutating config")
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if !fromGen {
		t.Error("expected promotion from the generated candidates")
	}
	if rz.Source != "promoted_candidate" {
		t.Errorf("source = %q, want promoted_candidate", rz.Source)
	}
	if rz.Confidence != "medium" {
		t.Errorf("confidence not preserved: %q", rz.Confidence)
	}
	if !hasRealization(authored, "contract.http.api_save_config", "contract.config_mutation_requires_valid_token") {
		t.Error("promoted pair missing from realizations")
	}
	// removed from candidates
	if hasCandidate(gen, "contract.http.api_save_config", "contract.config_mutation_requires_valid_token") {
		t.Error("candidate not removed after promotion")
	}
	// unrelated candidate untouched
	if !hasCandidate(gen, "contract.http.uploads", "contract.served_path_must_be_anchored_and_confined") {
		t.Error("unrelated candidate was disturbed")
	}
}

func TestPromote_RefusesMissingCandidate(t *testing.T) {
	authored := &crFile{}
	gen := crWithCandidates(cand("contract.http.uploads", "contract.served_path_must_be_anchored_and_confined", "low"))
	if _, _, err := promoteCandidate(authored, gen, "contract.http.api_save_config", "contract.config_mutation_requires_valid_token", "obligation: handler validates token before mutating config"); err == nil {
		t.Error("expected error promoting a non-existent candidate")
	}
}

func TestPromote_RefusesAmbiguous(t *testing.T) {
	authored := &crFile{}
	gen := crWithCandidates(
		cand("contract.http.api_save_config", "contract.config_mutation_requires_valid_token", "medium"),
		cand("contract.http.api_save_config", "contract.served_path_must_be_anchored_and_confined", "low"),
	)
	_, _, err := promoteCandidate(authored, gen, "contract.http.api_save_config", "", "obligation: token before mutation")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected ambiguous error, got %v", err)
	}
	// disambiguating with --arch must succeed
	if _, _, err := promoteCandidate(authored, gen, "contract.http.api_save_config", "contract.config_mutation_requires_valid_token", "obligation: handler validates token before mutating config"); err != nil {
		t.Errorf("disambiguated promotion failed: %v", err)
	}
}

func TestPromote_RefusesAlreadyAuthoritative(t *testing.T) {
	authored := &crFile{}
	authored.ContractRealizations.Realizations = []realizationCandidate{
		{Implementation: "contract.http.api_save_config", Realizes: "contract.config_mutation_requires_valid_token", Source: "promoted_candidate"},
	}
	gen := crWithCandidates(cand("contract.http.api_save_config", "contract.config_mutation_requires_valid_token", "medium"))
	if _, _, err := promoteCandidate(authored, gen, "contract.http.api_save_config", "contract.config_mutation_requires_valid_token", "obligation: handler validates token before mutating config"); err == nil || !strings.Contains(err.Error(), "already authoritative") {
		t.Errorf("expected already-authoritative refusal, got %v", err)
	}
}

func TestPromote_DoesNotTouchUnrelated_AndNeverBulk(t *testing.T) {
	authored := &crFile{}
	gen := crWithCandidates(
		cand("contract.http.api_save_config", "contract.config_mutation_requires_valid_token", "medium"),
		cand("contract.http.uploads", "contract.served_path_must_be_anchored_and_confined", "low"),
		cand("contract.http.api_get_images", "contract.served_path_must_be_anchored_and_confined", "low"),
	)
	if _, _, err := promoteCandidate(authored, gen, "contract.http.api_save_config", "contract.config_mutation_requires_valid_token", "obligation: handler validates token before mutating config"); err != nil {
		t.Fatal(err)
	}
	if n := len(authored.ContractRealizations.Realizations); n != 1 {
		t.Errorf("expected exactly 1 realization (no bulk promotion), got %d", n)
	}
	if n := len(gen.ContractRealizations.Candidates); n != 2 {
		t.Errorf("expected 2 candidates left, got %d", n)
	}
}

func TestPromote_OutputStableAndCandidatesOnlyStayCandidates(t *testing.T) {
	authored := &crFile{}
	gen := crWithCandidates(cand("contract.http.uploads", "contract.served_path_must_be_anchored_and_confined", "low"))
	out, err := renderCandidates(gen.ContractRealizations.Candidates)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "realizesContract") || strings.Contains(string(out), "realizedByContract") {
		t.Error("generated candidate render must not assert authoritative realizesContract")
	}
	// render is stable
	out2, _ := renderCandidates(gen.ContractRealizations.Candidates)
	if string(out2) != string(out) {
		t.Error("render not stable")
	}
	// promoted authored render carries realizations
	_, _, _ = promoteCandidate(authored, gen, "contract.http.uploads", "", "obligation: confines served path")
	a := string(renderCRFile(authored, authoredHeader))
	if !strings.Contains(a, "realizations:") || !strings.Contains(a, "promoted_candidate") {
		t.Error("authored render missing promoted realization")
	}
}
