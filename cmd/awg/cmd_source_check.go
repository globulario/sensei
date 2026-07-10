// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type sourcePattern struct {
	ID      string   `yaml:"id"`
	Pattern string   `yaml:"pattern"`
	Scope   string   `yaml:"scope"`
	Except  []string `yaml:"except"`
	Message string   `yaml:"message"`
}

type sourcePatternsYAML struct {
	SourcePatterns []sourcePattern `yaml:"source_patterns"`
}

type violation struct {
	File      string
	Line      int
	PatternID string
	Message   string
}

// compiledPattern is a source pattern with its regex + except regexes compiled.
type compiledPattern struct {
	sourcePattern
	re      *regexp.Regexp
	excepts []*regexp.Regexp
}

// Method/function header heuristics for scope:method. A header is a line that
// declares a class method (`name(args) {`) or a function (`function name(`).
var (
	// Note the optional `(?::[^={]+)?` — a TypeScript return-type annotation
	// (`connectedCallback(): void {`) sits between the params and the body.
	sourceMethodDeclRe = regexp.MustCompile(`^\s*(?:export\s+)?(?:public\s+|private\s+|protected\s+)?(?:async\s+)?(?:static\s+)?(?:\*\s*)?(?:(?:get|set)\s+)?([A-Za-z_$][\w$]*)\s*\([^)]*\)\s*(?::[^={]+)?\{`)
	sourceFuncDeclRe   = regexp.MustCompile(`\b(?:function|func)\s+([A-Za-z_$][\w$]*)\s*\(`)
	sourceCtrlKeywords = map[string]bool{
		"if": true, "for": true, "while": true, "switch": true, "catch": true,
		"return": true, "do": true, "else": true, "function": true, "await": true,
	}
)

// enclosingMethod returns the name of the method/function that encloses line idx
// (scanning upward), or "" if none is found. A heuristic, not a parser — good
// enough to tell which method an assignment lives in.
func enclosingMethod(lines []string, idx int) string {
	for i := idx; i >= 0 && i < len(lines); i-- {
		line := lines[i]
		if m := sourceFuncDeclRe.FindStringSubmatch(line); m != nil {
			return m[1]
		}
		if m := sourceMethodDeclRe.FindStringSubmatch(line); m != nil {
			if name := m[1]; !sourceCtrlKeywords[name] {
				return name
			}
		}
	}
	return ""
}

// matchViolations evaluates one compiled pattern against a file's lines.
//
//   - file   — every matching line is a violation.
//   - class  — a violation only if the file contains the pattern but NOT all of
//     the (file-level) except tokens; reported once.
//   - method — per match: a violation UNLESS the match's ENCLOSING method name
//     matches an except. Structural: `this.innerHTML` in connectedCallback (an
//     except) is fine; the same in render()/refresh() is flagged.
func matchViolations(cp compiledPattern, path string, lines []string, full string) []violation {
	var out []violation
	switch cp.Scope {
	case "class":
		if !cp.re.MatchString(full) {
			return nil
		}
		hasAllExcepts := true
		for _, exc := range cp.excepts {
			if !exc.MatchString(full) {
				hasAllExcepts = false
				break
			}
		}
		if hasAllExcepts {
			return nil
		}
		for i, line := range lines {
			if cp.re.MatchString(line) {
				out = append(out, violation{File: path, Line: i + 1, PatternID: cp.ID, Message: cp.Message})
				break
			}
		}
	case "method":
		for i, line := range lines {
			if !cp.re.MatchString(line) {
				continue
			}
			meth := enclosingMethod(lines, i)
			excepted := false
			for _, exc := range cp.excepts {
				if exc.MatchString(meth) {
					excepted = true
					break
				}
			}
			if !excepted {
				out = append(out, violation{File: path, Line: i + 1, PatternID: cp.ID, Message: cp.Message})
			}
		}
	default: // "file"
		for i, line := range lines {
			if cp.re.MatchString(line) {
				out = append(out, violation{File: path, Line: i + 1, PatternID: cp.ID, Message: cp.Message})
			}
		}
	}
	return out
}

func runSourceCheck(args []string) int {
	fs := flag.NewFlagSet("awg source-check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	patterns := fs.String("patterns", "", "path to source_patterns.yaml")
	source := fs.String("source", "", "source directory to scan")
	strict := fs.Bool("strict", false, "exit 1 on any violations")
	exts := fs.String("extensions", ".ts,.js,.go", "comma-separated file extensions to scan")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg source-check --patterns <path.yaml> --source <dir> [--strict] [--extensions .ts,.js]

Scan source files for structural pattern violations.

Checks regex patterns from source_patterns.yaml against source files. Scope:
  file   — every matching line is a violation.
  class  — a violation only if the file has the pattern but NOT all "except"
           tokens (e.g. setInterval without disconnectedCallback).
  method — per match: a violation unless its ENCLOSING method name matches an
           "except" (e.g. this.innerHTML in connectedCallback is fine, in
           render()/refresh() is flagged) — structural, not file-level.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *patterns == "" || *source == "" {
		fs.Usage()
		return 2
	}

	// Parse patterns YAML
	data, err := os.ReadFile(*patterns)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read patterns: %v\n", err)
		return 1
	}
	var pf sourcePatternsYAML
	if err := yaml.Unmarshal(data, &pf); err != nil {
		fmt.Fprintf(os.Stderr, "parse patterns: %v\n", err)
		return 1
	}

	allowedExts := map[string]bool{}
	for _, ext := range strings.Split(*exts, ",") {
		allowedExts[strings.TrimSpace(ext)] = true
	}

	// Compile patterns
	var compiled []compiledPattern
	for _, sp := range pf.SourcePatterns {
		re, err := regexp.Compile(sp.Pattern)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid regex for %s: %v\n", sp.ID, err)
			continue
		}
		var excepts []*regexp.Regexp
		for _, exc := range sp.Except {
			if excRe, err := regexp.Compile(exc); err == nil {
				excepts = append(excepts, excRe)
			}
		}
		compiled = append(compiled, compiledPattern{sp, re, excepts})
	}

	// Walk source directory
	var violations []violation
	filepath.WalkDir(*source, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && (d.Name() == "node_modules" || d.Name() == "dist" || d.Name() == ".git") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if !allowedExts[ext] {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(content), "\n")
		fullContent := string(content)

		for _, cp := range compiled {
			violations = append(violations, matchViolations(cp, path, lines, fullContent)...)
		}
		return nil
	})

	// Report
	if len(violations) == 0 {
		fmt.Printf("source-check: %d patterns, 0 violations\n", len(compiled))
		return 0
	}

	fmt.Printf("source-check: %d violation(s) found\n\n", len(violations))
	for _, v := range violations {
		fmt.Printf("  %s:%d  [%s] %s\n", v.File, v.Line, v.PatternID, v.Message)
	}
	fmt.Println()

	if *strict {
		return 1
	}
	return 0
}
