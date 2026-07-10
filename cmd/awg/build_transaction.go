// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/seedmeta"
)

const buildTransactionVersion = "v1"

func defaultTransactionPath(agRepo string) string {
	return filepath.Join(agRepo, "golang", "server", "embeddata", "awareness.transaction.tsv")
}

func buildTransactionTSV(agRepo, svcRepo string, ntBytes []byte) ([]byte, error) {
	marker, ok := seedmeta.ParseMarker(ntBytes)
	if !ok {
		return nil, fmt.Errorf("validated graph artifact carries no seed marker")
	}
	var buf bytes.Buffer
	writeTxn := func(format string, args ...any) {
		fmt.Fprintf(&buf, format, args...)
		buf.WriteByte('\n')
	}
	writeTxn("format\t%s", buildTransactionVersion)
	writeTxn("seed\tdigest_sha256\t%s", marker.Digest)
	writeTxn("seed\ttriple_count\t%d", marker.TripleCount)

	if err := appendRepoState(&buf, "awareness-graph", agRepo); err != nil {
		return nil, err
	}
	if err := appendRepoState(&buf, "services", svcRepo); err != nil {
		return nil, err
	}
	if err := appendToolState(&buf, "yaml2nt", filepath.Join(agRepo, "cmd", "yaml2nt", "main.go")); err != nil {
		return nil, err
	}
	if err := appendFileState(&buf, "build_script", filepath.Join(agRepo, "scripts", "build-awareness-graph.sh")); err != nil {
		return nil, err
	}
	if svcRepo != "" {
		if err := appendFileState(&buf, "namespace_registry", filepath.Join(svcRepo, "docs", "awareness", "namespaces.yaml")); err != nil {
			return nil, err
		}
		if err := appendFileState(&buf, "allowed_dangling_refs", filepath.Join(svcRepo, "docs", "awareness", "dangling_refs_baseline.tsv")); err != nil {
			return nil, err
		}
	}
	if err := appendTreeState(&buf, "ag_awareness", filepath.Join(agRepo, "docs", "awareness"), nil); err != nil {
		return nil, err
	}
	if err := appendTreeState(&buf, "ag_generated", filepath.Join(agRepo, "docs", "awareness", "generated"), nil); err != nil {
		return nil, err
	}
	if err := appendTreeState(&buf, "svc_awareness", filepath.Join(svcRepo, "docs", "awareness"), nil); err != nil {
		return nil, err
	}
	if err := appendTreeState(&buf, "svc_generated_filtered", filepath.Join(svcRepo, "docs", "awareness", "generated"), func(name string) bool {
		return !strings.HasPrefix(name, "awareness_graph_")
	}); err != nil {
		return nil, err
	}
	if err := appendTreeState(&buf, "svc_intent", filepath.Join(svcRepo, "docs", "intent"), nil); err != nil {
		return nil, err
	}
	if err := appendTreeState(&buf, "bench_contracts", filepath.Join(agRepo, "eval", "multi-swe-bench", "contracts"), nil); err != nil {
		return nil, err
	}
	if err := appendTreeState(&buf, "learning_events", filepath.Join(agRepo, "eval", "multi-swe-bench", "notes", "learning_events"), nil); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func appendRepoState(buf *bytes.Buffer, label, repo string) error {
	if strings.TrimSpace(repo) == "" {
		fmt.Fprintf(buf, "repo\t%s\tmissing\n", label)
		return nil
	}
	head, err := gitHead(repo)
	if err != nil {
		// Provenance degrades gracefully: a repo path we cannot read a git head
		// for (e.g. a standalone build with no sibling services repo, or a
		// non-git path) is recorded as missing rather than aborting the whole
		// rebuild + seed promotion. The combined build passes a real git repo.
		fmt.Fprintf(buf, "repo\t%s\tmissing\n", label)
		return nil
	}
	fmt.Fprintf(buf, "repo\t%s\t%s\n", label, head)
	return nil
}

func appendToolState(buf *bytes.Buffer, label, path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(buf, "tool\t%s\tmissing\n", label)
			return nil
		}
		return err
	}
	sum, err := fileSHA256(path)
	if err != nil {
		return err
	}
	fmt.Fprintf(buf, "tool\t%s\t%s\n", label, sum)
	return nil
}

func appendFileState(buf *bytes.Buffer, label, path string) error {
	if strings.TrimSpace(path) == "" {
		fmt.Fprintf(buf, "file\t%s\tmissing\n", label)
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(buf, "file\t%s\tmissing\n", label)
			return nil
		}
		return err
	}
	sum, err := fileSHA256(path)
	if err != nil {
		return err
	}
	fmt.Fprintf(buf, "file\t%s\t%s\n", label, sum)
	return nil
}

func appendTreeState(buf *bytes.Buffer, label, root string, include func(name string) bool) error {
	if strings.TrimSpace(root) == "" {
		fmt.Fprintf(buf, "tree\t%s\tmissing\n", label)
		return nil
	}
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(buf, "tree\t%s\tmissing\n", label)
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", root)
	}

	var rows []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			return nil
		}
		if include != nil && !include(name) {
			return nil
		}
		sum, err := fileSHA256(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rows = append(rows, fmt.Sprintf("tree\t%s\t%s\t%s", label, filepath.ToSlash(rel), sum))
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(rows)
	if len(rows) == 0 {
		fmt.Fprintf(buf, "tree\t%s\tempty\n", label)
		return nil
	}
	for _, row := range rows {
		buf.WriteString(row)
		buf.WriteByte('\n')
	}
	return nil
}

func gitHead(repo string) (string, error) {
	cmd := exec.Command("git", "-C", repo, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func fileSHA256(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func evaluateBuildTransactionFreshness(committed, current []byte) auditResult {
	if bytes.Equal(committed, current) {
		return auditResult{name: "cross-repo-transaction", level: auditPASS, summary: "current"}
	}

	committedLines := linesSet(committed)
	currentLines := linesSet(current)
	var details []string
	for _, line := range orderedDiffLines(committedLines, currentLines) {
		details = append(details, "missing current stamp: "+line)
	}
	for _, line := range orderedDiffLines(currentLines, committedLines) {
		details = append(details, "new current stamp: "+line)
	}
	res := auditResult{
		name:    "cross-repo-transaction",
		level:   auditWARN,
		summary: "STALE — transaction stamp is advisory-stale for the current cross-repo inputs",
	}
	if len(details) > 10 {
		res.details = details[:10]
	} else {
		res.details = details
	}
	return res
}

func linesSet(b []byte) map[string]bool {
	out := map[string]bool{}
	for _, raw := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		line := strings.TrimSpace(raw)
		if line != "" {
			out[line] = true
		}
	}
	return out
}

func orderedDiffLines(left, right map[string]bool) []string {
	var out []string
	for line := range left {
		if !right[line] {
			out = append(out, line)
		}
	}
	sort.Strings(out)
	return out
}
