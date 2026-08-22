// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/prospective"
)

// governingClasses are the classes that make up the eligible corpus.
var governingClasses = []string{"invariant", "failure_mode", "forbidden_fix", "incident_pattern", "contract", "meta_principle"}

// resolveClasses maps a class name to the argument `sensei resolve` takes.
var resolveClasses = map[string]string{
	"invariant":        "Invariant",
	"failure_mode":     "FailureMode",
	"forbidden_fix":    "ForbiddenFix",
	"incident_pattern": "IncidentPattern",
	"contract":         "Contract",
	"meta_principle":   "MetaPrinciple",
}

// Graph runs the production Sensei CLI. It is exec'd rather than linked so the
// evidence this harness classifies with is the same surface an author would
// get, and so the exact invocation can be recorded verbatim in the artifact.
type Graph struct {
	Bin    string
	Addr   string
	Domain string
}

// run invokes one production subcommand. The address and domain flags follow
// the subcommand because that is where the CLI parses them; placing them first
// makes every call print the top-level usage instead, which reads like an
// empty graph rather than like a malformed command.
func (g Graph) run(ctx context.Context, sub string, args ...string) ([]byte, error) {
	full := append([]string{sub, "--addr", g.Addr, "--domain", g.Domain}, args...)
	// Direct argv, never a shell.
	cmd := exec.CommandContext(ctx, g.Bin, full...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", g.Bin, strings.Join(full, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

type graphNode struct {
	ID         string   `json:"id"`
	Class      string   `json:"class"`
	Label      string   `json:"label"`
	Severity   string   `json:"severity"`
	Status     string   `json:"status"`
	RelatedIDs []string `json:"related_ids"`
	Facts      []struct {
		Predicate string `json:"predicate"`
		Value     string `json:"value"`
	} `json:"facts"`
}

type queryResult struct {
	Rows []struct {
		ID    string `json:"id"`
		Class string `json:"class"`
	} `json:"rows"`
}

type resolveResult struct {
	Node  graphNode `json:"node"`
	Found bool      `json:"found"`
}

// BuildCorpus freezes the eligible knowledge corpus.
//
// Anchors are carried on each item because the corpus must be reproducible,
// and they are the one field that never reaches an adjudicator — see
// prospective.BlindCorpusItem.
func (g Graph) BuildCorpus(ctx context.Context, graphDigest string, limit int) (prospective.Corpus, error) {
	var items []prospective.CorpusItem
	var unresolved []string
	for _, class := range governingClasses {
		raw, err := g.run(ctx, "query", "--mode", "by_class", "--class", class, "--limit", fmt.Sprint(limit), "--json")
		if err != nil {
			return prospective.Corpus{}, fmt.Errorf("corpus class %s: %w", class, err)
		}
		var res queryResult
		if err := json.Unmarshal(raw, &res); err != nil {
			return prospective.Corpus{}, fmt.Errorf("corpus class %s: %w", class, err)
		}
		for _, row := range res.Rows {
			id := strings.TrimPrefix(row.ID, class+":")
			item := prospective.CorpusItem{ID: row.ID, Class: row.Class}
			if rc, ok := resolveClasses[class]; ok {
				node, err := g.resolve(ctx, rc, id)
				if err != nil {
					// Recorded, not swallowed. An unreadable item still bounds
					// the denominator, and a human has to know it was in the
					// pile they could not judge.
					unresolved = append(unresolved, row.ID)
				} else {
					item.Title = node.Label
					item.Statement = statementOf(node)
					item.Anchors = anchorsOf(node)
				}
			} else {
				unresolved = append(unresolved, row.ID)
			}
			items = append(items, item)
		}
	}
	return prospective.NormalizeCorpus(prospective.Corpus{
		RepositoryDomain:  g.Domain,
		GraphDigestSHA256: graphDigest,
		ProducedBy:        fmt.Sprintf("%s query --addr %s --domain %s --mode by_class --class {%s} --json (+ resolve per node)", g.Bin, g.Addr, g.Domain, strings.Join(governingClasses, ",")),
		Items:             items,
		UnresolvedIDs:     unresolved,
	})
}

func (g Graph) resolve(ctx context.Context, class, id string) (graphNode, error) {
	raw, err := g.run(ctx, "resolve", "--json", class, id)
	if err != nil {
		return graphNode{}, err
	}
	var res resolveResult
	if err := json.Unmarshal(raw, &res); err != nil || !res.Found {
		return graphNode{}, fmt.Errorf("resolve %s %s: not found", class, id)
	}
	return res.Node, nil
}

func statementOf(n graphNode) string {
	var parts []string
	for _, f := range n.Facts {
		if f.Predicate == "status" {
			continue
		}
		parts = append(parts, f.Predicate+": "+f.Value)
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}

// anchorsOf keeps only the file anchors, which is what the blinding rule is
// about: the graph's own account of which files an item governs.
func anchorsOf(n graphNode) []string {
	var out []string
	for _, r := range n.RelatedIDs {
		if strings.HasPrefix(r, "source_file:") {
			out = append(out, strings.TrimPrefix(r, "source_file:"))
		}
	}
	sort.Strings(out)
	return out
}
