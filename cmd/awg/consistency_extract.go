// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// consistency_extract.go infers conservative structural-consistency CANDIDATES
// from Go source: two deterministic, no-LLM checks that look for the SHAPE of
// bug a code reviewer catches by noticing sibling functions disagree with each
// other, not by understanding what either one does.
//
//  1. divergent index shape — the same struct field is indexed with a
//     first-element shape (X[0]) in one function and a last-element shape
//     (X[len(X)-1]) in another, in the same package. Real example this was
//     built to catch: a route tree's getValue read n.children[len(n.children)-1]
//     (correct — the wildcard child is always last, per addChild's own
//     contract) while findCaseInsensitivePathRec read n.children[0] (wrong)
//     for the exact same field under the same guard.
//  2. asymmetric setup call — an unexported function/method is reached
//     (directly, or transitively through direct calls) by SOME exported
//     "peer" methods on a receiver type but not others, where peers are
//     methods that share a camelCase name prefix (Run/RunTLS/...) or an
//     identical parameter+result signature (e.g. HTTP-verb registration
//     methods, which share no name but do share a shape). Real example: a
//     route tree's compile step was called from Run() but not from Handler(),
//     RunTLS, RunUnix, RunFd, RunQUIC, or RunListener — all exported peers on
//     the same type.
//
//     Reachability follows one specific, narrow synthetic edge: a method that
//     hands its receiver (or receiver.Handler()) to a Serve-shaped stdlib call
//     (ListenAndServe/ListenAndServeTLS/ListenAndServeQUIC/Serve) is credited
//     with reaching ServeHTTP, since net/http invokes it through the
//     http.Handler interface at runtime — invisible to a plain static call
//     graph, but a well-known, common Go idiom worth bridging explicitly. This
//     is the ONE idiom bridged; a different dynamic-dispatch pattern would
//     need its own targeted bridge, not a general points-to analysis.
//
// Both checks are conservative and cite exactly what was observed (file:line
// pairs, or the caller/non-caller method lists) rather than asserting a bare
// verdict — status: candidate, assertion: inferred, never promoted.

type consistencyCandidate struct {
	ID          string   `yaml:"id"`
	Kind        string   `yaml:"kind"` // "divergent_index_shape" | "asymmetric_setup_call"
	Status      string   `yaml:"status"`
	Assertion   string   `yaml:"assertion"`
	Description string   `yaml:"description"`
	SourceFiles []string `yaml:"source_files,omitempty"`
	Evidence    []string `yaml:"evidence,omitempty"`
}

type consistencyCandidateDoc struct {
	ConsistencyFindings []consistencyCandidate `yaml:"consistency_findings"`
}

// extractConsistencyCandidates walks root's Go source (same exclusion rules
// as the other structural extractors), groups functions by directory (a
// package, in the common one-package-per-directory case), and runs both
// checks within each directory group.
func extractConsistencyCandidates(root string) ([]consistencyCandidate, error) {
	fset := token.NewFileSet()
	byDir := map[string][]*consistencyFuncInfo{}
	var dirOrder []string

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root && (bootstrapExcludedDir(entry.Name()) || strings.HasPrefix(entry.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || isTestFile(entry.Name()) || !isSourceFile(entry.Name()) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			return nil // tolerate unparseable files, same as the import-graph walk
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "." {
			dir = "root"
		}
		if _, seen := byDir[dir]; !seen {
			dirOrder = append(dirOrder, dir)
		}
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			fi := &consistencyFuncInfo{
				name:          fd.Name.Name,
				exported:      ast.IsExported(fd.Name.Name),
				file:          rel,
				line:          fset.Position(fd.Pos()).Line,
				body:          fd.Body,
				ftype:         fd.Type,
				fset:          fset,
				src:           src,
				directCall:    map[string]bool{},
				directCallAny: map[string]bool{},
			}
			if fd.Recv != nil && len(fd.Recv.List) == 1 {
				fi.recvType = goReceiverName(fd.Recv.List[0].Type)
				if len(fd.Recv.List[0].Names) == 1 {
					fi.recvName = fd.Recv.List[0].Names[0].Name
				}
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				id, ok := sel.X.(*ast.Ident)
				if !ok || fi.recvName == "" || id.Name != fi.recvName {
					return true
				}
				fi.directCallAny[sel.Sel.Name] = true
				if !ast.IsExported(sel.Sel.Name) {
					fi.directCall[sel.Sel.Name] = true
				}
				return true
			})
			if fi.recvName != "" {
				fi.dispatchIdiom = consistencyDetectDispatchIdiom(fi)
			}
			byDir[dir] = append(byDir[dir], fi)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(dirOrder)
	var out []consistencyCandidate
	for _, dir := range dirOrder {
		out = append(out, consistencyCheckIndexShape(dir, byDir[dir])...)
		out = append(out, consistencyCheckAsymmetricCall(dir, byDir[dir])...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

type consistencyFuncInfo struct {
	name          string
	exported      bool
	recvType      string // "" if not a method
	recvName      string // receiver variable name, e.g. "n", "engine"
	file          string
	line          int
	body          *ast.BlockStmt
	ftype         *ast.FuncType
	fset          *token.FileSet
	src           []byte
	directCall    map[string]bool // unexported receiver-method names called directly
	directCallAny map[string]bool // ANY receiver-method name called directly (exported or not)
	dispatchIdiom bool            // body feeds the receiver / receiver.Handler() to a Serve-shaped stdlib call
}

func consistencyExprText(src []byte, fset *token.FileSet, e ast.Expr) string {
	start := fset.Position(e.Pos()).Offset
	end := fset.Position(e.End()).Offset
	if start < 0 || end > len(src) || start > end {
		return ""
	}
	return string(src[start:end])
}

// ---------------- Check 1: divergent index shape ----------------

type consistencyShape int

const (
	consistencyShapeUnknown consistencyShape = iota
	consistencyShapeFirst                    // X[0]
	consistencyShapeLastLen                  // X[len(X)-1]
)

type consistencyIndexHit struct {
	fn    string
	file  string
	line  int
	shape consistencyShape
}

func consistencyCheckIndexShape(dir string, funcs []*consistencyFuncInfo) []consistencyCandidate {
	byBase := map[string][]consistencyIndexHit{}
	for _, fi := range funcs {
		ast.Inspect(fi.body, func(n ast.Node) bool {
			idx, ok := n.(*ast.IndexExpr)
			if !ok {
				return true
			}
			base := consistencyExprText(fi.src, fi.fset, idx.X)
			if base == "" || !strings.Contains(base, ".") {
				return true // only field-selector bases, e.g. n.children
			}
			sh := consistencyClassifyShape(idx.Index, base, fi.src, fi.fset)
			if sh == consistencyShapeUnknown {
				return true
			}
			byBase[base] = append(byBase[base], consistencyIndexHit{
				fn:    fi.name,
				file:  fi.file,
				line:  fi.fset.Position(idx.Pos()).Line,
				shape: sh,
			})
			return true
		})
	}

	var bases []string
	for b := range byBase {
		bases = append(bases, b)
	}
	sort.Strings(bases)

	var out []consistencyCandidate
	for _, base := range bases {
		hits := byBase[base]
		hasFirst, hasLast := false, false
		for _, h := range hits {
			if h.shape == consistencyShapeFirst {
				hasFirst = true
			}
			if h.shape == consistencyShapeLastLen {
				hasLast = true
			}
		}
		if !hasFirst || !hasLast {
			continue
		}
		var evidence []string
		filesSeen := map[string]bool{}
		var files []string
		for _, h := range hits {
			if h.shape == consistencyShapeUnknown {
				continue
			}
			evidence = append(evidence, h.file+":"+strconv.Itoa(h.line)+" "+consistencyShapeName(h.shape)+" in func "+h.fn)
			if !filesSeen[h.file] {
				filesSeen[h.file] = true
				files = append(files, h.file)
			}
		}
		sort.Strings(files)
		out = append(out, consistencyCandidate{
			ID:        "consistency.index_shape." + boundarySlug(dir) + "." + boundarySlug(base),
			Kind:      "divergent_index_shape",
			Status:    "candidate",
			Assertion: "inferred",
			Description: "Field " + strconv.Quote(base) + " is indexed with BOTH a first-element shape (X[0]) and a " +
				"last-element shape (X[len(X)-1]) across functions in this package. One of these is very likely " +
				"wrong for at least one caller — verify which shape the field's own writer (e.g. its append/insert " +
				"logic) actually guarantees, and whether every reader agrees with it.",
			SourceFiles: files,
			Evidence:    evidence,
		})
	}
	return out
}

func consistencyShapeName(s consistencyShape) string {
	switch s {
	case consistencyShapeFirst:
		return "[0]"
	case consistencyShapeLastLen:
		return "[len-1]"
	}
	return "?"
}

func consistencyClassifyShape(index ast.Expr, base string, src []byte, fset *token.FileSet) consistencyShape {
	switch e := index.(type) {
	case *ast.BasicLit:
		if e.Value == "0" {
			return consistencyShapeFirst
		}
	case *ast.BinaryExpr:
		if e.Op != token.SUB {
			return consistencyShapeUnknown
		}
		lit, ok := e.Y.(*ast.BasicLit)
		if !ok {
			return consistencyShapeUnknown
		}
		if n, err := strconv.Atoi(lit.Value); err != nil || n != 1 {
			return consistencyShapeUnknown
		}
		call, ok := e.X.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return consistencyShapeUnknown
		}
		fn, ok := call.Fun.(*ast.Ident)
		if !ok || fn.Name != "len" {
			return consistencyShapeUnknown
		}
		if consistencyExprText(src, fset, call.Args[0]) == base {
			return consistencyShapeLastLen
		}
	}
	return consistencyShapeUnknown
}

// ---------------- Handler-dispatch idiom + reachability ----------------

var consistencyDispatchCallNames = map[string]bool{
	"ListenAndServe":     true,
	"ListenAndServeTLS":  true,
	"ListenAndServeQUIC": true,
	"Serve":              true,
}

// consistencyDetectDispatchIdiom reports whether fi's body feeds fi.recvName
// or fi.recvName.Handler() to a Serve-shaped call (as a plain argument, or as
// a "Handler:" field in a struct literal, e.g. &http.Server{Handler: engine}).
func consistencyDetectDispatchIdiom(fi *consistencyFuncInfo) bool {
	selfText := fi.recvName
	handlerText := fi.recvName + ".Handler()"
	isHandlerArg := func(e ast.Expr) bool {
		t := consistencyExprText(fi.src, fi.fset, e)
		return t == selfText || t == handlerText
	}
	found := false
	ast.Inspect(fi.body, func(n ast.Node) bool {
		if found {
			return false
		}
		switch v := n.(type) {
		case *ast.CallExpr:
			var name string
			switch fn := v.Fun.(type) {
			case *ast.SelectorExpr:
				name = fn.Sel.Name
			case *ast.Ident:
				name = fn.Name
			}
			if consistencyDispatchCallNames[name] {
				for _, a := range v.Args {
					if isHandlerArg(a) {
						found = true
						return false
					}
				}
			}
		case *ast.KeyValueExpr:
			if id, ok := v.Key.(*ast.Ident); ok && id.Name == "Handler" && isHandlerArg(v.Value) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// consistencyReachability computes, for every method on a receiver type, the
// full set of method names transitively reachable via direct calls, plus one
// synthetic edge per dispatch-idiom method pointing at "ServeHTTP" (only
// added if the type actually defines one).
func consistencyReachability(methods []*consistencyFuncInfo) map[string]map[string]bool {
	byName := map[string]*consistencyFuncInfo{}
	hasServeHTTP := false
	for _, m := range methods {
		byName[m.name] = m
		if m.name == "ServeHTTP" {
			hasServeHTTP = true
		}
	}
	adjacency := func(name string) []string {
		m, ok := byName[name]
		if !ok {
			return nil
		}
		var out []string
		for c := range m.directCallAny {
			out = append(out, c)
		}
		if hasServeHTTP && m.dispatchIdiom {
			out = append(out, "ServeHTTP")
		}
		return out
	}
	reach := map[string]map[string]bool{}
	for _, m := range methods {
		seen := map[string]bool{}
		queue := adjacency(m.name)
		for len(queue) > 0 {
			next := queue[0]
			queue = queue[1:]
			if seen[next] {
				continue
			}
			seen[next] = true
			queue = append(queue, adjacency(next)...)
		}
		reach[m.name] = seen
	}
	return reach
}

// ---------------- Check 2: asymmetric setup call (family-scoped) ----------------

// consistencySplitCamel breaks an identifier into camelCase/acronym/digit-aware
// words: "RunTLS" -> [Run TLS], "GetInt8Slice" -> [Get Int 8 Slice].
func consistencySplitCamel(s string) []string {
	runes := []rune(s)
	isUpper := func(r rune) bool { return r >= 'A' && r <= 'Z' }
	isLower := func(r rune) bool { return r >= 'a' && r <= 'z' }
	isDigit := func(r rune) bool { return r >= '0' && r <= '9' }
	var words []string
	var cur []rune
	for i, r := range runes {
		if i > 0 {
			prev := runes[i-1]
			boundary := false
			switch {
			case isDigit(prev) != isDigit(r):
				boundary = true
			case isUpper(r) && (isLower(prev) || isDigit(prev)):
				boundary = true
			case isUpper(r) && isUpper(prev) && i+1 < len(runes) && isLower(runes[i+1]):
				boundary = true // "HTTPServer" -> boundary before the 'S' that starts "Server"
			}
			if boundary {
				words = append(words, string(cur))
				cur = nil
			}
		}
		cur = append(cur, r)
	}
	if len(cur) > 0 {
		words = append(words, string(cur))
	}
	return words
}

type consistencyTrieNode struct {
	children map[string]*consistencyTrieNode
	count    int
}

func consistencyNewTrieNode() *consistencyTrieNode {
	return &consistencyTrieNode{children: map[string]*consistencyTrieNode{}}
}

func consistencyBuildTrie(wordLists [][]string) *consistencyTrieNode {
	root := consistencyNewTrieNode()
	for _, words := range wordLists {
		node := root
		for _, w := range words {
			child, ok := node.children[w]
			if !ok {
				child = consistencyNewTrieNode()
				node.children[w] = child
			}
			child.count++
			node = child
		}
	}
	return root
}

// consistencyDeepestSharedPrefix returns the longest word-prefix of `words`
// still shared with >=1 other method (trie count >= 2), or "" if even the
// first word is unique to this method.
func consistencyDeepestSharedPrefix(root *consistencyTrieNode, words []string) string {
	node := root
	best := 0
	for i, w := range words {
		child, ok := node.children[w]
		if !ok {
			break
		}
		if child.count >= 2 {
			best = i + 1
		}
		node = child
		if child.count < 2 {
			break // count is non-increasing with depth
		}
	}
	if best == 0 {
		return ""
	}
	return strings.Join(words[:best], ".")
}

// consistencySignatureKey renders a method's parameter+result TYPES (not
// names) as a structural key, so methods with unrelated names but identical
// shape (HTTP-verb registration methods) still cluster.
func consistencySignatureKey(fi *consistencyFuncInfo) string {
	var params, results []string
	if fi.ftype.Params != nil {
		for _, f := range fi.ftype.Params.List {
			t := consistencyExprText(fi.src, fi.fset, f.Type)
			n := len(f.Names)
			if n == 0 {
				n = 1
			}
			for i := 0; i < n; i++ {
				params = append(params, t)
			}
		}
	}
	if fi.ftype.Results != nil {
		for _, f := range fi.ftype.Results.List {
			t := consistencyExprText(fi.src, fi.fset, f.Type)
			n := len(f.Names)
			if n == 0 {
				n = 1
			}
			for i := 0; i < n; i++ {
				results = append(results, t)
			}
		}
	}
	return strings.Join(params, ",") + "->" + strings.Join(results, ",")
}

type consistencyUnionFind struct{ parent []int }

func consistencyNewUnionFind(n int) *consistencyUnionFind {
	uf := &consistencyUnionFind{parent: make([]int, n)}
	for i := range uf.parent {
		uf.parent[i] = i
	}
	return uf
}
func (uf *consistencyUnionFind) find(x int) int {
	if uf.parent[x] != x {
		uf.parent[x] = uf.find(uf.parent[x])
	}
	return uf.parent[x]
}
func (uf *consistencyUnionFind) union(a, b int) {
	ra, rb := uf.find(a), uf.find(b)
	if ra != rb {
		uf.parent[ra] = rb
	}
}

func consistencyCheckAsymmetricCall(dir string, methods []*consistencyFuncInfo) []consistencyCandidate {
	byRecv := map[string][]*consistencyFuncInfo{}
	for _, fi := range methods {
		if fi.recvType == "" {
			continue
		}
		byRecv[fi.recvType] = append(byRecv[fi.recvType], fi)
	}
	var recvTypes []string
	for t := range byRecv {
		recvTypes = append(recvTypes, t)
	}
	sort.Strings(recvTypes)

	var out []consistencyCandidate
	for _, t := range recvTypes {
		typeMethods := byRecv[t]
		var exported []*consistencyFuncInfo
		for _, m := range typeMethods {
			if m.exported {
				exported = append(exported, m)
			}
		}
		if len(exported) < 2 {
			continue
		}

		reach := consistencyReachability(typeMethods)

		allUnexportedCallees := map[string]bool{}
		for _, m := range typeMethods {
			for c := range m.directCall {
				allUnexportedCallees[c] = true
			}
		}

		wordLists := make([][]string, len(exported))
		for i, m := range exported {
			wordLists[i] = consistencySplitCamel(m.name)
		}
		trie := consistencyBuildTrie(wordLists)

		uf := consistencyNewUnionFind(len(exported))

		prefixGroups := map[string][]int{}
		hasPrefixFamily := make([]bool, len(exported))
		for i, words := range wordLists {
			key := consistencyDeepestSharedPrefix(trie, words)
			if key == "" {
				continue
			}
			prefixGroups[key] = append(prefixGroups[key], i)
			hasPrefixFamily[i] = true
		}
		for _, idxs := range prefixGroups {
			for i := 1; i < len(idxs); i++ {
				uf.union(idxs[0], idxs[i])
			}
		}

		// Signature clustering only bridges methods with NO prefix family of
		// their own (true prefix-singletons, e.g. HTTP-verb methods). A
		// method that already belongs to a real name-based family is
		// excluded here — otherwise a coincidental shared signature (many
		// methods trivially shaped "(any) error") can transitively fuse two
		// unrelated, already-substantial families into one oversized blob.
		sigGroups := map[string][]int{}
		for i, m := range exported {
			if hasPrefixFamily[i] {
				continue
			}
			sigGroups[consistencySignatureKey(m)] = append(sigGroups[consistencySignatureKey(m)], i)
		}
		for _, idxs := range sigGroups {
			if len(idxs) < 2 {
				continue
			}
			for i := 1; i < len(idxs); i++ {
				uf.union(idxs[0], idxs[i])
			}
		}

		families := map[int][]int{}
		for i := range exported {
			families[uf.find(i)] = append(families[uf.find(i)], i)
		}
		var roots []int
		for r := range families {
			roots = append(roots, r)
		}
		sort.Ints(roots)

		for _, r := range roots {
			members := families[r]
			if len(members) < 2 {
				continue // singleton: nothing to compare
			}
			var names []string
			for _, idx := range members {
				names = append(names, exported[idx].name)
			}
			sort.Strings(names)

			var calleeNames []string
			for c := range allUnexportedCallees {
				anyReaches := false
				for _, idx := range members {
					if exported[idx].directCall[c] || reach[exported[idx].name][c] {
						anyReaches = true
						break
					}
				}
				if anyReaches {
					calleeNames = append(calleeNames, c)
				}
			}
			sort.Strings(calleeNames)

			for _, callee := range calleeNames {
				var callers, nonCallers []string
				var files []string
				filesSeen := map[string]bool{}
				for _, idx := range members {
					m := exported[idx]
					if !filesSeen[m.file] {
						filesSeen[m.file] = true
						files = append(files, m.file)
					}
					if m.directCall[callee] || reach[m.name][callee] {
						callers = append(callers, m.name)
					} else {
						nonCallers = append(nonCallers, m.name)
					}
				}
				if len(callers) == 0 || len(nonCallers) == 0 {
					continue // symmetric within this family (directly or via the dispatch idiom)
				}
				sort.Strings(files)
				out = append(out, consistencyCandidate{
					ID:        "consistency.asymmetric_call." + boundarySlug(dir) + "." + boundarySlug(t) + "." + boundarySlug(callee),
					Kind:      "asymmetric_setup_call",
					Status:    "candidate",
					Assertion: "inferred",
					Description: "Within the *" + t + " family {" + strings.Join(names, ", ") + "}, " + callee +
						" is reached by " + strings.Join(callers, ", ") + " but NOT by " + strings.Join(nonCallers, ", ") +
						" — verify whether the non-reaching peers are missing a required setup/precondition call, or " +
						"whether the asymmetry is intentional (peers doing a genuinely different job).",
					SourceFiles: files,
					Evidence: []string{
						"callee: " + callee,
						"reached by: " + strings.Join(callers, ", "),
						"NOT reached by: " + strings.Join(nonCallers, ", "),
					},
				})
			}
		}
	}
	return out
}
