// SPDX-License-Identifier: AGPL-3.0-only

package seedmeta

import "testing"

func TestParseTransactionStamp(t *testing.T) {
	got := ParseTransactionStamp([]byte(`format	v1
seed	digest_sha256	abc123
seed	triple_count	99
repo	awareness-graph	deadbeef
repo	services	cafebabe
tool	yaml2nt	toolsha
file	build_script	scriptsha
`))
	if !got.Present {
		t.Fatal("Present=false, want true")
	}
	if got.SeedDigest != "abc123" || got.SeedTripleCount != "99" {
		t.Fatalf("seed fields=%+v", got)
	}
	if got.AwarenessGraphCommit != "deadbeef" || got.ServicesCommit != "cafebabe" {
		t.Fatalf("repo fields=%+v", got)
	}
	if got.Yaml2NTSha256 != "toolsha" || got.BuildScriptSha256 != "scriptsha" {
		t.Fatalf("tool fields=%+v", got)
	}
}
