// SPDX-License-Identifier: AGPL-3.0-only

package knowledgeadmission

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const admissionCorpusDigestDomain = "sensei/knowledge-admission-corpus/v1"

// AdmissionCorpusDigest computes the canonical digest of the authored source
// declarations for the identities that are authoritatively admitted.
//
// This is the trust anchor for knowledge admission. It is deliberately upstream
// of publication: changing how yaml2nt filters or serializes the published graph
// must not perturb the admission binding.
//
// The corpus semantics are pinned here, once, for both freezing and verification:
//   - domain membership is docs/awareness beneath repoRoot;
//   - generated/ subtrees are build output and never authored admission input;
//   - candidates/ has no special treatment: pathname never grants or removes
//     authority; only the supplied identity set decides which declarations bind;
//   - pathnames do not participate in the digest;
//   - YAML formatting, comments, anchors, aliases and mapping-key order do not
//     participate; aliases are canonicalized as the semantic node they reference;
//   - sequence order and scalar/tag values do participate;
//   - duplicate declarations are retained as distinct declarations and therefore
//     affect the digest, even when byte-for-byte semantically identical;
//   - declaration ordering across files does not participate.
//
// Every requested identity must have at least one authored declaration. An
// admitted identity that exists only in generated/ output, or is absent from the
// authored corpus, makes the binding unprovable rather than silently disappearing.
func AdmissionCorpusDigest(repoRoot string, authoritativeIdentities []string) (string, error) {
	root := strings.TrimSpace(repoRoot)
	if root == "" {
		return "", fmt.Errorf("admission corpus digest: repository root is empty")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("admission corpus digest: repository root: %w", err)
	}
	corpusRoot := filepath.Join(root, "docs", "awareness")
	info, err := os.Stat(corpusRoot)
	if err != nil {
		return "", fmt.Errorf("admission corpus digest: corpus root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("admission corpus digest: %s is not a directory", corpusRoot)
	}

	wanted := make(map[string]struct{}, len(authoritativeIdentities))
	for _, raw := range authoritativeIdentities {
		id := strings.TrimSpace(raw)
		if id == "" {
			return "", fmt.Errorf("admission corpus digest: authoritative identity is empty")
		}
		wanted[id] = struct{}{}
	}
	if len(wanted) == 0 {
		return "", fmt.Errorf("admission corpus digest: authoritative identity set is empty")
	}

	type declaration struct {
		identity string
		schema   string
		body     []byte
	}
	var declarations []declaration
	seen := make(map[string]int, len(wanted))

	err = filepath.WalkDir(corpusRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path != corpusRoot && d.Name() == "generated" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isYAMLPath(path) {
			return nil
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		for {
			var doc yaml.Node
			if err := dec.Decode(&doc); err != nil {
				if err == io.EOF {
					break
				}
				rel, _ := filepath.Rel(root, path)
				return fmt.Errorf("parse %s: %w", filepath.ToSlash(rel), err)
			}
			for _, found := range authoredDeclarations(&doc, wanted) {
				declarations = append(declarations, declaration{
					identity: found.identity,
					schema:   found.schema,
					body:     found.body,
				})
				seen[found.identity]++
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("admission corpus digest: %w", err)
	}

	var missing []string
	for id := range wanted {
		if seen[id] == 0 {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return "", fmt.Errorf("admission corpus digest: %d authoritative identities have no authored declaration: %s", len(missing), strings.Join(missing, ", "))
	}

	sort.Slice(declarations, func(i, j int) bool {
		if declarations[i].identity != declarations[j].identity {
			return declarations[i].identity < declarations[j].identity
		}
		if declarations[i].schema != declarations[j].schema {
			return declarations[i].schema < declarations[j].schema
		}
		return bytes.Compare(declarations[i].body, declarations[j].body) < 0
	})

	h := sha256.New()
	writeDigestField(h, []byte(admissionCorpusDigestDomain))
	var count [8]byte
	binary.BigEndian.PutUint64(count[:], uint64(len(declarations)))
	_, _ = h.Write(count[:])
	for _, d := range declarations {
		writeDigestField(h, []byte(d.identity))
		writeDigestField(h, []byte(d.schema))
		writeDigestField(h, d.body)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func isYAMLPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

type authoredDeclaration struct {
	identity string
	schema   string
	body     []byte
}

func authoredDeclarations(doc *yaml.Node, wanted map[string]struct{}) []authoredDeclaration {
	if doc == nil || len(doc.Content) == 0 {
		return nil
	}
	n := doc.Content[0]
	if n.Kind != yaml.MappingNode {
		return nil
	}
	var out []authoredDeclaration
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, value := n.Content[i], n.Content[i+1]
		if key.Kind != yaml.ScalarNode || value.Kind != yaml.SequenceNode {
			continue
		}
		schema := strings.TrimSpace(key.Value)
		for _, item := range value.Content {
			id := mappingScalar(item, "id")
			if _, ok := wanted[id]; !ok {
				continue
			}
			var buf bytes.Buffer
			writeCanonicalYAMLNode(&buf, item)
			out = append(out, authoredDeclaration{identity: id, schema: schema, body: buf.Bytes()})
		}
	}
	return out
}

func mappingScalar(n *yaml.Node, key string) string {
	if n == nil || n.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind == yaml.ScalarNode && strings.TrimSpace(k.Value) == key && v.Kind == yaml.ScalarNode {
			return strings.TrimSpace(v.Value)
		}
	}
	return ""
}

// writeCanonicalYAMLNode serializes YAML semantics, not source spelling. It
// intentionally ignores comments, style, anchors, aliases and source positions.
func writeCanonicalYAMLNode(w io.Writer, n *yaml.Node) {
	if n == nil {
		writeDigestField(w, []byte("nil"))
		return
	}
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		// Alias spelling and anchor names are serialization choices. Hash the
		// referenced semantic node exactly as if it had been written inline.
		writeCanonicalYAMLNode(w, n.Alias)
		return
	}

	var kind string
	switch n.Kind {
	case yaml.DocumentNode:
		kind = "document"
	case yaml.SequenceNode:
		kind = "sequence"
	case yaml.MappingNode:
		kind = "mapping"
	case yaml.ScalarNode:
		kind = "scalar"
	default:
		kind = fmt.Sprintf("kind:%d", n.Kind)
	}
	writeDigestField(w, []byte(kind))
	writeDigestField(w, []byte(n.Tag))

	switch n.Kind {
	case yaml.ScalarNode:
		writeDigestField(w, []byte(n.Value))
	case yaml.SequenceNode, yaml.DocumentNode:
		var count [8]byte
		binary.BigEndian.PutUint64(count[:], uint64(len(n.Content)))
		_, _ = w.Write(count[:])
		for _, child := range n.Content {
			writeCanonicalYAMLNode(w, child)
		}
	case yaml.MappingNode:
		type pair struct{ key, value []byte }
		pairs := make([]pair, 0, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			var kb, vb bytes.Buffer
			writeCanonicalYAMLNode(&kb, n.Content[i])
			writeCanonicalYAMLNode(&vb, n.Content[i+1])
			pairs = append(pairs, pair{key: kb.Bytes(), value: vb.Bytes()})
		}
		sort.Slice(pairs, func(i, j int) bool {
			if c := bytes.Compare(pairs[i].key, pairs[j].key); c != 0 {
				return c < 0
			}
			return bytes.Compare(pairs[i].value, pairs[j].value) < 0
		})
		var count [8]byte
		binary.BigEndian.PutUint64(count[:], uint64(len(pairs)))
		_, _ = w.Write(count[:])
		for _, p := range pairs {
			writeDigestField(w, p.key)
			writeDigestField(w, p.value)
		}
	}
}

func writeDigestField(w io.Writer, b []byte) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(b)))
	_, _ = w.Write(n[:])
	_, _ = w.Write(b)
}
