// SPDX-License-Identifier: Apache-2.0

// Command coverage-report prints a risk-weighted awareness coverage report.
//
// It is fully offline and deterministic given a fixed source tree + seed:
//   - the file universe (denominator) comes from walking the services repo,
//     using the same skip rules as the annotation scanner;
//   - which files carry a direct anchor (numerator) comes from parsing the
//     embedded seed N-Triples for `<sourceFile> aw:implements <node>` edges;
//   - the authority-domain coversPath prefixes come from the same seed.
//
// The report stratifies coverage by named surface and weights it by file risk
// (coverage package), so the number that matters — critical / authority /
// repository / rbac / remediation surface coverage — is not diluted by helpers.
//
// Usage:
//
//	coverage-report [-repo ../services] [-seed golang/server/embeddata/awareness.nt]
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/awareness-graph/golang/coverage"
)

func main() {
	repo := flag.String("repo", "../services", "path to the services repo whose Go tree is the file universe")
	seed := flag.String("seed", "golang/server/embeddata/awareness.nt", "path to the awareness seed N-Triples")
	flag.Parse()

	anchored, covers, err := parseSeed(*seed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "coverage-report: read seed: %v\n", err)
		os.Exit(1)
	}

	files, err := walkGoFiles(*repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "coverage-report: walk repo: %v\n", err)
		os.Exit(1)
	}

	inv := make([]coverage.FileCoverage, 0, len(files))
	for _, f := range files {
		inv = append(inv, coverage.FileCoverage{Path: f, HasDirectAnchor: anchored[f]})
	}

	rep := coverage.BuildReport(inv, covers)
	printReport(rep, *repo, *seed)
}

// walkGoFiles returns repo-relative Go file paths under the services tree,
// using the annotation scanner's skip rules (no vendor/generated/test-tree
// noise). Paths are returned as `golang/...` to match the coverage prefixes.
func walkGoFiles(repoRoot string) ([]string, error) {
	root := filepath.Join(repoRoot, "golang")
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "dist", "bin":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// parseSeed extracts, from the seed N-Triples:
//   - anchored: repo-relative path -> true for files that are the subject of an
//     aw:implements edge (i.e. at least one invariant/failure_mode/intent
//     anchors the file);
//   - covers: the union of aw:coversPath literals across AuthorityDomain nodes.
func parseSeed(seedPath string) (anchored map[string]bool, covers []string, err error) {
	data, err := os.ReadFile(seedPath)
	if err != nil {
		return nil, nil, err
	}
	anchored = map[string]bool{}
	coverSet := map[string]bool{}

	const implementsPred = "<https://globular.io/awareness#implements>"
	const coversPred = "<https://globular.io/awareness#coversPath>"
	const sourceFilePrefix = "https://globular.io/awareness#sourceFile/"

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "<") {
			continue
		}
		sEnd := strings.IndexByte(line, '>')
		if sEnd < 0 {
			continue
		}
		subj := line[1:sEnd] // without angle brackets
		rest := strings.TrimSpace(line[sEnd+1:])

		switch {
		case strings.HasPrefix(rest, implementsPred) && strings.HasPrefix(subj, sourceFilePrefix):
			if p := decodeSourceFilePath(subj, sourceFilePrefix); p != "" {
				anchored[p] = true
			}
		case strings.HasPrefix(rest, coversPred):
			if v := firstLiteral(rest); v != "" {
				coverSet[v] = true
			}
		}
	}

	for c := range coverSet {
		covers = append(covers, c)
	}
	sort.Strings(covers)
	return anchored, covers, nil
}

// decodeSourceFilePath turns a minted SourceFile IRI body into its repo path.
// The id segment is percent-encoded per N-Triples IRIREF rules.
func decodeSourceFilePath(subj, prefix string) string {
	enc := strings.TrimPrefix(subj, prefix)
	dec, err := url.PathUnescape(enc)
	if err != nil {
		return enc
	}
	return dec
}

// firstLiteral returns the unescaped body of the first quoted literal on the
// remainder of an N-Triples line.
func firstLiteral(rest string) string {
	i := strings.IndexByte(rest, '"')
	if i < 0 {
		return ""
	}
	j := strings.LastIndexByte(rest, '"')
	if j <= i {
		return ""
	}
	r := strings.NewReplacer(`\\`, `\`, `\"`, `"`, `\n`, "\n", `\t`, "\t", `\r`, "\r")
	return r.Replace(rest[i+1 : j])
}

func printReport(rep *coverage.Report, repo, seed string) {
	fmt.Printf("# Awareness risk-weighted coverage report\n\n")
	fmt.Printf("repo: %s   seed: %s\n", repo, seed)
	fmt.Printf("files scanned: %d   risk-weighted overall: %d%%\n\n", rep.TotalFiles, rep.WeightedOverallPercent)

	fmt.Printf("## Surface coverage (do not chase 100%% raw coverage)\n\n")
	fmt.Printf("%-16s %8s %8s %9s %12s\n", "surface", "covered", "total", "percent", "weight%")
	for _, s := range rep.Surfaces {
		fmt.Printf("%-16s %8d %8d %8d%% %11d%%\n",
			s.Surface, s.CoveredFiles, s.TotalFiles, s.Percent, pct(s.CoveredWeight, s.TotalWeight))
	}

	fmt.Printf("\n## Unknown high-risk files (%d total — high/critical, no direct anchor)\n\n", rep.UnknownHighRiskCount)
	if len(rep.UnknownHighRiskFiles) == 0 {
		fmt.Printf("(none)\n")
		return
	}
	for _, f := range rep.UnknownHighRiskFiles {
		fmt.Printf("  %s\n", f)
	}
	if rep.UnknownHighRiskCount > len(rep.UnknownHighRiskFiles) {
		fmt.Printf("  ... and %d more (list capped at %d)\n",
			rep.UnknownHighRiskCount-len(rep.UnknownHighRiskFiles), coverage.MaxUnknownHighRiskFiles)
	}
}

func pct(num, den int) int {
	if den <= 0 {
		return 0
	}
	return num * 100 / den
}
