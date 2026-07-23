package briefing

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	githubapi "github.com/globulario/sensei-github-app/internal/github"
)

const CommentMarker = "<!-- sensei-architectural-briefing -->"

// Input binds a briefing to one exact repository transition.
type Input struct {
	Repository string
	PRNumber   int
	BaseSHA    string
	HeadSHA    string
	Files      []githubapi.PullRequestFile
}

// Report contains the two GitHub presentation surfaces for one analysis.
type Report struct {
	CommentBody  string
	CheckSummary string
	CheckText    string
	ExternalID   string
}

// Build produces a deterministic, static-only briefing. No repository code is executed.
func Build(input Input) Report {
	files := append([]githubapi.PullRequestFile(nil), input.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Filename < files[j].Filename })

	areas := make(map[string]struct{})
	types := make(map[string]int)
	surfaces := make(map[string]struct{})
	additions, deletions, tests := 0, 0, 0

	for _, file := range files {
		additions += file.Additions
		deletions += file.Deletions
		areas[topLevelArea(file.Filename)] = struct{}{}
		types[fileType(file.Filename)]++
		if isTest(file.Filename) {
			tests++
		}
		for _, surface := range detectSurfaces(file.Filename) {
			surfaces[surface] = struct{}{}
		}
	}

	var body strings.Builder
	body.WriteString(CommentMarker + "\n")
	body.WriteString("## 🐟 Sensei Architectural Briefing\n\n")
	body.WriteString("### Change identity\n\n")
	fmt.Fprintf(&body, "- Repository: `%s`\n", input.Repository)
	fmt.Fprintf(&body, "- Pull request: `#%d`\n", input.PRNumber)
	fmt.Fprintf(&body, "- Base revision: `%s`\n", input.BaseSHA)
	fmt.Fprintf(&body, "- Head revision: `%s`\n", input.HeadSHA)
	body.WriteString("- Analysis mode: `mechanical/static-only`\n\n")

	body.WriteString("### Mechanical scope\n\n")
	fmt.Fprintf(&body, "- Changed files: **%d**\n", len(files))
	fmt.Fprintf(&body, "- Line delta: **+%d / -%d**\n", additions, deletions)
	fmt.Fprintf(&body, "- Changed test files: **%d**\n", tests)
	fmt.Fprintf(&body, "- Affected areas: %s\n", codeList(sortedKeys(areas)))
	fmt.Fprintf(&body, "- File types: %s\n", typeSummary(types))
	if len(surfaces) > 0 {
		fmt.Fprintf(&body, "- Sensitive structural surfaces: %s\n", codeList(sortedKeys(surfaces)))
	}
	body.WriteString("\n")

	body.WriteString("### Changed files\n\n")
	body.WriteString("| File | Status | Delta |\n")
	body.WriteString("|---|---:|---:|\n")
	limit := len(files)
	if limit > 40 {
		limit = 40
	}
	for _, file := range files[:limit] {
		fmt.Fprintf(&body, "| `%s` | %s | +%d / -%d |\n", escapeMarkdown(file.Filename), file.Status, file.Additions, file.Deletions)
	}
	if len(files) > limit {
		fmt.Fprintf(&body, "\n_%d additional changed files omitted from this compact view._\n", len(files)-limit)
	}

	body.WriteString("\n### Current knowledge boundary\n\n")
	body.WriteString("This first GitHub App slice binds the exact change and reports mechanically derived scope without executing pull-request code. Governed invariants, failure modes, contracts, forbidden fixes, and required evidence will appear after the repository bootstrap is connected to the Sensei engine.\n\n")
	body.WriteString("_This comment is updated in place when the pull-request head changes._\n")

	summary := fmt.Sprintf("Mechanical briefing for %d changed files at `%s`.", len(files), shortSHA(input.HeadSHA))
	return Report{
		CommentBody:  body.String(),
		CheckSummary: summary,
		CheckText:    body.String(),
		ExternalID:   fmt.Sprintf("sensei-pr-%d-%s", input.PRNumber, input.HeadSHA),
	}
}

func topLevelArea(path string) string {
	path = filepath.ToSlash(strings.TrimPrefix(path, "./"))
	if before, _, ok := strings.Cut(path, "/"); ok {
		return before
	}
	return "repository-root"
}

func fileType(path string) string {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "dockerfile", "makefile", "justfile":
		return base
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return "extensionless"
	}
	return ext
}

func isTest(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	base := strings.ToLower(filepath.Base(path))
	return strings.Contains(lower, "/test/") ||
		strings.Contains(lower, "/tests/") ||
		strings.HasSuffix(base, "_test.go") ||
		strings.HasSuffix(base, ".test.ts") ||
		strings.HasSuffix(base, ".spec.ts") ||
		strings.HasSuffix(base, "_test.py") ||
		strings.HasPrefix(base, "test_")
}

func detectSurfaces(path string) []string {
	lower := strings.ToLower(filepath.ToSlash(path))
	base := strings.ToLower(filepath.Base(path))
	var result []string
	if strings.HasPrefix(lower, ".github/workflows/") {
		result = append(result, "CI workflow")
	}
	if strings.Contains(lower, "/migrations/") || strings.HasPrefix(lower, "migrations/") {
		result = append(result, "database migration")
	}
	if strings.HasSuffix(lower, ".proto") || strings.Contains(lower, "/api/") {
		result = append(result, "API contract")
	}
	switch base {
	case "go.mod", "go.sum", "package.json", "package-lock.json", "pnpm-lock.yaml", "cargo.toml", "cargo.lock":
		result = append(result, "dependency boundary")
	case "dockerfile", "docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml":
		result = append(result, "deployment configuration")
	}
	if strings.Contains(lower, "auth") || strings.Contains(lower, "permission") || strings.Contains(lower, "rbac") {
		result = append(result, "authorization surface")
	}
	return result
}

func sortedKeys[T any](values map[string]T) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func codeList(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, "`"+value+"`")
	}
	return strings.Join(quoted, ", ")
}

func typeSummary(values map[string]int) string {
	keys := sortedKeys(values)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("`%s` (%d)", key, values[key]))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func shortSHA(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func escapeMarkdown(value string) string {
	return strings.ReplaceAll(value, "|", "\\|")
}
