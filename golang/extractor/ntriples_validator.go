// SPDX-License-Identifier: AGPL-3.0-only

package extractor

// N-Triples validator. Lifted from the /tmp/oxigraph-spike code that
// caught two real bugs in the importer (prose IRIs and unencoded
// `{template}` placeholders in etcd keys) — now part of the package so
// every importer test can re-use it.
//
// Coverage: the validator checks the surface-level mistakes a triple
// emitter is most likely to make. It is NOT a full W3C N-Triples spec
// validator — there is no Unicode escape handling, no language-tag or
// datatype-IRI structural validation. The point is to fail loudly when
// the importer produces something Oxigraph would reject, not to certify
// conformance.
//
// What it catches:
//   - missing trailing '.'
//   - wrong number of tokens (S P O expected; not 2, not 4)
//   - IRI with whitespace or any character W3C disallows in IRIREF
//   - subject that is not an IRI
//   - predicate that is not an IRI
//   - object that is neither IRI nor literal
//   - unbalanced literal quotes
//   - whitespace after the closing literal quote (a lang tag / datatype
//     suffix MAY follow but must contain no whitespace)
//
// What it deliberately does NOT catch:
//   - semantic correctness (whether the predicate is a known aw: term)
//   - whether targets resolve to a typed node
//
// Those are the job of the drift / predicate-audit tests in this
// package; combining them with grammar checks here would obscure which
// layer failed.

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// ValidationError describes one violation found by the validator.
// LineNum is 1-indexed.
type ValidationError struct {
	LineNum int
	Msg     string
	Src     string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("line %d: %s\n    > %s", e.LineNum, e.Msg, e.Src)
}

// ValidateNTriples reads N-Triples from r and returns one error per
// invalid line. Empty lines and lines starting with '#' are ignored
// (the N-Triples spec allows comments). Returns nil when input is clean.
// Reading errors are surfaced via the returned errs slice rather than a
// separate error return — the caller usually wants the partial result.
func ValidateNTriples(r io.Reader) []ValidationError {
	var errs []ValidationError
	sc := bufio.NewScanner(r)
	// Bump the line buffer: some YAML-derived triples carry long
	// label/comment literals that exceed the 64 KiB default.
	sc.Buffer(make([]byte, 1<<20), 1<<20)

	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := sc.Text()
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasSuffix(line, ".") {
			errs = append(errs, ValidationError{lineNo, "missing trailing '.'", raw})
			continue
		}
		body := strings.TrimSpace(strings.TrimSuffix(line, "."))
		toks := tokenize(body)
		if len(toks) != 3 {
			errs = append(errs, ValidationError{lineNo, fmt.Sprintf("expected 3 tokens (S P O), got %d", len(toks)), raw})
			continue
		}
		if !isIRI(toks[0]) {
			errs = append(errs, ValidationError{lineNo, "subject is not a valid IRI: " + truncate(toks[0]), raw})
		}
		if !isIRI(toks[1]) {
			errs = append(errs, ValidationError{lineNo, "predicate is not a valid IRI: " + truncate(toks[1]), raw})
		}
		if !isIRI(toks[2]) && !isLiteral(toks[2]) {
			errs = append(errs, ValidationError{lineNo, "object is neither IRI nor literal: " + truncate(toks[2]), raw})
		}
	}
	if err := sc.Err(); err != nil {
		errs = append(errs, ValidationError{lineNo, "scan: " + err.Error(), ""})
	}
	return errs
}

// tokenize splits a triple body into its three tokens. IRIs (<...>) and
// literals ("...") may contain whitespace inside their delimiters;
// backslash escapes inside literals are honored.
func tokenize(s string) []string {
	var toks []string
	var cur strings.Builder
	const (
		stBetween   = 0
		stInIRI     = 1
		stInLiteral = 2
		stAfterLit  = 3 // after closing quote, may carry lang/datatype suffix
		stBareToken = 4 // bnode label or unexpected bare identifier
	)
	state := stBetween
	escape := false
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch state {
		case stBetween:
			if c == ' ' || c == '\t' {
				continue
			}
			cur.WriteByte(c)
			switch c {
			case '<':
				state = stInIRI
			case '"':
				state = stInLiteral
			default:
				state = stBareToken
			}
		case stInIRI:
			cur.WriteByte(c)
			if c == '>' {
				flush()
				state = stBetween
			}
		case stInLiteral:
			cur.WriteByte(c)
			if escape {
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == '"' {
				state = stAfterLit
			}
		case stAfterLit:
			if c == ' ' || c == '\t' {
				flush()
				state = stBetween
			} else {
				cur.WriteByte(c)
			}
		case stBareToken:
			if c == ' ' || c == '\t' {
				flush()
				state = stBetween
			} else {
				cur.WriteByte(c)
			}
		}
	}
	flush()
	return toks
}

// isIRI checks the W3C N-Triples IRIREF grammar minus Unicode escape
// handling. Whitespace, '<', '>', '"', '{', '}', '|', '^', '`', and '\'
// are explicitly disallowed inside an IRI; everything else is accepted.
func isIRI(t string) bool {
	if len(t) < 2 || t[0] != '<' || t[len(t)-1] != '>' {
		return false
	}
	inner := t[1 : len(t)-1]
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if c <= 0x20 || c == '<' || c == '>' || c == '"' || c == '{' || c == '}' ||
			c == '|' || c == '^' || c == '`' || c == '\\' {
			return false
		}
	}
	return true
}

// isLiteral checks that the token is a properly quoted string literal,
// optionally followed by a lang tag (@en) or datatype IRI (^^<...>).
// Inner-quote escapes (\") are honored; the suffix is required to be
// whitespace-free.
func isLiteral(t string) bool {
	if len(t) < 2 || t[0] != '"' {
		return false
	}
	escape := false
	closed := -1
	for i := 1; i < len(t); i++ {
		c := t[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' {
			escape = true
			continue
		}
		if c == '"' {
			closed = i
			break
		}
	}
	if closed < 0 {
		return false
	}
	suffix := t[closed+1:]
	for i := 0; i < len(suffix); i++ {
		if suffix[i] <= 0x20 {
			return false
		}
	}
	return true
}

func truncate(s string) string {
	const max = 80
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
