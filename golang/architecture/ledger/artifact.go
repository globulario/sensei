// SPDX-License-Identifier: Apache-2.0

package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"gopkg.in/yaml.v3"
)

type renderedPayload struct {
	semanticDigest string
	byteDigest     string
	mediaType      string
	path           string
	data           []byte
}

func renderPayload(payload any, mediaType string) (renderedPayload, error) {
	ext, err := mediaTypeExtension(mediaType)
	if err != nil {
		return renderedPayload{}, err
	}
	switch v := payload.(type) {
	case []byte:
		sum := sha256.Sum256(v)
		digest := hex.EncodeToString(sum[:])
		return renderedPayload{
			semanticDigest: digest,
			byteDigest:     digest,
			mediaType:      mediaType,
			path:           filepath.ToSlash(filepath.Join("artifacts", "sha256", digest+ext)),
			data:           append([]byte(nil), v...),
		}, nil
	case string:
		return renderPayload([]byte(v), mediaType)
	default:
		semanticDigest, err := closureprotocol.SemanticDigest(payload)
		if err != nil {
			return renderedPayload{}, err
		}
		var data []byte
		switch strings.TrimSpace(mediaType) {
		case "application/json":
			data, err = closureprotocol.CanonicalJSON(payload)
		case "application/yaml", "text/yaml", "application/x-yaml":
			data, err = binding.CanonicalYAML(payload)
		default:
			err = fmt.Errorf("structured payload requires json or yaml media type")
		}
		if err != nil {
			return renderedPayload{}, err
		}
		sum := sha256.Sum256(data)
		return renderedPayload{
			semanticDigest: semanticDigest,
			byteDigest:     hex.EncodeToString(sum[:]),
			mediaType:      mediaType,
			path:           filepath.ToSlash(filepath.Join("artifacts", "sha256", semanticDigest+ext)),
			data:           data,
		}, nil
	}
}

func semanticDigestForBytes(mediaType string, data []byte) (string, error) {
	return semanticDigestForBytesCtx(context.Background(), mediaType, data)
}

// semanticDigestForBytesCtx computes the semantic digest of a payload, reusing an
// evaluation-scoped memo when ctx carries one. The result is identical to the
// non-memoized computation for the same bytes; the memo only avoids re-parsing and
// re-canonicalizing bytes already digested in this evaluation. A parse failure is
// never memoized, so a malformed payload still surfaces its error on every call.
func semanticDigestForBytesCtx(ctx context.Context, mediaType string, data []byte) (string, error) {
	mt := strings.TrimSpace(mediaType)
	structured := mt == "application/yaml" || mt == "text/yaml" || mt == "application/x-yaml" || mt == "application/json"
	scope := scopeFrom(ctx)
	var key string
	if scope != nil && structured {
		key = digestMemoKey(mt, data)
		if d, ok := scope.lookup(key); ok {
			return d, nil
		}
	}
	digest, err := computeSemanticDigest(mt, data)
	if err != nil {
		return "", err
	}
	if scope != nil && structured {
		scope.store(key, digest)
	}
	return digest, nil
}

func computeSemanticDigest(mediaType string, data []byte) (string, error) {
	switch mediaType {
	case "application/yaml", "text/yaml", "application/x-yaml":
		var value any
		if err := yaml.Unmarshal(data, &value); err != nil {
			return "", err
		}
		return closureprotocol.SemanticDigest(value)
	case "application/json":
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return "", err
		}
		return closureprotocol.SemanticDigest(value)
	default:
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:]), nil
	}
}

func storePayloadArtifacts(taskDir string, payload renderedPayload) error {
	target := filepath.Join(taskDir, filepath.FromSlash(payload.path))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if existing, err := os.ReadFile(target); err == nil {
		if string(existing) != string(payload.data) {
			return fmt.Errorf("artifact digest collision for %s", payload.path)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeFileAtomic(target, payload.data)
}

func (s *Store) StoreArtifactBytes(data []byte, mediaType string) (closureprotocol.LedgerPayloadRef, error) {
	rendered, err := renderPayload(data, mediaType)
	if err != nil {
		return closureprotocol.LedgerPayloadRef{}, err
	}
	if err := storePayloadArtifacts(s.taskDir, rendered); err != nil {
		return closureprotocol.LedgerPayloadRef{}, err
	}
	return closureprotocol.LedgerPayloadRef{
		Path:         rendered.path,
		MediaType:    rendered.mediaType,
		DigestSHA256: rendered.semanticDigest,
	}, nil
}

func (s *Store) StoreArtifactFile(path, mediaType string) (closureprotocol.LedgerPayloadRef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return closureprotocol.LedgerPayloadRef{}, err
	}
	return s.StoreArtifactBytes(data, mediaType)
}
