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

// AnchorRuleDescription states what "usable anchors" means, because the B/C
// boundary is exactly this definition.
//
// A path is anchored when production `sensei impact` returns at least one
// governing node for it — an invariant, a failure mode, or an architecture
// node. Symbols alone do not count: an indexed file the graph holds no law
// about is precisely stratum B, and counting its symbols as anchors would file
// every indexed file under C and empty the stratum the experiment exists to
// measure.
const AnchorRuleDescription = "" +
	"A path is anchored when `sensei impact --file <path>` returns at least one of " +
	"direct_invariants, direct_failure_modes or direct_architecture. Symbol index entries do not count."

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

func (g Graph) run(ctx context.Context, args ...string) ([]byte, error) {
	full := append([]string{"--addr", g.Addr, "--domain", g.Domain}, args...)
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

type impactResult struct {
	DirectInvariants   []graphNode `json:"direct_invariants"`
	DirectFailureModes []graphNode `json:"direct_failure_modes"`
	DirectArchitecture []graphNode `json:"direct_architecture"`
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

// BuildAnchorIndex asks production which paths it holds governing law about.
func (g Graph) BuildAnchorIndex(ctx context.Context, paths []string, graphDigest string) (prospective.AnchorIndex, error) {
	var anchored []string
	for _, p := range paths {
		raw, err := g.run(ctx, "impact", "--file", p, "--json")
		if err != nil {
			// A path production cannot answer for is not anchored. It is not an
			// error either: "the graph holds no law about this file" is the
			// stratum-B fact this index exists to record.
			continue
		}
		var res impactResult
		if err := json.Unmarshal(raw, &res); err != nil {
			continue
		}
		if len(res.DirectInvariants)+len(res.DirectFailureModes)+len(res.DirectArchitecture) > 0 {
			anchored = append(anchored, p)
		}
	}
	return prospective.NormalizeAnchorIndex(prospective.AnchorIndex{
		RepositoryDomain:  g.Domain,
		GraphDigestSHA256: graphDigest,
		ProducedBy:        fmt.Sprintf("%s impact --addr %s --domain %s --file <path> --json  [%s]", g.Bin, g.Addr, g.Domain, AnchorRuleDescription),
		AnchoredPaths:     anchored,
	})
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
				if node, err := g.resolve(ctx, rc, id); err == nil {
					item.Title = node.Label
					item.Statement = statementOf(node)
					item.Anchors = anchorsOf(node)
				}
			}
			items = append(items, item)
		}
	}
	return prospective.NormalizeCorpus(prospective.Corpus{
		RepositoryDomain:  g.Domain,
		GraphDigestSHA256: graphDigest,
		ProducedBy:        fmt.Sprintf("%s query --addr %s --domain %s --mode by_class --class {%s} --json (+ resolve per node)", g.Bin, g.Addr, g.Domain, strings.Join(governingClasses, ",")),
		Items:             items,
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
