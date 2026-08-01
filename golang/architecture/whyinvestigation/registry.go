// SPDX-License-Identifier: AGPL-3.0-only

package whyinvestigation

import (
	"encoding/json"
	"sort"
	"sync"

	"github.com/globulario/sensei/golang/architecture/investigation"
)

var (
	registryMu sync.RWMutex
	registry   = make(map[string]func(root string) Provider)
)

func Register(id string, factory func(root string) Provider) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[id] = factory
}

func GetProvider(id string, root string) (Provider, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	factory, ok := registry[id]
	if !ok {
		return nil, false
	}
	return factory(root), true
}

func computeSnapshotDigest(snap *Snapshot) (string, error) {
	// Sort entries by SourceIdentity to be completely deterministic
	sort.Slice(snap.Entries, func(i, j int) bool {
		return snap.Entries[i].SourceIdentity < snap.Entries[j].SourceIdentity
	})

	// Sort commits to be stable
	sort.Slice(snap.Commits, func(i, j int) bool {
		return snap.Commits[i].ID < snap.Commits[j].ID
	})

	// Create a stable descriptor representing the entire snapshot
	descriptor := struct {
		Provider       investigation.ProviderBinding  `json:"provider"`
		Category       investigation.EvidenceCategory `json:"category"`
		Entries        []SnapshotEntry                `json:"entries"`
		RequestedRange GitRange                       `json:"requested_range"`
		ResolvedRange  GitRange                       `json:"resolved_range"`
		Incomplete     bool                           `json:"incomplete"`
		Commits        []Commit                       `json:"commits"`
	}{
		Provider:       snap.Provider,
		Category:       snap.Category,
		Entries:        snap.Entries,
		RequestedRange: snap.RequestedRange,
		ResolvedRange:  snap.ResolvedRange,
		Incomplete:     snap.Incomplete,
		Commits:        snap.Commits,
	}

	bytes, err := json.Marshal(descriptor)
	if err != nil {
		return "", err
	}
	return investigation.SHA256Bytes(bytes), nil
}

func init() {
	Register(GitProviderID, func(root string) Provider {
		return GitProvider{Root: root}
	})
	Register("documentation_provider", func(root string) Provider {
		return DocumentationProvider{Root: root}
	})
	Register("tests_provider", func(root string) Provider {
		return TestsProvider{Root: root}
	})
	Register("awareness_provider", func(root string) Provider {
		return AwarenessProvider{Root: root}
	})
	Register("scars_provider", func(root string) Provider {
		return ScarsProvider{Root: root}
	})
	Register("architect_answers_provider", func(root string) Provider {
		return ArchitectAnswersProvider{Root: root}
	})
	Register("imported_evidence_provider", func(root string) Provider {
		return ImportedEvidenceProvider{Root: root}
	})
}
