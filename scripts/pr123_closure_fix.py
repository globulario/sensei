#!/usr/bin/env python3
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected exactly one match, found {count}")
    return text.replace(old, new, 1)


source_path = Path("cmd/awg/consistency_extract.go")
source = source_path.read_text()
source = replace_once(
    source,
    'import (\n\t"go/ast"\n',
    'import (\n\t"go/ast"\n\t"go/build"\n',
    "go/build import",
)
source = replace_once(
    source,
    '\t\tif !strings.HasSuffix(entry.Name(), ".go") || isTestFile(entry.Name()) || !isSourceFile(entry.Name()) {\n\t\t\treturn nil\n\t\t}\n',
    '\t\tif !strings.HasSuffix(entry.Name(), ".go") || isTestFile(entry.Name()) || !isSourceFile(entry.Name()) {\n\t\t\treturn nil\n\t\t}\n\t\tmatched, matchErr := build.Default.MatchFile(filepath.Dir(path), entry.Name())\n\t\tif matchErr != nil || !matched {\n\t\t\treturn nil\n\t\t}\n',
    "build constraint filtering",
)
old_dispatch = '''func consistencyDetectDispatchIdiom(fi *consistencyFuncInfo) bool {
\tselfText := fi.recvName
\thandlerText := fi.recvName + ".Handler()"
\tisHandlerArg := func(e ast.Expr) bool {
\t\tt := consistencyExprText(fi.src, fi.fset, e)
\t\treturn t == selfText || t == handlerText
\t}
\tfound := false
\tast.Inspect(fi.body, func(n ast.Node) bool {
\t\tif found {
\t\t\treturn false
\t\t}
\t\tswitch v := n.(type) {
\t\tcase *ast.CallExpr:
\t\t\tvar name string
\t\t\tswitch fn := v.Fun.(type) {
\t\t\tcase *ast.SelectorExpr:
\t\t\t\tname = fn.Sel.Name
\t\t\tcase *ast.Ident:
\t\t\t\tname = fn.Name
\t\t\t}
\t\t\tif consistencyDispatchCallNames[name] {
\t\t\t\tfor _, a := range v.Args {
\t\t\t\t\tif isHandlerArg(a) {
\t\t\t\t\t\tfound = true
\t\t\t\t\t\treturn false
\t\t\t\t\t}
\t\t\t\t}
\t\t\t}
\t\tcase *ast.KeyValueExpr:
\t\t\tif id, ok := v.Key.(*ast.Ident); ok && id.Name == "Handler" && isHandlerArg(v.Value) {
\t\t\t\tfound = true
\t\t\t\treturn false
\t\t\t}
\t\t}
\t\treturn true
\t})
\treturn found
}
'''
new_dispatch = '''func consistencyDetectDispatchIdiom(fi *consistencyFuncInfo) bool {
\tselfText := fi.recvName
\thandlerText := fi.recvName + ".Handler()"
\tisHandlerArg := func(e ast.Expr) bool {
\t\tt := consistencyExprText(fi.src, fi.fset, e)
\t\treturn t == selfText || t == handlerText
\t}

\tvar containsHandler func(ast.Expr) bool
\tcontainsHandler = func(e ast.Expr) bool {
\t\tswitch v := e.(type) {
\t\tcase *ast.UnaryExpr:
\t\t\treturn v.Op == token.AND && containsHandler(v.X)
\t\tcase *ast.CompositeLit:
\t\t\tfor _, elt := range v.Elts {
\t\t\t\tkv, ok := elt.(*ast.KeyValueExpr)
\t\t\t\tif !ok {
\t\t\t\t\tcontinue
\t\t\t\t}
\t\t\t\tid, ok := kv.Key.(*ast.Ident)
\t\t\t\tif ok && id.Name == "Handler" && isHandlerArg(kv.Value) {
\t\t\t\t\treturn true
\t\t\t\t}
\t\t\t}
\t\t}
\t\treturn false
\t}

\tserverVars := map[string]bool{}
\tast.Inspect(fi.body, func(n ast.Node) bool {
\t\tswitch v := n.(type) {
\t\tcase *ast.AssignStmt:
\t\t\tfor i, rhs := range v.Rhs {
\t\t\t\tif !containsHandler(rhs) || i >= len(v.Lhs) {
\t\t\t\t\tcontinue
\t\t\t\t}
\t\t\t\tif id, ok := v.Lhs[i].(*ast.Ident); ok {
\t\t\t\t\tserverVars[id.Name] = true
\t\t\t\t}
\t\t\t}
\t\tcase *ast.DeclStmt:
\t\t\tdecl, ok := v.Decl.(*ast.GenDecl)
\t\t\tif !ok {
\t\t\t\tbreak
\t\t\t}
\t\t\tfor _, spec := range decl.Specs {
\t\t\t\tvalues, ok := spec.(*ast.ValueSpec)
\t\t\t\tif !ok {
\t\t\t\t\tcontinue
\t\t\t\t}
\t\t\t\tfor i, value := range values.Values {
\t\t\t\t\tif containsHandler(value) && i < len(values.Names) {
\t\t\t\t\t\tserverVars[values.Names[i].Name] = true
\t\t\t\t\t}
\t\t\t\t}
\t\t\t}
\t\t}
\t\treturn true
\t})

\tfound := false
\tast.Inspect(fi.body, func(n ast.Node) bool {
\t\tif found {
\t\t\treturn false
\t\t}
\t\tcall, ok := n.(*ast.CallExpr)
\t\tif !ok {
\t\t\treturn true
\t\t}
\t\tvar name string
\t\tvar receiverVar string
\t\tswitch fn := call.Fun.(type) {
\t\tcase *ast.SelectorExpr:
\t\t\tname = fn.Sel.Name
\t\t\tif id, ok := fn.X.(*ast.Ident); ok {
\t\t\t\treceiverVar = id.Name
\t\t\t}
\t\tcase *ast.Ident:
\t\t\tname = fn.Name
\t\t}
\t\tif !consistencyDispatchCallNames[name] {
\t\t\treturn true
\t\t}
\t\tif serverVars[receiverVar] {
\t\t\tfound = true
\t\t\treturn false
\t\t}
\t\tfor _, arg := range call.Args {
\t\t\tif isHandlerArg(arg) {
\t\t\t\tfound = true
\t\t\t\treturn false
\t\t\t}
\t\t\tif id, ok := arg.(*ast.Ident); ok && serverVars[id.Name] {
\t\t\t\tfound = true
\t\t\t\treturn false
\t\t\t}
\t\t}
\t\treturn true
\t})
\treturn found
}
'''
source = replace_once(source, old_dispatch, new_dispatch, "dispatch idiom")
source = replace_once(
    source,
    '''\t\tfor _, idxs := range prefixGroups {
\t\t\tfor i := 1; i < len(idxs); i++ {
\t\t\t\tuf.union(idxs[0], idxs[i])
\t\t\t}
\t\t}

\t\t// Signature clustering only bridges methods with NO prefix family of
''',
    '''\t\tfor _, idxs := range prefixGroups {
\t\t\tfor i := 1; i < len(idxs); i++ {
\t\t\t\tuf.union(idxs[0], idxs[i])
\t\t\t}
\t\t}
\t\tvar prefixKeys []string
\t\tfor key := range prefixGroups {
\t\t\tprefixKeys = append(prefixKeys, key)
\t\t}
\t\tsort.Strings(prefixKeys)
\t\tfor i := 0; i < len(prefixKeys); i++ {
\t\t\tfor j := i + 1; j < len(prefixKeys); j++ {
\t\t\t\ta, b := prefixKeys[i], prefixKeys[j]
\t\t\t\tif strings.HasPrefix(a, b+".") || strings.HasPrefix(b, a+".") {
\t\t\t\t\tuf.union(prefixGroups[a][0], prefixGroups[b][0])
\t\t\t\t}
\t\t\t}
\t\t}

\t\t// Signature clustering only bridges methods with NO prefix family of
''',
    "nested prefix union",
)
source_path.write_text(source)


test_path = Path("cmd/awg/consistency_extract_test.go")
tests = test_path.read_text()
addition = r'''

func TestConsistencyCheckBuildConstraints_IgnoreMutuallyExclusiveFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "active.go"), `package sample

type node struct { children []*node }
func (n *node) active() *node { return n.children[0] }
`)
	writeFile(t, filepath.Join(root, "ignored.go"), `//go:build ignore

package sample

type ignoredNode struct { children []*ignoredNode }
func (n *ignoredNode) ignored() *ignoredNode { return n.children[len(n.children)-1] }
`)
	got, err := extractConsistencyCandidates(root)
	if err != nil {
		t.Fatalf("extractConsistencyCandidates: %v", err)
	}
	if c := findConsistencyCandidate(got, "divergent_index_shape", "children"); c != nil {
		t.Fatalf("build-incompatible file contributed evidence: %+v", c)
	}
}

func TestConsistencyCheckDispatchIdiom_HandlerConstructionWithoutServeDoesNotBridge(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "engine.go"), `package engine

type Server struct{ Handler *Engine }
type Engine struct{}
func (e *Engine) updateRouteTrees() {}
func (e *Engine) ServeHTTP() { e.updateRouteTrees() }
func (e *Engine) Run() error { e.updateRouteTrees(); return nil }
func (e *Engine) RunTLS() error { _ = &Server{Handler: e}; return nil }
`)
	got, err := extractConsistencyCandidates(root)
	if err != nil {
		t.Fatalf("extractConsistencyCandidates: %v", err)
	}
	if c := findConsistencyCandidate(got, "asymmetric_setup_call", "updateroutetrees"); c == nil {
		t.Fatalf("handler construction without a serve call suppressed the real asymmetry: %+v", got)
	}
}

func TestConsistencyCheckAsymmetricCall_NestedPrefixFamiliesAreUnified(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "store.go"), `package store

type Store struct{}
func (s *Store) prepare() {}
func (s *Store) GetUser() { s.prepare() }
func (s *Store) GetUserByID() {}
func (s *Store) GetUserByName() {}
`)
	got, err := extractConsistencyCandidates(root)
	if err != nil {
		t.Fatalf("extractConsistencyCandidates: %v", err)
	}
	if c := findConsistencyCandidate(got, "asymmetric_setup_call", "prepare"); c == nil {
		t.Fatalf("nested Get.User and Get.User.By prefix families were not unified: %+v", got)
	}
}
'''
if "TestConsistencyCheckBuildConstraints_IgnoreMutuallyExclusiveFile" not in tests:
    tests += addition
test_path.write_text(tests)


failure_path = Path("docs/awareness/failure_modes.yaml")
failure = failure_path.read_text()
scar_id = "failure.intent_mine_citation_cage_rejected_claude_cli_citations_miss"
if scar_id not in failure:
    failure += '''\n  - id: failure.intent_mine_citation_cage_rejected_claude_cli_citations_miss
    title: 'Intent-mine citation cage rejected claude-cli citations missing the file: prefix'
    protects:
      files:
        - golang/extractor/coldsource/intent_draft.go
    evidence:
      - 'Regression proof: golang/extractor/coldsource/intent_extract_test.go:TestLLMIntentDrafter_ToleratesDroppedFileSchemePrefix'
      - "12/12 claude-cli-drafted intents for a cold-imported gin checkout were rejected as fabricated although every citation resolved after restoring the dropped file: scheme prefix."
    contract: Every LLM-drafted intent source citation must resolve to a real gathered excerpt citation id or be rejected as fabricated.
'''
else:
    failure = failure.replace(
        '''    required_tests:
      - golang/extractor/coldsource/intent_extract_test.go:TestLLMIntentDrafter_ToleratesDroppedFileSchemePrefix
    evidence:
''',
        '''    evidence:
      - 'Regression proof: golang/extractor/coldsource/intent_extract_test.go:TestLLMIntentDrafter_ToleratesDroppedFileSchemePrefix'
''',
        1,
    )
failure_path.write_text(failure)
