// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/architecture/knowledgeadmission"
	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/seedmeta"
)

// admissionScope resolves the authoritative admission decision for a corpus.
//
// Returns nil when admission cannot be established, which the validators surface
// as UNAVAILABLE. Returning an empty scope instead would silently narrow every
// gate to nothing while still reporting success.
func admissionScope(inputs []string) extractor.AdmissionScope {
	if len(inputs) == 0 {
		return nil
	}
	root := repoRootFor(inputs[0])
	admitted, _, err := knowledgeadmission.LoadFromRepo(knowledgeadmission.LoadOptions{
		RepoRoot:    root,
		GraphDigest: seedGraphDigest(root),
		EvaluatedAt: time.Now().UTC(),
		Index:       policyIndexFor(root),
	})
	if err != nil {
		return nil
	}
	return admitted
}

func repoRootFor(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	for {
		if info, serr := os.Stat(filepath.Join(abs, ".git")); serr == nil && (info.IsDir() || info.Mode().IsRegular()) {
			return abs
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return dir
		}
		abs = parent
	}
}

func policyIndexFor(root string) authority.PolicyIndex {
	idx, err := authority.LoadPolicyIndex(root)
	if err != nil {
		return authority.PolicyIndex{}
	}
	return idx
}

// seedGraphDigest reads the digest the embedded seed was built at, so admission
// binds to the corpus actually published rather than to a caller's assertion.
func seedGraphDigest(repoRoot string) string {
	raw, err := os.ReadFile(filepath.Join(repoRoot, "golang", "server", "embeddata", "awareness.nt"))
	if err != nil {
		return ""
	}
	var kept []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, "seedBuild/sha256-") {
			continue
		}
		kept = append(kept, line)
	}
	_, marker := seedmeta.AppendMarker([]byte(strings.Join(kept, "\n")))
	return marker.Digest
}
