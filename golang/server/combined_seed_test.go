// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"testing"
)

// requireCombinedSeed skips tests whose golden fixtures assert Globular/services
// content (doctor remediation, RBAC, repository publish, workflow resume) that
// exists only in the COMBINED awareness-graph + services seed built by
// scripts/build-awareness-graph.sh.
//
// The standalone / open-source build ships a self-only seed
// (scripts/build-awareness-graph-self.sh) that deliberately omits services
// content, so these tests skip there and run in the combined build. It is a
// runtime guard rather than a //go:build tag because several of these tests
// live in files that also hold generic tests and share helpers with them —
// a build tag would need a risky three-way file split.
func requireCombinedSeed(t *testing.T) {
	t.Helper()
	if !bytes.Contains(seedNT, []byte("authority.repository_artifact_metadata")) {
		t.Skip("combined-seed golden: standalone seed omits Globular/services content")
	}
}
