// SPDX-License-Identifier: Apache-2.0

package coldsource

import "testing"

func TestIsExcludedSurface(t *testing.T) {
	excluded := []string{
		"go.mod", "go.sum", "go.work", "go.work.sum",
		"vendor/modules.txt", "package-lock.json", "yarn.lock", "Cargo.lock",
		"vendor/github.com/foo/bar/baz.go",
		"third_party/x/y.go",
		"contrib/raftexample/raft.go", // etcd noise case
		"examples/demo/main.go", "example/x.go",
		"server/foo_test.go has testdata? no", // sanity: not a path, must be false
		"api/v3/etcdserverpb/rpc.pb.go",       // generated
		"server/storage/schema/schema_generated.go",
		"pkg/mock_client.go",
		"zz_generated.deepcopy.go",
	}
	// The "sanity" entry above is actually a non-path string; treat it specially.
	for _, p := range excluded {
		if p == "server/foo_test.go has testdata? no" {
			if isExcludedSurface(p) {
				t.Errorf("a prose string with 'testdata' substring must NOT be excluded by word: %q", p)
			}
			continue
		}
		if !isExcludedSurface(p) {
			t.Errorf("isExcludedSurface(%q) = false, want true", p)
		}
	}

	included := []string{
		"", // empty is not a surface
		"modules/caddyhttp/encode/encode.go",
		"server/etcdserver/raft.go",
		"client/v3/client.go",
		"caddyconfig/httpcaddyfile/options.go",
		"go_real_source.go",      // not the manifest
		"server/contribution.go", // "contrib" substring but not a path segment
	}
	for _, p := range included {
		if isExcludedSurface(p) {
			t.Errorf("isExcludedSurface(%q) = true, want false", p)
		}
	}
}

// REGRESSION (Caddy): a go.mod dependency-churn revert plus a go.mod PR comment
// must NOT triangulate into a standalone candidate theme. This is the exact
// noise candidate the live Caddy run produced before the surface exclusion.
func TestNoCandidateTheme_CaddyGoModDependencyChurn(t *testing.T) {
	// Revert commit touching only go.mod + go.sum.
	revertSignals := ExtractReverts([]CommitRecord{{
		SHA: "dep1", Subject: "go.mod: Upgrade CertMagic",
		Files: []string{"go.mod", "go.sum"},
	}})
	// PR comment anchored on go.mod (the "data races now solved" style note).
	prSignals := ExtractPRReviews([]ReviewComment{{
		PRID: "7244", CommentID: "1", Path: "go.mod", Line: 56,
		Body: "this indirect bump is required, the data races are now solved",
	}})

	eligible, _ := Triangulate(append(revertSignals, prSignals...))
	for _, b := range eligible {
		if b.ThemeKey == "go" || b.ThemeKey == "go.mod" || b.ThemeKey == "go.sum" {
			t.Fatalf("dependency-manifest churn must not form a standalone theme; got eligible %q", b.ThemeKey)
		}
	}
}

// REGRESSION (etcd): import/build churn in contrib/raftexample must NOT form a
// candidate theme — it's bundled example code, not the product's architecture.
// This is the exact low-confidence noise candidate the live etcd run produced.
func TestNoCandidateTheme_EtcdRaftexampleImportChurn(t *testing.T) {
	revertSignals := ExtractReverts([]CommitRecord{
		{SHA: "ic1", Subject: "Revert module import paths", Files: []string{"contrib/raftexample/raft.go"}},
		{SHA: "ic2", Subject: `Revert "internal/raftsnap"`, Files: []string{"contrib/raftexample/raft.go"}},
	})
	prSignals := ExtractPRReviews([]ReviewComment{{
		PRID: "1", CommentID: "9", Path: "contrib/raftexample/raft_test.go", Line: 131,
		Body: "avoid custom code here, must reuse the helper",
	}})

	eligible, _ := Triangulate(append(revertSignals, prSignals...))
	for _, b := range eligible {
		if b.ThemeKey == "contrib.raftexample.raft" {
			t.Fatalf("contrib/raftexample example/import churn must not form a candidate theme; got %q", b.ThemeKey)
		}
	}
}

// A commit touching BOTH an excluded surface and a REAL source file must still
// produce the real file's theme — exclusion withholds standalone dep themes, it
// does not suppress the real architecture signal in the same commit.
func TestExcludedSurface_RealSourceInSameCommitSurvives(t *testing.T) {
	revertSignals := ExtractReverts([]CommitRecord{{
		SHA: "mix1", Subject: "Revert change that broke server",
		Files: []string{"go.mod", "server/etcdserver/server.go"},
	}})
	prSignals := ExtractPRReviews([]ReviewComment{{
		PRID: "1", CommentID: "1", Path: "server/etcdserver/server.go", Line: 1894,
		Body: "do not dereference the proto struct, it copies the embedded mutex",
	}})

	eligible, _ := Triangulate(append(revertSignals, prSignals...))
	var found bool
	for _, b := range eligible {
		if b.ThemeKey == "server.etcdserver.server" {
			found = true
		}
		if b.ThemeKey == "go" {
			t.Fatalf("go.mod must not seed a theme even alongside a real source file")
		}
	}
	if !found {
		t.Fatalf("real source theme server.etcdserver.server must survive when bundled with an excluded surface")
	}
}

func TestChooseWindowDepth(t *testing.T) {
	ladder := []int{500, 1000, 2000, 4000, 8000}
	cases := []struct {
		name   string
		counts []int
		target int
		want   int // index into ladder
	}{
		// Caddy-like: dense scars, smallest window already meets target.
		{"dense_smallest_window", []int{10, 18, 28, 50, 84}, 8, 0},
		// etcd-like: sparse at 500, meets target at 2000.
		{"sparse_widen_to_2000", []int{1, 4, 9, 20, 40}, 8, 2},
		// Never meets target → use largest scanned window.
		{"never_meets_use_max", []int{1, 2, 3, 4, 5}, 8, 4},
		// Exactly meets at the last entry.
		{"meets_at_last", []int{1, 2, 3, 4, 8}, 8, 4},
		// Fewer counts than ladder (history shorter): use last available.
		{"short_history", []int{1, 2}, 8, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := chooseWindowDepth(ladder, c.counts, c.target); got != c.want {
				t.Errorf("chooseWindowDepth(%v, target=%d) = %d, want %d", c.counts, c.target, got, c.want)
			}
		})
	}
}
