// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func TestParseQueryClassArchitectureClaim(t *testing.T) {
	got, ok := parseQueryClass("architecture_claim")
	if !ok {
		t.Fatal("architecture_claim query class was not accepted")
	}
	if got != awarenesspb.QueryClass_QUERY_CLASS_ARCHITECTURE_CLAIM {
		t.Fatalf("class=%s, want ARCHITECTURE_CLAIM", got)
	}
}

func TestParseQueryClassOpenQuestion(t *testing.T) {
	got, ok := parseQueryClass("open_question")
	if !ok {
		t.Fatal("open_question query class was not accepted")
	}
	if got != awarenesspb.QueryClass_QUERY_CLASS_OPEN_QUESTION {
		t.Fatalf("class=%s, want OPEN_QUESTION", got)
	}
}

func TestParseQueryClassArchitectAnswer(t *testing.T) {
	got, ok := parseQueryClass("architect_answer")
	if !ok {
		t.Fatal("architect_answer query class was not accepted")
	}
	if got != awarenesspb.QueryClass_QUERY_CLASS_ARCHITECT_ANSWER {
		t.Fatalf("class=%s, want ARCHITECT_ANSWER", got)
	}
}

func TestParseQueryClassEvidenceProbe(t *testing.T) {
	got, ok := parseQueryClass("evidence_probe")
	if !ok {
		t.Fatal("evidence_probe query class was not accepted")
	}
	if got != awarenesspb.QueryClass_QUERY_CLASS_EVIDENCE_PROBE {
		t.Fatalf("class=%s, want EVIDENCE_PROBE", got)
	}
}
