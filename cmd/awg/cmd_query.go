// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
)

var queryRPC = func(ctx context.Context, addr string, req *awarenesspb.QueryRequest) (*awarenesspb.QueryResponse, error) {
	c, err := connectAWG(addr)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.Query(ctx, req)
}

func runQuery(args []string) int {
	fs := flag.NewFlagSet("awg query", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", "localhost:10120", "AWG gRPC server address")
	mode := fs.String("mode", "", "by_file | by_id | by_class | related (required)")
	file := fs.String("file", "", "repo-relative path (for mode=by_file)")
	id := fs.String("id", "", "class-qualified id (for mode=by_id/related)")
	class := fs.String("class", "", "class name (for mode=by_class)")
	limit := fs.Int("limit", 50, "maximum rows")
	asJSON := fs.Bool("json", false, "output as JSON")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg query --mode <mode> [flags]

Structured browse of the awareness graph.

Modes:
  by_file   list nodes whose anchor names --file
  by_id     return the node matching --id
  by_class  list all nodes of --class (use --limit)
  related   list nodes pointed at by --id

Classes: invariant, failure_mode, incident_pattern, intent, symbol, source_file,
         code_symbol, forbidden_fix, test, meta_principle, component, boundary,
         contract, decision, evidence, design_pattern, implementation_pattern,
         pattern_misuse

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *mode == "" {
		fmt.Fprintln(os.Stderr, "awg query: --mode is required")
		return 2
	}

	qm, ok := parseQueryMode(*mode)
	if !ok {
		fmt.Fprintln(os.Stderr, "awg query: --mode must be one of: by_file, by_id, by_class, related")
		return 2
	}
	req := &awarenesspb.QueryRequest{
		Mode:  qm,
		File:  *file,
		Id:    *id,
		Limit: int32(*limit),
	}
	if *class != "" {
		qc, ok := parseQueryClass(*class)
		if !ok {
			fmt.Fprintln(os.Stderr, "awg query: --class must be one of: invariant, failure_mode, incident_pattern, intent, symbol, source_file")
			return 2
		}
		req.Class = qc
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := queryRPC(ctx, *addr, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg query: %s\n", formatReadSurfaceError("query", err))
		return 1
	}

	if *asJSON {
		return emitProtoJSON(resp)
	}

	rows := resp.GetRows()
	printGraphAuthority(resp.GetAuthority())
	if len(rows) == 0 {
		fmt.Println("(no rows)")
		return 0
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CLASS\tID\tLABEL\tSEVERITY\tSTATUS\tRELATION\tSOURCE")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.GetClass(), r.GetId(),
			truncate(r.GetLabel(), 60),
			r.GetSeverity(), r.GetStatus(),
			r.GetRelation(), r.GetSourceFile(),
		)
	}
	_ = w.Flush()
	fmt.Printf("\n%d row(s)\n", len(rows))
	return 0
}

func parseQueryMode(s string) (awarenesspb.QueryMode, bool) {
	switch s {
	case "by_file":
		return awarenesspb.QueryMode_QUERY_MODE_BY_FILE, true
	case "by_id":
		return awarenesspb.QueryMode_QUERY_MODE_BY_ID, true
	case "by_class":
		return awarenesspb.QueryMode_QUERY_MODE_BY_CLASS, true
	case "related":
		return awarenesspb.QueryMode_QUERY_MODE_RELATED, true
	}
	return 0, false
}

func parseQueryClass(s string) (awarenesspb.QueryClass, bool) {
	switch s {
	case "invariant":
		return awarenesspb.QueryClass_QUERY_CLASS_INVARIANT, true
	case "failure_mode":
		return awarenesspb.QueryClass_QUERY_CLASS_FAILURE_MODE, true
	case "incident_pattern":
		return awarenesspb.QueryClass_QUERY_CLASS_INCIDENT_PATTERN, true
	case "intent":
		return awarenesspb.QueryClass_QUERY_CLASS_INTENT, true
	case "symbol":
		return awarenesspb.QueryClass_QUERY_CLASS_SYMBOL, true
	case "source_file":
		return awarenesspb.QueryClass_QUERY_CLASS_SOURCE_FILE, true
	case "code_symbol":
		return awarenesspb.QueryClass_QUERY_CLASS_CODE_SYMBOL, true
	case "forbidden_fix":
		return awarenesspb.QueryClass_QUERY_CLASS_FORBIDDEN_FIX, true
	case "test":
		return awarenesspb.QueryClass_QUERY_CLASS_TEST, true
	// Architectural spine (Stage A).
	case "meta_principle":
		return awarenesspb.QueryClass_QUERY_CLASS_META_PRINCIPLE, true
	case "component":
		return awarenesspb.QueryClass_QUERY_CLASS_COMPONENT, true
	case "boundary":
		return awarenesspb.QueryClass_QUERY_CLASS_BOUNDARY, true
	case "contract":
		return awarenesspb.QueryClass_QUERY_CLASS_CONTRACT, true
	case "decision":
		return awarenesspb.QueryClass_QUERY_CLASS_DECISION, true
	case "evidence":
		return awarenesspb.QueryClass_QUERY_CLASS_EVIDENCE, true
	case "design_pattern":
		return awarenesspb.QueryClass_QUERY_CLASS_DESIGN_PATTERN, true
	case "implementation_pattern":
		return awarenesspb.QueryClass_QUERY_CLASS_IMPLEMENTATION_PATTERN, true
	case "pattern_misuse":
		return awarenesspb.QueryClass_QUERY_CLASS_PATTERN_MISUSE, true
	}
	return 0, false
}
