// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"path/filepath"
	"testing"

	"github.com/globulario/awareness-graph/golang/extractor/protoscan"
)

func iface(rw string) *protoscan.UML { return &protoscan.UML{Kind: "Interface"} }

func TestExtractPatternCandidates(t *testing.T) {
	contracts := []protoscan.Contract{
		{ID: "contract.read_api", Name: "ReadAPI", ReadOrWrite: "read", Uml: iface("read")},
		{ID: "contract.write_api", Name: "WriteAPI", ReadOrWrite: "read_write", Uml: iface("read_write")},
		{ID: "contract.maybe_api", Name: "MaybeAPI", ReadOrWrite: "unknown", Uml: iface("unknown")},
		// RPC-level contract (Operation) must be ignored — only services suggest patterns.
		{ID: "contract.read_api.get", Name: "ReadAPI.Get", ReadOrWrite: "read", Uml: &protoscan.UML{Kind: "Operation"}},
	}
	got := extractPatternCandidates(contracts)
	byID := map[string]candidateDoc{}
	for _, c := range got {
		byID[c.doc.ID] = c.doc
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 candidates (read-only + guarded, unknown+rpc skipped), got %d", len(got))
	}
	ro, ok := byID["candidate.implementation_pattern.read_api_read_only"]
	if !ok || ro.Status != "candidate" || ro.Confidence != "candidate" || ro.Class != "ImplementationPattern" {
		t.Errorf("read-only candidate wrong: %+v", ro)
	}
	if _, ok := byID["candidate.implementation_pattern.write_api_guarded_mutation"]; !ok {
		t.Errorf("missing guarded_mutation candidate for the write service")
	}
}

func TestExtractMisuseCandidates(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "store", "db.go"), "package store\nimport \"database/sql\"\nfunc load(db *sql.DB) { _, _ = db.Query(\"select 1\") }\n")
	writeFile(t, filepath.Join(root, "pure", "calc.go"), "package pure\nfunc Add(a,b int) int { return a+b }\n")
	writeFile(t, filepath.Join(root, "store", "db_test.go"), "package store\nimport \"database/sql\"\n") // test → ignored

	got := extractMisuseCandidates(root)
	if len(got) != 1 {
		t.Fatalf("expected 1 aggregate misuse candidate, got %d", len(got))
	}
	c := got[0].doc
	if c.Class != "PatternMisuse" || c.Status != "candidate" {
		t.Errorf("misuse candidate wrong: class=%q status=%q", c.Class, c.Status)
	}
	if len(c.DetectedIn) != 1 || c.DetectedIn[0] != "store/db.go" {
		t.Errorf("detected_in = %v, want [store/db.go] (test file + pure file excluded)", c.DetectedIn)
	}

	// No storage drivers → no candidate.
	clean := t.TempDir()
	writeFile(t, filepath.Join(clean, "x.go"), "package x\n")
	if got := extractMisuseCandidates(clean); got != nil {
		t.Errorf("expected no misuse candidates for a driver-free repo, got %v", got)
	}
}

func TestExtractMisuseCandidates_IgnoresWiringOnlyAndExplicitOwnedException(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "wiring", "etcd_client.go"), "package wiring\nimport clientv3 \"go.etcd.io/etcd/client/v3\"\nfunc newClient() clientv3.OpOption { return clientv3.WithPrefix() }\n")
	writeFile(t, filepath.Join(root, "xds", "etcd_ingress.go"), `package xds
// @awareness file_role=xds_self_owned_legacy_etcd_ingress_config_parser
// Because xDS is the OWNER of /globular/xds/v1/*, these reads are allowed.
import clientv3 "go.etcd.io/etcd/client/v3"
type getter interface { Get(any, string, ...clientv3.OpOption) }
func parse(g getter) { _, _ = g.Get(nil, "/globular/xds/v1/ingress", clientv3.WithPrefix()) }
`)

	if got := extractMisuseCandidates(root); got != nil {
		t.Fatalf("expected no misuse candidates for wiring-only import and explicit owned exception, got %v", got)
	}
}
