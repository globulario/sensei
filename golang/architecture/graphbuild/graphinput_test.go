// SPDX-License-Identifier: AGPL-3.0-only

package graphbuild

import "testing"

const supDigest = "1111111111111111111111111111111111111111111111111111111111111111"

func validSnapshot() GraphInputSnapshot {
	return GraphInputSnapshot{
		SchemaVersion:    GraphInputSnapshotSchemaVersion,
		PolicyID:         "sensei.resultpipeline.graph-inputs/v1",
		RepositoryDomain: "github.com/globulario/sensei",
		SourceRoots: []LogicalSourceRoot{
			{LogicalPath: "docs/awareness", SkipNestedGenerated: true},
		},
	}
}

func TestGraphInputSnapshotDigestOrderIndependent(t *testing.T) {
	a := validSnapshot()
	a.SourceRoots = []LogicalSourceRoot{{LogicalPath: "eval/contracts"}, {LogicalPath: "docs/awareness"}}
	a.SupplementalGraphs = []SupplementalGraphBinding{
		{ID: "pack.b", Version: "v1", SemanticDigestSHA256: supDigest, ArtifactKey: "supplemental_graph.pack.b"},
		{ID: "pack.a", Version: "v1", SemanticDigestSHA256: supDigest, ArtifactKey: "supplemental_graph.pack.a"},
	}
	b := validSnapshot()
	b.SourceRoots = []LogicalSourceRoot{{LogicalPath: "docs/awareness"}, {LogicalPath: "eval/contracts"}}
	b.SupplementalGraphs = []SupplementalGraphBinding{
		{ID: "pack.a", Version: "v1", SemanticDigestSHA256: supDigest, ArtifactKey: "supplemental_graph.pack.a"},
		{ID: "pack.b", Version: "v1", SemanticDigestSHA256: supDigest, ArtifactKey: "supplemental_graph.pack.b"},
	}
	da, err := GraphInputSnapshotDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := GraphInputSnapshotDigest(b)
	if err != nil {
		t.Fatal(err)
	}
	if da != db {
		t.Fatal("snapshot digest must be order-independent")
	}
}

func TestGraphInputSnapshotDigestSelfExcluding(t *testing.T) {
	s := validSnapshot()
	d, err := GraphInputSnapshotDigest(s)
	if err != nil {
		t.Fatal(err)
	}
	s.SnapshotDigestSHA256 = d
	d2, err := GraphInputSnapshotDigest(s)
	if err != nil {
		t.Fatal(err)
	}
	if d != d2 {
		t.Fatal("stamping the digest must not change it")
	}
	if err := ValidateGraphInputSnapshot(s); err != nil {
		t.Fatalf("stamped snapshot must validate: %v", err)
	}
}

// Empty supplemental set is an explicit, valid state — never inferred from absence.
func TestGraphInputSnapshotEmptySupplementalValid(t *testing.T) {
	s := validSnapshot()
	if err := ValidateGraphInputSnapshot(s); err != nil {
		t.Fatalf("empty supplemental snapshot must validate: %v", err)
	}
}

func TestGraphInputSnapshotRejections(t *testing.T) {
	cases := map[string]func(*GraphInputSnapshot){
		"empty policy":  func(s *GraphInputSnapshot) { s.PolicyID = "" },
		"empty domain":  func(s *GraphInputSnapshot) { s.RepositoryDomain = "" },
		"no roots":      func(s *GraphInputSnapshot) { s.SourceRoots = nil },
		"absolute root": func(s *GraphInputSnapshot) { s.SourceRoots = []LogicalSourceRoot{{LogicalPath: "/etc"}} },
		"escaping root": func(s *GraphInputSnapshot) { s.SourceRoots = []LogicalSourceRoot{{LogicalPath: "../x"}} },
		"wrong schema":  func(s *GraphInputSnapshot) { s.SchemaVersion = "other" },
		"bad supplemental": func(s *GraphInputSnapshot) {
			s.SupplementalGraphs = []SupplementalGraphBinding{{ID: "p", Version: "v1", SemanticDigestSHA256: "short", ArtifactKey: "k"}}
		},
		"supplemental no key": func(s *GraphInputSnapshot) {
			s.SupplementalGraphs = []SupplementalGraphBinding{{ID: "p", Version: "v1", SemanticDigestSHA256: supDigest}}
		},
	}
	for name, mut := range cases {
		s := validSnapshot()
		mut(&s)
		if err := ValidateGraphInputSnapshot(s); err == nil {
			t.Fatalf("%s: expected validation failure", name)
		}
	}
}

func TestGraphInputSnapshotDuplicatesRejected(t *testing.T) {
	s := validSnapshot()
	s.SourceRoots = []LogicalSourceRoot{{LogicalPath: "docs/awareness"}, {LogicalPath: "docs/awareness"}}
	if _, err := CanonicalizeGraphInputSnapshot(s); err == nil {
		t.Fatal("duplicate source root must be rejected")
	}
	s = validSnapshot()
	s.SupplementalGraphs = []SupplementalGraphBinding{
		{ID: "p", Version: "v1", SemanticDigestSHA256: supDigest, ArtifactKey: "k1"},
		{ID: "p", Version: "v2", SemanticDigestSHA256: supDigest, ArtifactKey: "k2"},
	}
	if _, err := CanonicalizeGraphInputSnapshot(s); err == nil {
		t.Fatal("duplicate supplemental id must be rejected")
	}
}

func TestGraphInputSnapshotTamperedDigestRejected(t *testing.T) {
	s := validSnapshot()
	s.SnapshotDigestSHA256 = supDigest // wrong
	if err := ValidateGraphInputSnapshot(s); err == nil {
		t.Fatal("a snapshot whose digest does not recompute must be rejected")
	}
}
