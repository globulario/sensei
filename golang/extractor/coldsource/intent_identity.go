// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// MintIntentID creates a durable Sensei-owned identity for model-mined intent.
// The readable slug comes from the title; the hash binds the semantic statement
// and scope so changed propositions do not silently reuse the same id.
func MintIntentID(title, statement string, scope []string) string {
	base := intentIdentitySlug(title)
	if base == "" {
		base = intentIdentitySlug(statement)
	}
	if base == "" {
		base = "untitled"
	}
	if len(base) > 72 {
		base = strings.Trim(base[:72], "_")
	}
	h := sha256.Sum256([]byte(intentIdentityFingerprint(title, statement, scope)))
	return "intent." + base + "." + hex.EncodeToString(h[:])[:10]
}

// IntentSemanticFingerprint is stable for duplicate detection before writing.
func IntentSemanticFingerprint(title, statement string, scope []string) string {
	return intentIdentityFingerprint(title, statement, scope)
}

// ValidIntentID validates the canonical durable id shape. It rejects prefix-only
// ids, empty segments, trailing separators, and segments starting with '-'.
func ValidIntentID(id string) bool {
	if !strings.HasPrefix(id, "intent.") {
		return false
	}
	rest := strings.TrimPrefix(id, "intent.")
	if rest == "" || strings.HasPrefix(rest, ".") || strings.HasSuffix(rest, ".") {
		return false
	}
	for _, seg := range strings.Split(rest, ".") {
		if seg == "" || strings.HasPrefix(seg, "-") || strings.HasPrefix(seg, "_") || strings.HasSuffix(seg, "-") {
			return false
		}
		for _, r := range seg {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func intentIdentityFingerprint(title, statement string, scope []string) string {
	normScope := make([]string, 0, len(scope))
	for _, s := range scope {
		if t := strings.TrimSpace(s); t != "" {
			normScope = append(normScope, t)
		}
	}
	sort.Strings(normScope)
	return strings.Join([]string{
		normalizeIdentityText(title),
		normalizeIdentityText(statement),
		strings.Join(normScope, "\n"),
	}, "\x00")
}

func intentIdentitySlug(s string) string {
	var b strings.Builder
	prevSep := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevSep = false
		case r == '-' || r == '_' || r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if !prevSep && b.Len() > 0 {
				b.WriteByte('_')
				prevSep = true
			}
		default:
			if !prevSep && b.Len() > 0 {
				b.WriteByte('_')
				prevSep = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

func normalizeIdentityText(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}
