// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// INTENT SOURCE ADAPTERS — the read-only gatherers that feed the intent
// proposer (docs/intent-mining-design.md, phase 1). They scan a repo for
// RULE-BEARING text — the explicit "charter" — so the LLM proposer receives
// focused, high-intent-density excerpts rather than the whole tree. Each excerpt
// carries a stable citation; the proposer may only cite from this set (the
// intent cage), so it cannot fabricate a stated source.
//
// Gathering is mechanical (file reads, git log) — no LLM, no key. It proposes
// nothing and grounds nothing; it only collects candidate stated intent.

// IntentExcerpt is one rule-bearing snippet of stated intent with its citation.
type IntentExcerpt struct {
	Kind     string // docs | comments | schemas | tests | commits | prs
	Citation string // file:<relpath>:<line> | commit:<sha> | pr:<id>:<cid>
	Text     string // the rule-bearing line/snippet (bounded)
}

// ruleLang matches imperative/authority language that signals stated intent. It
// is deliberately broad on the gather side (recall over precision) — the
// proposer and the grounding gate cut the noise downstream.
var ruleLang = regexp.MustCompile(`(?i)\b(must not|must|never|always|shall|do not|don't|owns|owner of|sole source|source of truth|forbidden|required|invariant|responsible for|authority|not allowed|cannot|only .{0,40}\b(?:may|can)\b)\b`)

// sourceExts maps a source kind to the file extensions its adapter scans.
var commentExts = map[string]bool{".go": true, ".ts": true, ".tsx": true, ".js": true, ".py": true, ".rs": true, ".java": true}
var schemaExts = map[string]bool{".proto": true}

// excerptSkipDirs are never walked (noise / vendored / generated / Sensei's own
// scaffolding). The `.sensei`/`.claude`/`.agents`/`.cursor` trees hold Sensei's
// skill and rule surfaces — reading them would mine Sensei's own charter back as
// if the target repo authored it.
var excerptSkipDirs = map[string]bool{
	".git": true, "vendor": true, "node_modules": true, "dist": true,
	"build": true, "testdata": true, "third_party": true, "generated": true,
	".sensei": true, ".claude": true, ".agents": true, ".cursor": true,
}

// senseiCorpusDirs are Sensei's own awareness/intent corpus, scaffolded into a
// repo by `sensei bootstrap`. They are never the target repo's authored charter.
var senseiCorpusDirs = map[string]bool{
	"docs/awareness": true, "docs/intent": true,
}

// isSenseiCorpusDir reports whether rel is (or is under) a Sensei corpus dir.
func isSenseiCorpusDir(rel string) bool {
	rel = filepath.ToSlash(rel)
	for d := range senseiCorpusDirs {
		if rel == d || strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	return false
}

// senseiSectionHeading matches the `## Sensei` heading that `appendSnippet` adds
// to a repo's CLAUDE.md/AGENTS.md. Everything from it to the next same-or-higher
// markdown heading is Sensei's appended charter, not the repo's own.
var senseiSectionHeading = regexp.MustCompile(`(?i)^#{1,2}\s+Sensei\b`)

// markdownH1OrH2 matches a level-1 or level-2 markdown heading (`# ` or `## `),
// which closes a `## Sensei` section. Deeper headings (`### `) stay inside it.
var markdownH1OrH2 = regexp.MustCompile(`^#{1,2}\s+\S`)

const maxExcerptLen = 240

// GatherIntentExcerpts collects rule-bearing excerpts for the requested source
// kinds, bounded to `max` total (0 → a sane default). prComments may be nil.
func GatherIntentExcerpts(repoRoot string, kinds []string, prComments []ReviewComment, max int) []IntentExcerpt {
	if max <= 0 {
		max = 400
	}
	want := map[string]bool{}
	for _, k := range kinds {
		want[strings.TrimSpace(k)] = true
	}
	var out []IntentExcerpt
	add := func(es ...IntentExcerpt) {
		for _, e := range es {
			if len(out) >= max {
				return
			}
			out = append(out, e)
		}
	}

	if want["docs"] || want["comments"] || want["schemas"] || want["tests"] {
		_ = filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil || len(out) >= max {
				return nil
			}
			if d.IsDir() {
				if excerptSkipDirs[d.Name()] {
					return filepath.SkipDir
				}
				if r, _ := filepath.Rel(repoRoot, path); isSenseiCorpusDir(r) {
					return filepath.SkipDir
				}
				return nil
			}
			rel, _ := filepath.Rel(repoRoot, path)
			ext := strings.ToLower(filepath.Ext(path))
			switch {
			case want["tests"] && isTestPath(rel):
				add(scanFile(path, rel, "tests")...)
			case want["schemas"] && schemaExts[ext]:
				add(scanFile(path, rel, "schemas")...)
			case want["docs"] && ext == ".md":
				add(scanFile(path, rel, "docs")...)
			case want["comments"] && commentExts[ext] && !isTestPath(rel):
				add(scanFile(path, rel, "comments")...)
			}
			return nil
		})
	}
	if want["commits"] {
		add(gatherCommits(repoRoot, max)...)
	}
	if want["prs"] {
		add(gatherPRs(prComments)...)
	}
	return out
}

// scanFile returns the rule-bearing lines of one file as excerpts. For comments/
// schemas it only considers comment lines; for docs/tests it considers all lines.
func scanFile(path, rel, kind string) []IntentExcerpt {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var out []IntentExcerpt
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	line := 0
	inSenseiSection := false
	for sc.Scan() {
		line++
		t := strings.TrimSpace(sc.Text())
		if t == "" {
			continue
		}
		// In markdown, skip the `## Sensei` section that `sensei init` appends —
		// it is Sensei's charter, not the repo's. Everything from that heading to
		// the next H1/H2 heading (or EOF) is excluded.
		if kind == "docs" {
			if senseiSectionHeading.MatchString(t) {
				inSenseiSection = true
				continue
			}
			if inSenseiSection {
				if markdownH1OrH2.MatchString(t) {
					inSenseiSection = false
				} else {
					continue
				}
			}
		}
		if (kind == "comments" || kind == "schemas") && !isCommentLine(t) {
			continue
		}
		if !ruleLang.MatchString(t) {
			continue
		}
		out = append(out, IntentExcerpt{
			Kind:     kind,
			Citation: "file:" + filepath.ToSlash(rel) + ":" + strconv.Itoa(line),
			Text:     truncate(stripCommentMarkers(t), maxExcerptLen),
		})
		if len(out) >= 40 { // per-file cap
			break
		}
	}
	return out
}

func isCommentLine(t string) bool {
	return strings.HasPrefix(t, "//") || strings.HasPrefix(t, "#") ||
		strings.HasPrefix(t, "*") || strings.HasPrefix(t, "/*")
}

func stripCommentMarkers(t string) string {
	t = strings.TrimSpace(t)
	for _, p := range []string{"//", "/*", "*/", "*", "#"} {
		t = strings.TrimSpace(strings.TrimPrefix(t, p))
	}
	return t
}

// gatherCommits reads recent commit subjects/bodies with intent language.
func gatherCommits(repoRoot string, max int) []IntentExcerpt {
	n := max
	if n > 300 {
		n = 300
	}
	out, err := exec.Command("git", "-C", repoRoot, "log", "-n", strconv.Itoa(n), "--format=%H%x1f%s%x1f%b%x1e").Output()
	if err != nil {
		return nil
	}
	var ex []IntentExcerpt
	for _, rec := range strings.Split(string(out), "\x1e") {
		parts := strings.SplitN(strings.TrimSpace(rec), "\x1f", 3)
		if len(parts) < 2 {
			continue
		}
		sha, subj := parts[0], parts[1]
		body := ""
		if len(parts) == 3 {
			body = parts[2]
		}
		text := strings.TrimSpace(subj + " " + body)
		if !ruleLang.MatchString(text) {
			continue
		}
		ex = append(ex, IntentExcerpt{Kind: "commits", Citation: "commit:" + sha, Text: truncate(text, maxExcerptLen)})
	}
	return ex
}

// gatherPRs filters PR review comments for intent language.
func gatherPRs(comments []ReviewComment) []IntentExcerpt {
	var ex []IntentExcerpt
	for _, c := range comments {
		if !ruleLang.MatchString(c.Body) {
			continue
		}
		ex = append(ex, IntentExcerpt{
			Kind:     "prs",
			Citation: "pr:" + c.PRID + ":" + c.CommentID,
			Text:     truncate(strings.TrimSpace(c.Body), maxExcerptLen),
		})
	}
	return ex
}
