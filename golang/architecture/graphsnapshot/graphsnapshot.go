// SPDX-License-Identifier: Apache-2.0

package graphsnapshot

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
)

type Triple struct {
	Subject     string
	Predicate   string
	Object      string
	ObjectIsIRI bool
}

type Receipt struct {
	Path         string
	DigestSHA256 string
	Status       string
	Verified     bool
	Reasons      []Reason
}

type Reason struct {
	Code   string
	Detail string
}

func Load(path string) ([]Triple, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f)
}

func Read(r io.Reader) ([]Triple, error) {
	var triples []Triple
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := sc.Text()
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		t, err := parseNTripleLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		triples = append(triples, t)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return triples, nil
}

func Verify(path, digest, status string) (Receipt, error) {
	status = strings.TrimSpace(status)
	digest = strings.TrimSpace(digest)
	receipt := Receipt{Path: strings.TrimSpace(path), DigestSHA256: digest, Status: status}
	switch status {
	case architecture.GraphDigestResolved:
		if receipt.Path == "" {
			receipt.Reasons = []Reason{{Code: "graphsnapshot.digest_unavailable", Detail: "--graph-nt is required when graph digest is resolved"}}
			return receipt, nil
		}
		if digest == "" {
			receipt.Reasons = []Reason{{Code: "graphsnapshot.digest_unavailable", Detail: "--graph-digest is required when graph digest is resolved"}}
			return receipt, nil
		}
		data, err := os.ReadFile(receipt.Path)
		if err != nil {
			return receipt, err
		}
		sum := sha256.Sum256(data)
		got := hex.EncodeToString(sum[:])
		receipt.DigestSHA256 = got
		if got != digest {
			receipt.Reasons = []Reason{{Code: "graphsnapshot.digest_mismatch", Detail: "graph snapshot digest does not match supplied digest"}}
			return receipt, nil
		}
		receipt.Verified = true
		receipt.Reasons = []Reason{{Code: "graphsnapshot.digest_current", Detail: "graph snapshot digest matches supplied digest"}}
		return receipt, nil
	case architecture.GraphDigestUnavailable, architecture.GraphDigestNotRequested:
		receipt.Reasons = []Reason{{Code: "graphsnapshot.digest_unavailable", Detail: "graph digest status is " + status}}
		return receipt, nil
	default:
		return receipt, fmt.Errorf("graph digest status must be resolved, unavailable, or not_requested")
	}
}

func parseNTripleLine(line string) (Triple, error) {
	if !strings.HasSuffix(line, ".") {
		return Triple{}, fmt.Errorf("missing trailing '.'")
	}
	body := strings.TrimSpace(strings.TrimSuffix(line, "."))
	toks := tokenizeTriple(body)
	if len(toks) != 3 {
		return Triple{}, fmt.Errorf("expected 3 tokens, got %d", len(toks))
	}
	if !validIRI(toks[0]) || !validIRI(toks[1]) {
		return Triple{}, fmt.Errorf("subject and predicate must be IRIs")
	}
	t := Triple{Subject: unwrapIRI(toks[0]), Predicate: unwrapIRI(toks[1])}
	switch {
	case validIRI(toks[2]):
		t.Object = unwrapIRI(toks[2])
		t.ObjectIsIRI = true
	case strings.HasPrefix(toks[2], "\""):
		lit, err := parseLiteral(toks[2])
		if err != nil {
			return Triple{}, err
		}
		t.Object = lit
	default:
		return Triple{}, fmt.Errorf("object must be IRI or literal")
	}
	return t, nil
}

func tokenizeTriple(s string) []string {
	var toks []string
	var cur strings.Builder
	state := 0
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
		case 0:
			if c == ' ' || c == '\t' {
				continue
			}
			cur.WriteByte(c)
			if c == '<' {
				state = 1
			} else if c == '"' {
				state = 2
			} else {
				state = 4
			}
		case 1:
			cur.WriteByte(c)
			if c == '>' {
				flush()
				state = 0
			}
		case 2:
			cur.WriteByte(c)
			if escape {
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == '"' {
				state = 3
			}
		case 3:
			if c == ' ' || c == '\t' {
				flush()
				state = 0
			} else {
				cur.WriteByte(c)
			}
		case 4:
			if c == ' ' || c == '\t' {
				flush()
				state = 0
			} else {
				cur.WriteByte(c)
			}
		}
	}
	flush()
	return toks
}

func validIRI(t string) bool {
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

func unwrapIRI(t string) string { return strings.TrimSuffix(strings.TrimPrefix(t, "<"), ">") }

func parseLiteral(t string) (string, error) {
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
		return "", fmt.Errorf("unterminated literal")
	}
	suffix := t[closed+1:]
	if strings.TrimSpace(suffix) != suffix {
		return "", fmt.Errorf("literal suffix must not contain whitespace")
	}
	raw := t[:closed+1]
	v, err := strconv.Unquote(raw)
	if err != nil {
		return "", err
	}
	return v, nil
}
