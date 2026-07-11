// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

type authoritySurfaceCandidateDoc struct {
	AuthoritySurfaceCandidates struct {
		RepoRoot    string                      `yaml:"repo_root"`
		GeneratedBy string                      `yaml:"generated_by"`
		Candidates  []authoritySurfaceCandidate `yaml:"candidates"`
	} `yaml:"authority_surface_candidates"`
}

type authoritySurfaceCandidate struct {
	ID                string   `yaml:"id"`
	Class             string   `yaml:"class"`
	Status            string   `yaml:"status"`
	Confidence        string   `yaml:"confidence"`
	Kind              string   `yaml:"kind"`
	Owner             string   `yaml:"owner,omitempty"`
	SourceFiles       []string `yaml:"source_files"`
	Symbols           []string `yaml:"symbols"`
	Routes            []string `yaml:"routes,omitempty"`
	MutatesState      []string `yaml:"mutates_state,omitempty"`
	ControlsLifecycle []string `yaml:"controls_lifecycle,omitempty"`
	RequiredAuthority []string `yaml:"required_authority,omitempty"`
	RequiredGuards    []string `yaml:"required_guards,omitempty"`
	Notes             []string `yaml:"notes,omitempty"`
	Evidence          []string `yaml:"evidence,omitempty"`
}

type authorityFeatures struct {
	relPath    string
	symbol     string
	owner      string
	hasHandler bool
	routes     []string
	mutates    []string
	lifecycle  []string
	guards     []string
	authority  []string
	notes      []string
	evidence   []string
}

func runExtractAuthority(args []string) int {
	fs := flag.NewFlagSet("sensei extract-authority", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoRoot := fs.String("repo-root", ".", "repository root to scan")
	output := fs.String("output", "", "candidate YAML to write (default: <repo>/docs/awareness/candidates/authority_surface_candidates.yaml)")
	check := fs.Bool("check", false, "compare committed candidate YAML to a fresh run; exit 1 if stale")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei extract-authority [flags]

Extract conservative AuthoritySurface candidates from Go source. The command
emits status:candidate YAML only; nothing is auto-promoted or imported as live
graph authority.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	root, err := filepath.Abs(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei extract-authority: resolve repo root: %v\n", err)
		return 1
	}
	cands, err := extractAuthorityCandidates(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei extract-authority: %v\n", err)
		return 1
	}
	out, err := renderAuthorityCandidates(root, cands)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei extract-authority: render: %v\n", err)
		return 1
	}

	target := *output
	if target == "" {
		target = filepath.Join(root, "docs", "awareness", "candidates", "authority_surface_candidates.yaml")
	}
	if *check {
		committed, _ := os.ReadFile(target)
		if !bytes.Equal(bytes.TrimSpace(committed), bytes.TrimSpace(out)) {
			fmt.Fprintf(os.Stderr, "extract-authority: STALE — %s differs from a fresh run; rerun to regenerate\n", target)
			return 1
		}
		fmt.Fprintf(os.Stderr, "extract-authority: fresh (%d candidates)\n", len(cands))
		return 0
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "sensei extract-authority: mkdir: %v\n", err)
		return 1
	}
	if err := os.WriteFile(target, out, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "sensei extract-authority: write: %v\n", err)
		return 1
	}

	kinds := map[string]int{}
	for _, c := range cands {
		kinds[c.Kind]++
	}
	fmt.Fprintf(os.Stderr, "extract-authority: wrote %d candidate(s) to %s\n", len(cands), target)
	for _, line := range authorityKindSummary(kinds) {
		fmt.Fprintf(os.Stderr, "  %s\n", line)
	}
	return 0
}

func extractAuthorityCandidates(root string) ([]authoritySurfaceCandidate, error) {
	var files []string
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && authorityExcludedDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(d.Name()) != ".go" || isTestFile(d.Name()) || strings.HasSuffix(d.Name(), ".pb.go") {
			return nil
		}
		files = append(files, path)
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(files)

	var out []authoritySurfaceCandidate
	for _, path := range files {
		cands, err := scanAuthorityFile(root, path)
		if err != nil {
			return nil, err
		}
		out = append(out, cands...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func scanAuthorityFile(root, path string) ([]authoritySurfaceCandidate, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	rel = filepath.ToSlash(rel)

	routes := map[string][]string{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || sel.Sel.Name != "HandleFunc" || len(call.Args) < 2 {
				return true
			}
			route := authorityStringLiteral(call.Args[0])
			handler := authorityHandlerName(call.Args[1])
			if route == "" || handler == "" {
				return true
			}
			routes[handler] = append(routes[handler], route)
			return true
		})
	}

	var out []authoritySurfaceCandidate
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Name == nil {
			continue
		}
		features := authorityFeatures{
			relPath: rel,
			symbol:  fn.Name.Name,
			owner:   authorityOwner(file.Name.Name, fn),
			routes:  authorityDedupe(routes[fn.Name.Name]),
		}
		features.hasHandler = authorityLooksLikeHandler(fn, features.routes)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			authorityApplyCall(&features, call)
			return true
		})
		cand, ok := authorityCandidateFromFeatures(features)
		if ok {
			out = append(out, cand)
		}
	}
	return out, nil
}

func authorityApplyCall(features *authorityFeatures, call *ast.CallExpr) {
	name := authorityCallName(call.Fun)
	lower := strings.ToLower(name)
	if lower == "" {
		return
	}

	switch {
	case authorityIsGuardCall(lower):
		features.guards = append(features.guards, authorityGuardName(name))
		features.evidence = append(features.evidence, "guard:"+name)
	case authorityIsLifecycleCall(lower):
		features.lifecycle = append(features.lifecycle, authorityLifecycleName(name))
		features.authority = append(features.authority, "service_lifecycle_authority")
		features.evidence = append(features.evidence, "lifecycle:"+name)
	case authorityIsCertificateCall(lower):
		features.mutates = append(features.mutates, "certificate_state")
		features.authority = append(features.authority, "certificate_authority")
		features.evidence = append(features.evidence, "certificate:"+name)
	case authorityIsIdentityCall(lower):
		features.mutates = append(features.mutates, "identity_state")
		features.authority = append(features.authority, "identity_authority")
		features.evidence = append(features.evidence, "identity:"+name)
	case authorityIsNetworkCall(lower):
		features.mutates = append(features.mutates, "network_peer_state")
		features.authority = append(features.authority, "network_authority")
		features.evidence = append(features.evidence, "network:"+name)
	case authorityIsConfigMutationCall(lower):
		features.mutates = append(features.mutates, "config_state")
		features.authority = append(features.authority, "config_authority")
		features.evidence = append(features.evidence, "config:"+name)
	case authorityIsFilesystemMutationCall(lower):
		target := "filesystem"
		if len(call.Args) > 0 {
			if lit := authorityStringLiteral(call.Args[0]); lit != "" {
				target = "file:" + lit
			}
		}
		features.mutates = append(features.mutates, target)
		features.authority = append(features.authority, "filesystem_authority")
		features.evidence = append(features.evidence, "filesystem:"+name)
	}
}

func authorityCandidateFromFeatures(f authorityFeatures) (authoritySurfaceCandidate, bool) {
	f.mutates = authorityDedupe(f.mutates)
	f.lifecycle = authorityDedupe(f.lifecycle)
	f.guards = authorityDedupe(f.guards)
	f.authority = authorityDedupe(f.authority)
	f.notes = authorityDedupe(f.notes)
	f.evidence = authorityDedupe(f.evidence)
	if len(f.mutates) == 0 && len(f.lifecycle) == 0 && len(f.guards) == 0 {
		return authoritySurfaceCandidate{}, false
	}
	if authorityPureFilesystemMutation(f) && !f.hasHandler && len(f.guards) == 0 && len(f.lifecycle) == 0 {
		return authoritySurfaceCandidate{}, false
	}
	if len(f.guards) > 0 && len(f.mutates) == 0 && !f.hasHandler {
		return authoritySurfaceCandidate{}, false
	}
	if len(f.lifecycle) == 0 && !f.hasHandler && !authorityHasNamedGovernance(f.authority) && len(f.guards) == 0 {
		return authoritySurfaceCandidate{}, false
	}

	kind := "state_mutation"
	switch {
	case len(f.lifecycle) > 0:
		kind = "lifecycle_control"
	case f.hasHandler && len(f.mutates) > 0 && len(f.guards) > 0:
		kind = "guarded_mutation_handler"
	case f.hasHandler && len(f.mutates) > 0:
		kind = "mutation_handler"
	case f.hasHandler:
		kind = "guard_surface"
	case len(f.guards) > 0 && len(f.mutates) > 0:
		kind = "guarded_state_mutation"
	case len(f.guards) > 0:
		kind = "security_surface"
	}

	notes := append([]string{}, f.notes...)
	if len(f.routes) > 0 {
		for _, route := range f.routes {
			notes = append(notes, "route:"+route)
		}
	}
	if f.hasHandler && len(f.routes) == 0 {
		notes = append(notes, "handler-like function signature observed")
	}
	notes = authorityDedupe(notes)

	id := "candidate.authority." + authoritySlug(strings.TrimSuffix(f.relPath, ".go")) + "." + authoritySlug(f.symbol)
	return authoritySurfaceCandidate{
		ID:                id,
		Class:             "AuthoritySurface",
		Status:            "candidate",
		Confidence:        "candidate",
		Kind:              kind,
		Owner:             f.owner,
		SourceFiles:       []string{f.relPath},
		Symbols:           []string{f.symbol},
		Routes:            f.routes,
		MutatesState:      f.mutates,
		ControlsLifecycle: f.lifecycle,
		RequiredAuthority: f.authority,
		RequiredGuards:    f.guards,
		Notes:             notes,
		Evidence:          f.evidence,
	}, true
}

func renderAuthorityCandidates(root string, cands []authoritySurfaceCandidate) ([]byte, error) {
	var doc authoritySurfaceCandidateDoc
	doc.AuthoritySurfaceCandidates.RepoRoot = root
	doc.AuthoritySurfaceCandidates.GeneratedBy = "sensei extract-authority"
	doc.AuthoritySurfaceCandidates.Candidates = cands

	var buf bytes.Buffer
	buf.WriteString("# GENERATED candidate authority surfaces by `sensei extract-authority`.\n")
	buf.WriteString("# status:candidate only. Review and promote explicitly before treating\n")
	buf.WriteString("# any of these surfaces as authoritative governance facts.\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	_ = enc.Close()
	return buf.Bytes(), nil
}

func authorityOwner(pkg string, fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return pkg
	}
	return authorityExprString(fn.Recv.List[0].Type)
}

func authorityLooksLikeHandler(fn *ast.FuncDecl, routes []string) bool {
	if len(routes) > 0 || strings.Contains(strings.ToLower(fn.Name.Name), "handler") {
		return true
	}
	if fn.Type == nil || fn.Type.Params == nil || len(fn.Type.Params.List) < 2 {
		return false
	}
	params := fn.Type.Params.List
	first := authorityExprString(params[0].Type)
	second := authorityExprString(params[1].Type)
	return strings.Contains(first, "ResponseWriter") && strings.Contains(second, "Request")
}

func authorityCallName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.SelectorExpr:
		left := authorityExprString(t.X)
		if left == "" {
			return t.Sel.Name
		}
		return left + "." + t.Sel.Name
	case *ast.Ident:
		return t.Name
	default:
		return ""
	}
}

func authorityExprString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return authorityExprString(t.X)
	case *ast.SelectorExpr:
		left := authorityExprString(t.X)
		if left == "" {
			return t.Sel.Name
		}
		return left + "." + t.Sel.Name
	default:
		return ""
	}
}

func authorityStringLiteral(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	unq, err := strconv.Unquote(lit.Value)
	if err != nil {
		return strings.Trim(lit.Value, `"`)
	}
	return unq
}

func authorityHandlerName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	default:
		return ""
	}
}

func authorityGuardName(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

func authorityLifecycleName(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return strings.ToLower(name[idx+1:])
	}
	return strings.ToLower(name)
}

func authorityIsGuardCall(lower string) bool {
	return strings.Contains(lower, "validatetoken") ||
		strings.Contains(lower, "authorize") ||
		strings.Contains(lower, "authent") ||
		strings.Contains(lower, "verifytoken")
}

func authorityIsFilesystemMutationCall(lower string) bool {
	return lower == "os.writefile" || lower == "ioutil.writefile" ||
		lower == "os.create" || lower == "os.remove" ||
		lower == "os.rename" || lower == "os.mkdirall"
}

func authorityIsConfigMutationCall(lower string) bool {
	return strings.Contains(lower, "saveconfig") ||
		strings.Contains(lower, "setconfig") ||
		strings.Contains(lower, "writeconfig")
}

func authorityIsLifecycleCall(lower string) bool {
	return strings.HasSuffix(lower, ".start") ||
		strings.HasSuffix(lower, ".stop") ||
		strings.HasSuffix(lower, ".restart") ||
		strings.HasSuffix(lower, ".signal") ||
		strings.Contains(lower, "startservice") ||
		strings.Contains(lower, "stopservice") ||
		strings.Contains(lower, "registerservice") ||
		strings.Contains(lower, "subscribelog")
}

func authorityIsCertificateCall(lower string) bool {
	return strings.Contains(lower, "signcertificate") ||
		strings.Contains(lower, "signcacertificate") ||
		strings.Contains(lower, "signcsr") ||
		strings.Contains(lower, "publickey")
}

func authorityIsIdentityCall(lower string) bool {
	return strings.Contains(lower, "registeradminaccount") ||
		strings.Contains(lower, "createadminrole") ||
		strings.Contains(lower, "createrole") ||
		strings.Contains(lower, "generatetoken")
}

func authorityIsNetworkCall(lower string) bool {
	return strings.Contains(lower, "updatepeer") ||
		strings.Contains(lower, "settext") ||
		strings.Contains(lower, "removetext") ||
		strings.Contains(lower, "registerdns")
}

func authorityDedupe(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func authorityExcludedDir(name string) bool {
	if bootstrapExcludedDir(name) {
		return true
	}
	switch name {
	case "work", "records":
		return true
	}
	return false
}

func authoritySlug(in string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(in) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('.')
	}
	parts := strings.FieldsFunc(b.String(), func(r rune) bool { return r == '.' })
	return strings.Join(parts, ".")
}

func authorityKindSummary(kinds map[string]int) []string {
	var keys []string
	for k := range kinds {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s=%d", k, kinds[k]))
	}
	return out
}

func authorityPureFilesystemMutation(f authorityFeatures) bool {
	if len(f.mutates) == 0 {
		return false
	}
	for _, item := range f.mutates {
		if item != "filesystem" && !strings.HasPrefix(item, "file:") {
			return false
		}
	}
	for _, item := range f.authority {
		if item != "filesystem_authority" {
			return false
		}
	}
	return true
}

func authorityHasNamedGovernance(authority []string) bool {
	for _, item := range authority {
		switch item {
		case "config_authority", "identity_authority", "network_authority", "certificate_authority":
			return true
		}
	}
	return false
}
