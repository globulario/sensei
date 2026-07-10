// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = old
	}()
	fn()
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestPrintGraphAuthority_ShowsFreshnessAndProvenance(t *testing.T) {
	out := captureStdout(t, func() {
		printGraphAuthority(&awarenesspb.GraphAuthority{
			Authoritative:                   true,
			GraphFreshnessState:             awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
			BuildProvenanceState:            awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED,
			GraphBuildCommit:                "abc123",
			EmbeddedSeedDigestSha256:        "seed123",
			LiveStoreGraphDigestSha256:      "live123",
			LiveStoreGraphTripleCount:       42,
			EmbeddedTransactionStampPresent: true,
			EmbeddedTransactionMatchesSeed:  true,
			CertifiedAwarenessGraphCommit:   "awg456",
			CertifiedServicesRepoCommit:     "svc789",
			EmbeddedTransactionDetail:       "embedded transaction certifies embedded seed",
		})
	})
	for _, want := range []string{
		"Authority: authoritative (current, provenance=stamped, transaction=certified)",
		"Live digest:  live123",
		"Seed digest:  seed123",
		"Build commit: abc123",
		"Tx awg:       awg456",
		"Tx services:  svc789",
		"Tx detail:    embedded transaction certifies embedded seed",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}
