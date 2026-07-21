// SPDX-License-Identifier: AGPL-3.0-only

// Package gosemantics extracts bounded, repository-local Go semantic
// observations without executing the target repository or its Tests.
package gosemantics

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/globulario/sensei/golang/extractor/importgraph"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

const (
	PredicateDefinesSymbol           = "defines_symbol"
	PredicateExportsSymbol           = "exports_symbol"
	PredicateCallsSymbol             = "calls_symbol"
	PredicateImplementsInterface     = "implements_interface"
	PredicateTestCallsSymbol         = "test_calls_symbol"
	PredicateComponentDependsOn      = "component_depends_on_component"
	PredicateEntrypointReachesSymbol = "entrypoint_reaches_symbol"
)

type Observation struct {
	Kind       string
	Subject    string
	Predicate  string
	Object     string
	File       string
	Symbol     string
	Line       int
	Confidence float64
	Meta       map[string]string
}

type Limitation struct {
	Scope  string
	Reason string
}

type Result struct {
	Observations []Observation
	Limitations  []Limitation
}

type extractor struct {
	root          string
	fset          *token.FileSet
	packages      []*packages.Package
	generated     map[string]bool
	observations  []Observation
	limitations   []Limitation
	rootComponent *importgraph.RootComponent
}

// Extract loads repository packages, builds type and SSA information, and
// returns only observations whose source and target resolve inside root.
func Extract(root string) (result Result, err error) {
	root, err = filepath.Abs(root)
	if err != nil {
		return Result{}, err
	}
	fset := token.NewFileSet()
	goCache := filepath.Join(os.TempDir(), "sensei-go-build-cache")
	if err := os.MkdirAll(goCache, 0o755); err != nil {
		return Result{}, fmt.Errorf("create Go semantic analysis cache: %w", err)
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
			packages.NeedDeps | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedTypesSizes |
			packages.NeedSyntax | packages.NeedModule,
		Dir: root, Fset: fset, Tests: true,
		Env: replaceEnvironmentValue(os.Environ(), "GOCACHE", goCache),
	}
	loaded, loadErr := packages.Load(cfg, "./...")
	if loadErr != nil {
		return Result{}, loadErr
	}
	e := &extractor{root: root, fset: fset, packages: loaded, generated: map[string]bool{}}
	e.rootComponent, _ = importgraph.DetectGoRootComponent(root)
	for _, pkg := range loaded {
		for _, pkgErr := range pkg.Errors {
			e.limitations = append(e.limitations, Limitation{Scope: pkg.PkgPath, Reason: pkgErr.Msg})
		}
	}
	e.extractDefinitionsAndInterfaces()
	e.extractComponentDependencies()
	e.extractSSACalls()
	e.extractDataShapes()
	e.observations = normalizeObservations(e.observations)
	e.limitations = normalizeLimitations(e.limitations)
	return Result{Observations: e.observations, Limitations: e.limitations}, nil
}

func replaceEnvironmentValue(environment []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(environment)+1)
	for _, item := range environment {
		if !strings.HasPrefix(item, prefix) {
			out = append(out, item)
		}
	}
	return append(out, prefix+value)
}

func (e *extractor) extractDefinitionsAndInterfaces() {
	type namedType struct {
		named  *types.Named
		object *types.TypeName
		file   string
		line   int
	}
	var interfaces, concretes []namedType
	seenPackage := map[string]bool{}
	for _, pkg := range e.packages {
		if pkg.Types == nil || pkg.TypesInfo == nil || seenPackage[pkg.PkgPath] || !e.packageIsLocal(pkg) {
			continue
		}
		seenPackage[pkg.PkgPath] = true
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			file, line, ok := e.position(obj.Pos())
			if !ok {
				continue
			}
			symbol := objectSymbol(obj)
			e.add(Observation{Kind: "symbol", Subject: pkg.Types.Name(), Predicate: PredicateDefinesSymbol, Object: symbol, File: file, Symbol: symbol, Line: line, Confidence: .98})
			if obj.Exported() {
				e.add(Observation{Kind: "export", Subject: symbol, Predicate: PredicateExportsSymbol, Object: objectKind(obj), File: file, Symbol: symbol, Line: line, Confidence: .98})
			}
			typeName, ok := obj.(*types.TypeName)
			if !ok {
				continue
			}
			named, ok := types.Unalias(typeName.Type()).(*types.Named)
			if !ok {
				continue
			}
			entry := namedType{named: named, object: typeName, file: file, line: line}
			if _, ok := named.Underlying().(*types.Interface); ok {
				interfaces = append(interfaces, entry)
				e.add(Observation{Kind: "contract_seam", Subject: symbol, Predicate: "exports_interface", Object: "interface", File: file, Symbol: symbol, Line: line, Confidence: .98})
			} else {
				concretes = append(concretes, entry)
			}
			for i := 0; i < named.NumMethods(); i++ {
				method := named.Method(i)
				methodFile, methodLine, methodOK := e.position(method.Pos())
				if !methodOK {
					continue
				}
				methodSymbol := objectSymbol(method)
				e.add(Observation{Kind: "symbol", Subject: symbol, Predicate: PredicateDefinesSymbol, Object: methodSymbol, File: methodFile, Symbol: methodSymbol, Line: methodLine, Confidence: .98})
				if method.Exported() {
					e.add(Observation{Kind: "export", Subject: methodSymbol, Predicate: PredicateExportsSymbol, Object: "method", File: methodFile, Symbol: methodSymbol, Line: methodLine, Confidence: .98})
				}
			}
		}
	}
	for _, concrete := range concretes {
		for _, iface := range interfaces {
			interfaceType := iface.named.Underlying().(*types.Interface).Complete()
			if !types.Implements(concrete.named, interfaceType) && !types.Implements(types.NewPointer(concrete.named), interfaceType) {
				continue
			}
			e.add(Observation{
				Kind: "interface", Subject: objectSymbol(concrete.object), Predicate: PredicateImplementsInterface,
				Object: objectSymbol(iface.object), File: concrete.file, Symbol: objectSymbol(concrete.object), Line: concrete.line,
				Confidence: .98, Meta: map[string]string{"interface_file": iface.file},
			})
		}
	}
}

func (e *extractor) extractComponentDependencies() {
	componentByPackage := map[string]string{}
	for _, pkg := range e.packages {
		if !e.packageIsLocal(pkg) {
			continue
		}
		for _, file := range pkg.CompiledGoFiles {
			if rel, ok := e.relativeFile(file); ok {
				if component := e.componentForFile(rel); component != "" {
					componentByPackage[pkg.PkgPath] = component
					break
				}
			}
		}
	}
	for _, pkg := range e.packages {
		if !e.packageIsLocal(pkg) || pkg.TypesInfo == nil {
			continue
		}
		for _, fileAST := range pkg.Syntax {
			file, _, ok := e.position(fileAST.Pos())
			if !ok {
				continue
			}
			sourceComponent := e.componentForFile(file)
			for _, spec := range fileAST.Imports {
				importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
				if unquoteErr != nil {
					continue
				}
				imported := pkg.Imports[importPath]
				if imported == nil {
					continue
				}
				targetComponent := componentByPackage[imported.PkgPath]
				if sourceComponent == "" || targetComponent == "" || sourceComponent == targetComponent {
					continue
				}
				e.add(Observation{Kind: "component_dependency", Subject: sourceComponent, Predicate: PredicateComponentDependsOn,
					Object: targetComponent, File: file, Symbol: sourceComponent, Line: e.fset.Position(spec.Pos()).Line, Confidence: .98})
			}
		}
	}
}

func (e *extractor) extractSSACalls() {
	defer func() {
		if recovered := recover(); recovered != nil {
			e.limitations = append(e.limitations, Limitation{Scope: "go_ssa", Reason: fmt.Sprintf("SSA construction unavailable: %v", recovered)})
		}
	}()
	program, _ := ssautil.AllPackages(e.packages, ssa.InstantiateGenerics)
	program.Build()
	functions := ssautil.AllFunctions(program)
	edges := map[*ssa.Function]map[*ssa.Function]bool{}
	var ordered []*ssa.Function
	for fn := range functions {
		if _, _, ok := e.functionPosition(fn); ok {
			ordered = append(ordered, fn)
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return functionSortKey(ordered[i]) < functionSortKey(ordered[j]) })
	for _, caller := range ordered {
		callerFile, callerLine, ok := e.functionPosition(caller)
		if !ok {
			continue
		}
		callerSymbol := functionSymbol(caller)
		for _, block := range caller.Blocks {
			for _, instruction := range block.Instrs {
				call, ok := instruction.(ssa.CallInstruction)
				if !ok {
					continue
				}
				callee := call.Common().StaticCallee()
				calleeFile, _, calleeOK := e.functionPosition(callee)
				if !calleeOK || caller == callee {
					continue
				}
				if edges[caller] == nil {
					edges[caller] = map[*ssa.Function]bool{}
				}
				edges[caller][callee] = true
				predicate := PredicateCallsSymbol
				kind := "call"
				if strings.HasSuffix(callerFile, "_test.go") && !strings.HasSuffix(calleeFile, "_test.go") {
					predicate = PredicateTestCallsSymbol
					kind = "test_call"
				}
				line := callerLine
				if pos := instruction.Pos(); pos.IsValid() {
					line = e.fset.Position(pos).Line
				}
				e.add(Observation{Kind: kind, Subject: callerSymbol, Predicate: predicate, Object: functionSymbol(callee),
					File: callerFile, Symbol: callerSymbol, Line: line, Confidence: .96, Meta: map[string]string{"target_file": calleeFile}})
			}
		}
	}
	for _, entrypoint := range ordered {
		entryFile, entryLine, ok := e.functionPosition(entrypoint)
		if !ok || strings.HasSuffix(entryFile, "_test.go") || !functionExported(entrypoint) {
			continue
		}
		visited := map[*ssa.Function]bool{entrypoint: true}
		queue := []*ssa.Function{entrypoint}
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			var next []*ssa.Function
			for target := range edges[current] {
				next = append(next, target)
			}
			sort.Slice(next, func(i, j int) bool { return functionSortKey(next[i]) < functionSortKey(next[j]) })
			for _, target := range next {
				if visited[target] {
					continue
				}
				visited[target] = true
				queue = append(queue, target)
				targetFile, _, _ := e.functionPosition(target)
				e.add(Observation{Kind: "reachability", Subject: functionSymbol(entrypoint), Predicate: PredicateEntrypointReachesSymbol,
					Object: functionSymbol(target), File: entryFile, Symbol: functionSymbol(entrypoint), Line: entryLine,
					Confidence: .90, Meta: map[string]string{"target_file": targetFile}})
			}
		}
	}
}

func (e *extractor) add(observation Observation) {
	if observation.Subject == "" || observation.Predicate == "" || observation.Object == "" || observation.File == "" {
		return
	}
	e.observations = append(e.observations, observation)
}

func (e *extractor) position(pos token.Pos) (file string, line int, ok bool) {
	if !pos.IsValid() {
		return "", 0, false
	}
	position := e.fset.Position(pos)
	file, ok = e.relativeFile(position.Filename)
	return file, position.Line, ok
}

func (e *extractor) functionPosition(fn *ssa.Function) (file string, line int, ok bool) {
	if fn == nil || fn.Pkg == nil || fn.Pkg.Pkg == nil {
		return "", 0, false
	}
	return e.position(fn.Pos())
}

func (e *extractor) relativeFile(path string) (string, bool) {
	path = filepath.Clean(path)
	rel, err := filepath.Rel(e.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if excludedPath(rel) || e.isGenerated(path) {
		return "", false
	}
	return rel, true
}

func (e *extractor) packageIsLocal(pkg *packages.Package) bool {
	for _, file := range pkg.CompiledGoFiles {
		if _, ok := e.relativeFile(file); ok {
			return true
		}
	}
	return false
}

func (e *extractor) componentForFile(file string) string {
	if !strings.Contains(file, "/") && e.rootComponent != nil {
		return e.rootComponent.ID
	}
	component, ok := importgraph.ComponentForFile(file)
	if !ok {
		return ""
	}
	return component
}

func (e *extractor) isGenerated(path string) bool {
	if generated, ok := e.generated[path]; ok {
		return generated
	}
	generated := strings.HasSuffix(path, ".pb.go") || strings.HasSuffix(path, "_generated.go")
	if !generated {
		file, err := os.Open(path)
		if err == nil {
			scanner := bufio.NewScanner(file)
			for lines := 0; lines < 8 && scanner.Scan(); lines++ {
				line := scanner.Text()
				if strings.Contains(line, "Code generated") && strings.Contains(line, "DO NOT EDIT") {
					generated = true
					break
				}
			}
			_ = file.Close()
		}
	}
	e.generated[path] = generated
	return generated
}

func objectSymbol(obj types.Object) string {
	if obj == nil || obj.Pkg() == nil {
		return ""
	}
	if fn, ok := obj.(*types.Func); ok {
		signature, _ := fn.Type().(*types.Signature)
		if signature != nil && signature.Recv() != nil {
			return obj.Pkg().Name() + "." + receiverName(signature.Recv().Type()) + "." + obj.Name()
		}
	}
	return obj.Pkg().Name() + "." + obj.Name()
}

func functionSymbol(fn *ssa.Function) string {
	if fn == nil {
		return ""
	}
	if obj := fn.Object(); obj != nil {
		return objectSymbol(obj)
	}
	if parent := fn.Parent(); parent != nil {
		return functionSymbol(parent)
	}
	return ""
}

func functionExported(fn *ssa.Function) bool {
	if fn == nil || fn.Object() == nil {
		return false
	}
	return fn.Object().Exported()
}

func functionSortKey(fn *ssa.Function) string {
	return functionSymbol(fn) + "|" + fn.String()
}

func receiverName(value types.Type) string {
	if pointer, ok := value.(*types.Pointer); ok {
		value = pointer.Elem()
	}
	if named, ok := types.Unalias(value).(*types.Named); ok {
		return named.Obj().Name()
	}
	return types.TypeString(value, func(*types.Package) string { return "" })
}

func objectKind(obj types.Object) string {
	switch obj.(type) {
	case *types.Func:
		return "function"
	case *types.TypeName:
		return "type"
	case *types.Var:
		return "variable"
	case *types.Const:
		return "constant"
	default:
		return "symbol"
	}
}

func excludedPath(path string) bool {
	path = "/" + filepath.ToSlash(path) + "/"
	for _, segment := range []string{"/vendor/", "/.git/", "/.sensei/", "/generated/", "/testdata/", "/examples/", "/example/"} {
		if strings.Contains(path, segment) {
			return true
		}
	}
	return false
}

func normalizeObservations(in []Observation) []Observation {
	sort.Slice(in, func(i, j int) bool {
		return observationKey(in[i]) < observationKey(in[j])
	})
	seen := map[string]bool{}
	out := make([]Observation, 0, len(in))
	for _, observation := range in {
		key := observationKey(observation)
		if !seen[key] {
			seen[key] = true
			out = append(out, observation)
		}
	}
	return out
}

func observationKey(observation Observation) string {
	return strings.Join([]string{observation.Predicate, observation.Subject, observation.Object, observation.File, strconv.Itoa(observation.Line)}, "\x1f")
}

func normalizeLimitations(in []Limitation) []Limitation {
	sort.Slice(in, func(i, j int) bool {
		if in[i].Scope != in[j].Scope {
			return in[i].Scope < in[j].Scope
		}
		return in[i].Reason < in[j].Reason
	})
	seen := map[string]bool{}
	out := make([]Limitation, 0, len(in))
	for _, limitation := range in {
		key := limitation.Scope + "\x00" + limitation.Reason
		if !seen[key] {
			seen[key] = true
			out = append(out, limitation)
		}
	}
	return out
}

// Keep ast imported as an explicit compile-time assertion that the package
// loader supplies parsed Go syntax; dependency extraction walks ImportSpecs.
var _ ast.Node = (*ast.ImportSpec)(nil)

type structFieldInfo struct {
	name   string
	typ    string
	tagKey string
	tagVal string
	rawTag string
	line   int
}

func (e *extractor) extractDataShapes() {
	type structInfo struct {
		named  *types.Named
		obj    *types.TypeName
		file   string
		line   int
		fields []structFieldInfo
	}

	parseTag := func(tag string) (key, name string) {
		tag = strings.Trim(tag, "`")
		for _, k := range []string{"json", "yaml", "bson", "xml", "protobuf"} {
			prefix := k + ":\""
			idx := strings.Index(tag, prefix)
			if idx < 0 {
				continue
			}
			rest := tag[idx+len(prefix):]
			end := strings.Index(rest, "\"")
			if end < 0 {
				continue
			}
			val := strings.Split(rest[:end], ",")[0]
			val = strings.TrimSpace(val)
			if val != "" && val != "-" {
				return k, val
			}
		}
		return "", ""
	}

	var structs []structInfo

	for _, pkg := range e.packages {
		if pkg.Types == nil || pkg.TypesInfo == nil || !e.packageIsLocal(pkg) {
			continue
		}

		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			typeName, ok := obj.(*types.TypeName)
			if !ok {
				continue
			}
			named, ok := types.Unalias(typeName.Type()).(*types.Named)
			if !ok {
				continue
			}
			st, ok := named.Underlying().(*types.Struct)
			if !ok {
				continue
			}
			file, line, ok := e.position(obj.Pos())
			if !ok {
				continue
			}

			var fields []structFieldInfo
			for i := 0; i < st.NumFields(); i++ {
				field := st.Field(i)
				tag := st.Tag(i)
				tagKey, tagVal := parseTag(tag)

				fields = append(fields, structFieldInfo{
					name:   field.Name(),
					typ:    field.Type().String(),
					tagKey: tagKey,
					tagVal: tagVal,
					rawTag: tag,
					line:   e.fset.Position(field.Pos()).Line,
				})
			}

			structs = append(structs, structInfo{
				named:  named,
				obj:    typeName,
				file:   file,
				line:   line,
				fields: fields,
			})
		}
	}

	// For each struct, check if it matches the 5 boundary crossing/serialization paths
	for _, s := range structs {
		crossesBoundary := false
		var boundarySymbols []string
		hasSerializationTag := false

		for _, f := range s.fields {
			if f.tagKey != "" {
				hasSerializationTag = true
			}
		}

		// 1. Referenced in another package (Path 1)
		for _, otherPkg := range e.packages {
			if otherPkg.Types == nil || otherPkg.TypesInfo == nil || otherPkg.PkgPath == s.obj.Pkg().Path() || !e.packageIsLocal(otherPkg) {
				continue
			}
			for _, obj := range otherPkg.TypesInfo.Uses {
				if obj == s.obj {
					crossesBoundary = true
					boundarySymbols = append(boundarySymbols, "package:"+otherPkg.PkgPath)
				}
			}
		}

		// 2. Used in exported function / method / interface method (Paths 2, 3, 4)
		for _, pkg := range e.packages {
			if pkg.Types == nil || !e.packageIsLocal(pkg) {
				continue
			}
			scope := pkg.Types.Scope()
			for _, name := range scope.Names() {
				obj := scope.Lookup(name)
				if fn, ok := obj.(*types.Func); ok && fn.Exported() {
					sig := fn.Type().(*types.Signature)
					if signatureUsesType(sig, s.named) {
						crossesBoundary = true
						boundarySymbols = append(boundarySymbols, objectSymbol(fn))
					}
				}
				if typeName, ok := obj.(*types.TypeName); ok && typeName.Exported() {
					if iface, ok := types.Unalias(typeName.Type()).Underlying().(*types.Interface); ok {
						for i := 0; i < iface.NumMethods(); i++ {
							m := iface.Method(i)
							sig := m.Type().(*types.Signature)
							if signatureUsesType(sig, s.named) {
								crossesBoundary = true
								boundarySymbols = append(boundarySymbols, objectSymbol(typeName)+"."+m.Name())
							}
						}
					}
					if namedType, ok := types.Unalias(typeName.Type()).(*types.Named); ok {
						for i := 0; i < namedType.NumMethods(); i++ {
							m := namedType.Method(i)
							if m.Exported() {
								sig := m.Type().(*types.Signature)
								if signatureUsesType(sig, s.named) {
									crossesBoundary = true
									boundarySymbols = append(boundarySymbols, objectSymbol(m))
								}
							}
						}
					}
				}
			}
		}

		isRecognized := hasSerializationTag || crossesBoundary

		if isRecognized {
			typeName := s.obj.Pkg().Name() + "." + s.obj.Name()

			// Emit declares_data_shape
			e.add(Observation{
				Kind:       "data_shape",
				Subject:    typeName,
				Predicate:  "declares_data_shape",
				Object:     "struct",
				File:       s.file,
				Symbol:     typeName,
				Line:       s.line,
				Confidence: 0.98,
			})

			// Emit has_serialized_field for each field
			for _, f := range s.fields {
				fieldSymbol := typeName + "." + f.name
				serializedName := f.tagVal
				if serializedName == "" {
					serializedName = f.name
				}
				meta := map[string]string{
					"field_type": f.typ,
				}
				if f.tagKey != "" {
					meta["tag"] = f.tagKey
					meta["serialized_name"] = f.tagVal
				}
				e.add(Observation{
					Kind:       "data_shape",
					Subject:    fieldSymbol,
					Predicate:  "has_serialized_field",
					Object:     serializedName,
					File:       s.file,
					Symbol:     fieldSymbol,
					Line:       f.line,
					Confidence: 0.98,
					Meta:       meta,
				})
			}

			// Emit uses_data_shape_across_boundary for each crossing
			seenBoundary := make(map[string]bool)
			for _, bs := range boundarySymbols {
				if seenBoundary[bs] {
					continue
				}
				seenBoundary[bs] = true
				e.add(Observation{
					Kind:       "data_shape",
					Subject:    typeName,
					Predicate:  "uses_data_shape_across_boundary",
					Object:     bs,
					File:       s.file,
					Symbol:     typeName,
					Line:       s.line,
					Confidence: 0.98,
					Meta:       map[string]string{"boundary": bs},
				})
			}
		}
	}
}

func signatureUsesType(sig *types.Signature, target *types.Named) bool {
	params := sig.Params()
	for i := 0; i < params.Len(); i++ {
		if typeContainsTarget(params.At(i).Type(), target) {
			return true
		}
	}
	results := sig.Results()
	for i := 0; i < results.Len(); i++ {
		if typeContainsTarget(results.At(i).Type(), target) {
			return true
		}
	}
	return false
}

func typeContainsTarget(t types.Type, target *types.Named) bool {
	if t == nil {
		return false
	}
	under := t
	for {
		switch x := under.(type) {
		case *types.Pointer:
			under = x.Elem()
		case *types.Slice:
			under = x.Elem()
		case *types.Array:
			under = x.Elem()
		case *types.Map:
			return typeContainsTarget(x.Key(), target) || typeContainsTarget(x.Elem(), target)
		case *types.Chan:
			under = x.Elem()
		default:
			goto done
		}
	}
done:
	if named, ok := under.(*types.Named); ok {
		return named.Obj() == target.Obj()
	}
	return false
}
