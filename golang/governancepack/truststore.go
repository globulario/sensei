// SPDX-License-Identifier: AGPL-3.0-only

package governancepack

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

type RotationFinding struct {
	PublisherID string
	KeyID       string
	Severity    string
	Message     string
}

func WriteTrustStore(path string, store TrustStore) error {
	if err := store.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepathDir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir trust store dir: %w", err)
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal trust store: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write trust store temp file: %w", err)
	}
	return os.Rename(tmp, path)
}

func MergeTrustStore(existing, incoming TrustStore) (TrustStore, error) {
	if strings.TrimSpace(existing.SchemaVersion) == "" {
		existing.SchemaVersion = TrustStoreSchemaV1
	}
	if existing.SchemaVersion != TrustStoreSchemaV1 || incoming.SchemaVersion != TrustStoreSchemaV1 {
		return TrustStore{}, fmt.Errorf("%w: trust-store schema mismatch", ErrTrustStoreInvalid)
	}
	index := map[string]TrustedPublisher{}
	for _, p := range existing.Publishers {
		index[p.PublisherID] = p
	}
	for _, p := range incoming.Publishers {
		cur := index[p.PublisherID]
		cur.PublisherID = p.PublisherID
		if strings.TrimSpace(p.DisplayName) != "" {
			cur.DisplayName = p.DisplayName
		}
		keyIdx := map[string]TrustedKey{}
		for _, k := range cur.Keys {
			keyIdx[k.KeyID+"|"+strings.ToLower(k.Algorithm)] = k
		}
		for _, k := range p.Keys {
			keyIdx[k.KeyID+"|"+strings.ToLower(k.Algorithm)] = k
		}
		cur.Keys = make([]TrustedKey, 0, len(keyIdx))
		for _, k := range keyIdx {
			cur.Keys = append(cur.Keys, k)
		}
		sort.Slice(cur.Keys, func(i, j int) bool { return cur.Keys[i].KeyID < cur.Keys[j].KeyID })
		index[p.PublisherID] = cur
	}
	out := TrustStore{SchemaVersion: TrustStoreSchemaV1}
	for _, p := range index {
		out.Publishers = append(out.Publishers, p)
	}
	sort.Slice(out.Publishers, func(i, j int) bool { return out.Publishers[i].PublisherID < out.Publishers[j].PublisherID })
	if err := out.Validate(); err != nil {
		return TrustStore{}, err
	}
	return out, nil
}

func RotationCheck(store TrustStore, active *ActiveRecord, now time.Time) []RotationFinding {
	var out []RotationFinding
	for _, p := range store.Publishers {
		for _, k := range p.Keys {
			status := normalizedKeyStatus(k.Status)
			switch status {
			case "revoked":
				out = append(out, RotationFinding{PublisherID: p.PublisherID, KeyID: k.KeyID, Severity: "critical", Message: "key is revoked"})
			case "deprecated":
				out = append(out, RotationFinding{PublisherID: p.PublisherID, KeyID: k.KeyID, Severity: "warning", Message: "key is deprecated"})
			case "future":
				out = append(out, RotationFinding{PublisherID: p.PublisherID, KeyID: k.KeyID, Severity: "info", Message: "key is staged for future use"})
			}
			if k.ValidUntil != "" {
				if ts, err := time.Parse(time.RFC3339, k.ValidUntil); err == nil && now.After(ts) {
					out = append(out, RotationFinding{PublisherID: p.PublisherID, KeyID: k.KeyID, Severity: "critical", Message: "key has expired"})
				}
			}
		}
	}
	if active != nil {
		foundPublisher := false
		for _, p := range store.Publishers {
			if p.PublisherID == active.PublisherID {
				foundPublisher = true
				break
			}
		}
		if !foundPublisher {
			out = append(out, RotationFinding{PublisherID: active.PublisherID, Severity: "critical", Message: "active pack publisher is no longer trusted"})
		}
	}
	return out
}

func filepathDir(path string) string {
	i := strings.LastIndex(path, string(os.PathSeparator))
	if i <= 0 {
		return "."
	}
	return path[:i]
}
