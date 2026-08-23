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

// resolveClass is the class name `sensei resolve` accepts.
//
// It is the snake_case name by_class already returned, passed through
// unchanged. An earlier version translated to the CamelCase spelling the
// resolve help text advertises, which production rejects as an unsupported
// class. The bug was invisible for single-word classes -- Invariant and
// Contract are spelled the same either way, and both resolved at 100% -- while
// every multi-word class resolved at 0%, so it read as a graph with no detail
// rather than as a caller sending the wrong string.
func resolveClass(class string) string { return class }

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
	Total      int  `json:"total"`
	TotalKnown bool `json:"total_known"`
	Truncated  bool `json:"truncated"`
}

// classRows is a complete class listing, or an honest account of why it is not.
type classRows struct {
	IDs        []string
	Classes    map[string]string
	Total      int
	TotalKnown bool
	// Complete is true only when the server said there was nothing left. It is
	// never inferred from the row count, because a listing that happens to end
	// on a page boundary looks identical to one that was cut there.
	Complete bool
}

// enumerateClass walks a class to the end, one page at a time.
//
// Before by_class could page, the harness took the first page and recorded the
// cap as the population -- which made "the first N rows the API returned" the
// selection rule for a measurement denominator. Paging is why the eligible
// corpus can now be the population rather than a page of it.
func (g Graph) enumerateClass(ctx context.Context, class string, pageSize int) (classRows, error) {
	out := classRows{Classes: map[string]string{}}
	seen := map[string]bool{}
	offset := 0
	for page := 0; ; page++ {
		if page > 1000 {
			return out, fmt.Errorf("class %s: paging did not terminate after %d pages", class, page)
		}
		raw, err := g.run(ctx, "query", "--mode", "by_class", "--class", class,
			"--limit", fmt.Sprint(pageSize), "--offset", fmt.Sprint(offset), "--json")
		if err != nil {
			if offset == 0 {
				return out, fmt.Errorf("class %s: %w", class, err)
			}
			// The server stopped answering mid-walk. Whatever was collected is
			// kept, but the listing is not claimed complete.
			return out, nil
		}
		var res queryResult
		if err := json.Unmarshal(raw, &res); err != nil {
			return out, fmt.Errorf("class %s: %w", class, err)
		}
		if res.TotalKnown {
			out.Total, out.TotalKnown = res.Total, true
		}
		added := 0
		for _, row := range res.Rows {
			if seen[row.ID] {
				continue
			}
			seen[row.ID] = true
			out.IDs = append(out.IDs, row.ID)
			out.Classes[row.ID] = row.Class
			added++
		}
		offset += len(res.Rows)
		if !res.Truncated {
			out.Complete = true
			return out, nil
		}
		if len(res.Rows) == 0 || added == 0 {
			// Truncated but nothing new: a server that cannot page would loop
			// here forever, serving page 0 each time.
			return out, nil
		}
	}
}

type resolveResult struct {
	Node  graphNode `json:"node"`
	Found bool      `json:"found"`
}

// BuildCorpus freezes the eligible knowledge corpus.
//
// Every item that reaches the corpus is human-adjudicable. Anything that
// cannot be materialized is excluded with a stable reason and counted, and the
// per-class accounting reconciles what the graph reports holding against what
// a capped enumeration could see -- because production caps by_class at a
// fixed number of rows, and a capped enumeration left unreconciled reports its
// own cap as if it were the population.
//
// Anchors are carried on each item because the corpus must be reproducible,
// and they are the one field that never reaches an adjudicator -- see
// prospective.BlindCorpusItem.
func (g Graph) BuildCorpus(ctx context.Context, graphDigest string, rowCap int) (prospective.Corpus, error) {
	totals, err := g.ClassTotals(ctx)
	if err != nil {
		return prospective.Corpus{}, err
	}
	var items []prospective.CorpusItem
	var excluded []prospective.CorpusExclusion
	var accounting []prospective.ClassAccounting

	for _, class := range governingClasses {
		listing, err := g.enumerateClass(ctx, class, rowCap)
		if err != nil {
			return prospective.Corpus{}, err
		}
		graphTotal := totals[class]
		if listing.TotalKnown {
			// The listing's own total is preferred: it is scoped exactly like
			// the rows it accompanies, whereas the metadata count is a
			// graph-wide figure that can be scoped differently.
			graphTotal = listing.Total
		}
		acc := prospective.ClassAccounting{Class: class, GraphTotal: graphTotal, Enumerated: len(listing.IDs)}
		if acc.GraphTotal > acc.Enumerated {
			acc.NotEnumerable = acc.GraphTotal - acc.Enumerated
		}
		if !listing.Complete && acc.NotEnumerable == 0 && acc.GraphTotal > 0 {
			// The walk did not finish but the numbers happen to line up. Say so
			// rather than let a coincidence read as completeness.
			return prospective.Corpus{}, fmt.Errorf("class %s: enumeration did not report completion; refusing to record %d rows as the whole population",
				class, len(listing.IDs))
		}
		for _, qualified := range listing.IDs {
			id := strings.TrimPrefix(qualified, class+":")
			node, err := g.resolve(ctx, resolveClass(class), id)
			if err != nil {
				excluded = append(excluded, prospective.CorpusExclusion{
					ID: qualified, Class: class, Reason: prospective.CorpusExcludedUnresolvable, Detail: err.Error(),
				})
				continue
			}
			item := prospective.CorpusItem{
				ID:        qualified,
				Class:     listing.Classes[qualified],
				Title:     node.Label,
				Statement: statementOf(node),
				Anchors:   anchorsOf(node),
			}
			item.Materialization = prospective.MaterializedFromNode
			if strings.TrimSpace(item.Title) == "" && strings.TrimSpace(item.Statement) == "" {
				item.Title = humanizeSlug(id)
				item.Statement, err = g.relatedStatement(ctx, qualified)
				item.Materialization = prospective.MaterializedFromRelated
				if err != nil || strings.TrimSpace(item.Statement) == "" {
					excluded = append(excluded, prospective.CorpusExclusion{
						ID: qualified, Class: class, Reason: prospective.CorpusExcludedNoStatement,
						Detail: "the node carries no label and no facts, and nothing governing relates to it, so nothing in the pinned world says what it means",
					})
					continue
				}
			}
			items = append(items, item)
			acc.Materialized++
		}
		acc.Excluded = acc.Enumerated - acc.Materialized
		accounting = append(accounting, acc)
	}

	return prospective.NormalizeCorpus(prospective.Corpus{
		RepositoryDomain:  g.Domain,
		GraphDigestSHA256: graphDigest,
		ProducedBy: fmt.Sprintf("%s query --addr %s --domain %s --mode by_class --class {%s} --limit %d --json, then %s resolve <class> <id> per row; totals from %s metadata --json",
			g.Bin, g.Addr, g.Domain, strings.Join(governingClasses, ","), rowCap, g.Bin, g.Bin),
		Items:       items,
		Excluded:    excluded,
		Accounting:  accounting,
		QueryRowCap: rowCap,
	})
}

// ClassTotals reads the graph's own per-class counts.
//
// It is an INDEPENDENT total, and that independence is the point: without it,
// a capped enumeration has no way to know it was capped, and reports the cap
// as the population.
func (g Graph) ClassTotals(ctx context.Context) (map[string]int, error) {
	raw, err := g.run(ctx, "metadata", "--json")
	if err != nil {
		return nil, fmt.Errorf("class totals: %w", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("class totals: %w", err)
	}
	out := map[string]int{}
	for _, class := range governingClasses {
		if v, ok := fields[class+"_count"]; ok {
			switch n := v.(type) {
			case float64:
				out[class] = int(n)
			case string:
				var parsed int
				if _, err := fmt.Sscanf(n, "%d", &parsed); err == nil {
					out[class] = parsed
				}
			}
		}
	}
	return out, nil
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

// relatedGoverningClasses are the classes a composed statement may quote.
//
// source_file and test entries are excluded deliberately: they are the graph's
// account of WHERE an item applies, and putting them in front of an
// adjudicator would hand over the answer key by another route. The blinding
// rule is about the information, not about the field it arrives in.
var relatedGoverningClasses = map[string]bool{
	"invariant": true, "failure_mode": true, "forbidden_fix": true,
	"incident_pattern": true, "contract": true, "meta_principle": true,
	"decision": true, "design_pattern": true, "implementation_pattern": true,
}

// relatedStatement composes what the pinned world says about a node that
// carries no text of its own.
func (g Graph) relatedStatement(ctx context.Context, qualifiedID string) (string, error) {
	raw, err := g.run(ctx, "query", "--mode", "related", "--id", qualifiedID, "--json")
	if err != nil {
		return "", err
	}
	var res struct {
		Rows []graphNode `json:"rows"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", err
	}
	var lines []string
	for _, r := range res.Rows {
		if !relatedGoverningClasses[r.Class] || strings.TrimSpace(r.Label) == "" {
			continue
		}
		lines = append(lines, "relates to "+r.Class+": "+r.Label)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n"), nil
}

// humanizeSlug turns an identifier into a readable phrase. The id is graph
// content; this only changes how it reads.
func humanizeSlug(id string) string {
	s := strings.ReplaceAll(id, "_", " ")
	s = strings.ReplaceAll(s, ".", " · ")
	return strings.TrimSpace(s)
}
