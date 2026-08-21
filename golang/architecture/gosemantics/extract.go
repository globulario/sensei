// SPDX-License-Identifier: AGPL-3.0-only

// Package gosemantics extracts bounded, repository-local Go semantic
// observations without executing the target repository or its Tests.
package gosemantics

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/extractbudget"
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
	// Selection records which files the budget admitted and, more
	// importantly, which it did not. A caller that reports observations
	// without it is reporting a bounded search as an exhaustive one.
	Selection extractbudget.Selection
	// Packages is the number of loaded packages actually analysed, after any
	// budget truncation.
	Packages int
	// Cancelled is true when the CALLER's context ended the run.
	// WallClockExhausted is true when this run's own MaxWallClock did. Both
	// arrive as the same context error and mean opposite things to whoever
	// reads the receipt -- one says widen the limit, the other says the limit
	// was never the constraint -- so they are reported separately rather than
	// collapsed into "it stopped early".
	Cancelled          bool
	WallClockExhausted bool
	// Truncated names the budget dimensions this extractor actually cut, so
	// the disposition is reported by the stage that declined the work rather
	// than inferred from a comparison downstream.
	Truncated []string
}

type extractor struct {
	root          string
	selectedFiles map[string]bool
	fset          *token.FileSet
	packages      []*packages.Package
	observations  []Observation
	limitations   []Limitation
	rootComponent *importgraph.RootComponent
}

// Extract loads repository packages, builds type and SSA information, and
// returns only observations whose source and target resolve inside root.
func Extract(root string) (result Result, err error) {
	return ExtractBounded(context.Background(), root, extractbudget.Budget{})
}

// ExtractBoundedOrAbandon is ExtractBounded with the ceiling applied to the
// ANSWER rather than to the loader.
//
// ExtractBounded hands packages.Load the context and that is not sufficient:
// the loader shells out to the Go toolchain and type-checks the module, and it
// returns when that finishes rather than when the deadline passes. Measured on
// Sensei's own repository, a 20-second ceiling produced a 122-second run. A
// ceiling the dominating stage can ignore is not a ceiling.
//
// So this returns as soon as EITHER the load finishes or the context ends. When
// the context ends first, abandoned is true, the result is empty, and the caller
// must record that absence as the clock rather than as a repository with no
// semantics. The load itself keeps running until the toolchain returns and its
// result is discarded: this bounds when an answer is available, never the work
// already committed to, and describing it any other way would restore the
// comfortable version of exactly the defect it repairs.
//
// A context with no deadline and no cancellation runs inline, so an unbounded
// caller is byte-for-byte unchanged.
func ExtractBoundedOrAbandon(ctx context.Context, root string, budget extractbudget.Budget) (result Result, err error, abandoned bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, bounded := ctx.Deadline(); !bounded && ctx.Done() == nil {
		res, err := ExtractBounded(ctx, root, budget)
		return res, err, false
	}
	type outcome struct {
		res Result
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := ExtractBounded(ctx, root, budget)
		done <- outcome{res: res, err: err}
	}()
	select {
	case out := <-done:
		return out.res, out.err, false
	case <-ctx.Done():
		return Result{}, nil, true
	}
}

// ExtractBounded is Extract with a resource contract that actually binds.
//
// The zero Budget is exactly Extract's behaviour, so this is the same code
// path in both cases rather than a bounded variant that could drift from the
// unbounded one.
//
// What each limit actually bounds differs, and saying so precisely matters
// more than making the contract sound uniform:
//
//   - MaxFiles / MaxSourceBytes / the include-exclude scopes bound the
//     ATTRIBUTION set -- which files may produce observations -- and therefore
//     everything downstream of it: observations, evidence receipts, and the
//     source capture I/O that dominates a large run's output. They do NOT
//     narrow the package load, which is still "./..." over the whole module,
//     because an observation about one file needs type information that can
//     come from any file in the module. Loading less would produce cheaper
//     and wronger types.
//   - MaxWallClock is therefore the ONLY limit that bounds the load, through
//     the context packages.Load honours. A repository whose type-check does
//     not finish in the time available is bounded by this and nothing else.
//     The deadline is normally applied by the CALLER (howextract), so the
//     ceiling covers the whole extraction rather than this stage alone; this
//     function derives its own only when it was called without one.
//   - MaxPackages can only bind AFTER the load, since the count is not known
//     until then. It bounds the analysis, not the load.
//
// Stating this rather than papering over it is the point of the checkpoint: a
// contract that claimed all seven limits bound the same stage would be a more
// comfortable and less true description of what a bounded run costs.
func ExtractBounded(ctx context.Context, root string, budget extractbudget.Budget) (result Result, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	budget = budget.Normalize()
	if err := budget.Validate(); err != nil {
		return Result{}, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return Result{}, err
	}
	candidates, err := searchedCandidates(root)
	if err != nil {
		return Result{}, err
	}
	selection := extractbudget.Select(candidates, budget)
	selectedFiles := make(map[string]bool, len(selection.Files))
	for _, path := range selection.Files {
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return Result{}, fmt.Errorf("relativize selected source file %s: %w", path, relErr)
		}
		selectedFiles[filepath.ToSlash(rel)] = true
	}
	// A caller that already applied this budget's wall clock (howextract does,
	// so the ceiling covers the whole extraction rather than only this stage)
	// passes a context that is already deadline-bound. Deriving a second
	// deadline from the same budget would be harmless but redundant; what
	// matters is that a DeadlineExceeded arriving from either source is still
	// attributed to the budget rather than to the caller.
	parent := ctx
	budgetDeadline := false
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		budgetDeadline = budget.MaxWallClock > 0
	}
	if deadline, bounded := budget.Deadline(time.Now()); bounded && !budgetDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
		budgetDeadline = true
	}
	// stopped attributes a context error to whoever actually caused it. The
	// parent is asked first: if the caller had already stopped, the budget's
	// own deadline is incidental and must not take the blame.
	// Attribution order matters, and it changed when the wall clock moved up to
	// howextract so it could cover the whole extraction: the deadline now
	// usually arrives on the INHERITED context, so a parent-error check first
	// would blame the caller for every budget expiry.
	//
	// A deadline is attributed to the budget whenever the budget set one; an
	// explicit cancellation is always the caller's. The residual ambiguity --
	// a caller-imposed deadline firing while a budget wall clock is also set --
	// resolves to the budget, which is the actionable reading: the operator
	// asked for a ceiling and hit one.
	stopped := func() (cancelled, wallClock bool, reason string) {
		err := ctx.Err()
		if err == nil {
			return false, false, ""
		}
		if budgetDeadline && errors.Is(err, context.DeadlineExceeded) {
			return false, true, "semantic package load stopped at the max_wall_clock bound"
		}
		if parent.Err() != nil {
			return true, false, "semantic package load stopped by the caller: " + parent.Err().Error()
		}
		return true, false, "semantic package load stopped: " + err.Error()
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
		Env:     replaceEnvironmentValue(os.Environ(), "GOCACHE", goCache),
		Context: ctx,
	}
	loaded, loadErr := packages.Load(cfg, "./...")
	if loadErr != nil {
		// A deadline or cancellation reaching packages.Load is a governed
		// stop, not an extraction defect: report it as one so the caller can
		// tell "we ran out of time" from "this repository does not type-check".
		if cancelled, wallClock, reason := stopped(); cancelled || wallClock {
			return Result{Selection: selection, Cancelled: cancelled, WallClockExhausted: wallClock,
				Limitations: []Limitation{{Scope: "repository", Reason: reason}},
			}, nil
		}
		return Result{}, loadErr
	}
	if cancelled, wallClock, reason := stopped(); cancelled || wallClock {
		return Result{Selection: selection, Cancelled: cancelled, WallClockExhausted: wallClock,
			Limitations: []Limitation{{Scope: "repository", Reason: reason}},
		}, nil
	}
	loadedPackageCount := len(loaded)
	var packageLimitations []Limitation
	var truncated []string
	if budget.MaxPackages > 0 && len(loaded) > budget.MaxPackages {
		sort.Slice(loaded, func(i, j int) bool { return loaded[i].PkgPath < loaded[j].PkgPath })
		packageLimitations = append(packageLimitations, Limitation{
			Scope: "repository",
			Reason: fmt.Sprintf("extraction budget reached (max_packages): %d of %d loaded package(s) were analysed; the rest were not, beginning at %s",
				budget.MaxPackages, loadedPackageCount, loaded[budget.MaxPackages].PkgPath),
		})
		loaded = loaded[:budget.MaxPackages]
		truncated = append(truncated, extractbudget.DimensionPackages)
	}
	e := &extractor{root: root, selectedFiles: selectedFiles, fset: fset, packages: loaded}
	e.limitations = append(e.limitations, packageLimitations...)
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
	if budget.MaxObservations > 0 && len(e.observations) > budget.MaxObservations {
		e.limitations = append(e.limitations, Limitation{
			Scope: "repository",
			Reason: fmt.Sprintf("extraction budget reached (max_observations): %d of %d observation(s) were kept",
				budget.MaxObservations, len(e.observations)),
		})
		e.observations = e.observations[:budget.MaxObservations]
		truncated = append(truncated, extractbudget.DimensionObservations)
	}
	e.limitations = normalizeLimitations(e.limitations)
	return Result{
		Observations: e.observations,
		Limitations:  e.limitations,
		Selection:    selection,
		Packages:     len(loaded),
		Truncated:    truncated,
	}, nil
}

// searchedCandidates enumerates every file whose source positions may produce
// HOW observations, with the sizes the byte ceiling is measured against.
func searchedCandidates(root string) ([]extractbudget.Candidate, error) {
	selected, err := SearchedFiles(root)
	if err != nil {
		return nil, err
	}
	out := make([]extractbudget.Candidate, 0, len(selected))
	for _, abs := range selected {
		// SearchedFiles also returns go.mod/go.sum, which are semantic INPUTS
		// and can never carry a source position an observation is attributed
		// to. Letting them consume the file and byte ceilings spends the
		// budget on files that cannot produce anything: in a repository with a
		// root go.mod and z.go, `--max-files 1` selected go.mod (paths sort
		// first), rejected every position in z.go, and returned no semantic
		// observations while appearing to allow one source file.
		if filepath.Ext(abs) != ".go" {
			continue
		}
		rel, relErr := filepath.Rel(root, abs)
		if relErr != nil {
			return nil, fmt.Errorf("relativize selected source file %s: %w", abs, relErr)
		}
		var size int64
		if info, statErr := os.Stat(abs); statErr == nil {
			size = info.Size()
		}
		out = append(out, extractbudget.Candidate{RelPath: filepath.ToSlash(rel), AbsPath: abs, Size: size})
	}
	return out, nil
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
	if !e.selectedFiles[rel] {
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
	component, ok := importgraph.ComponentForFile(file, "go")
	if !ok {
		return ""
	}
	return component
}

// IsGeneratedFile returns true if the Go file at the absolute path is generated.
func IsGeneratedFile(path string) bool {
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
	return generated
}

// IsExcludedPath returns true if a repository-relative path should be excluded.
func IsExcludedPath(path string) bool {
	return excludedPath(path)
}

// SearchedFiles returns the sorted, absolute paths whose source positions may
// produce HOW observations. Generated files are excluded from emitted evidence.
func SearchedFiles(root string) ([]string, error) {
	return sourceFiles(root, false)
}

// SemanticInputFiles returns every local file that can affect the semantic
// compiler input. Generated Go files are included because their declarations
// can change type information for observations attributed to selected files.
func SemanticInputFiles(root string) ([]string, error) {
	return sourceFiles(root, true)
}

func sourceFiles(root string, includeGenerated bool) ([]string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	var files []string
	err = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, relErr := filepath.Rel(absRoot, path)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("file outside repository root refused: %s", path)
		}
		relSlash := filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && IsExcludedPath(relSlash) {
				return filepath.SkipDir
			}
			return nil
		}
		if IsExcludedPath(relSlash) {
			return nil
		}
		semanticInput := relSlash == "go.mod" || relSlash == "go.sum" || filepath.Ext(relSlash) == ".go"
		if !semanticInput {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in searched source set refused: %s", path)
		}
		if !includeGenerated && filepath.Ext(relSlash) == ".go" && IsGeneratedFile(path) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
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
	for _, segment := range []string{"/vendor/", "/node_modules/", "/.git/", "/.sensei/", "/generated/", "/testdata/", "/examples/", "/example/"} {
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
	// file is where the FIELD is declared, which is not always where its struct
	// is: an embedded or promoted field comes from another file.
	file string
	line int
}

type boundaryInfo struct {
	symbol string
	file   string
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

				// A field is not necessarily declared where its struct is: an
				// embedded or promoted field lives in another file, and taking
				// the line from the field while taking the file from the struct
				// produced an anchor that cited a line past the end of the file
				// it named. Anchor the field to its own declaration, and drop
				// the field when that declaration is outside the observation
				// surface rather than attributing it to the struct's file.
				fieldFile, fieldLine, ok := e.position(field.Pos())
				if !ok {
					continue
				}
				fields = append(fields, structFieldInfo{
					name:   field.Name(),
					typ:    field.Type().String(),
					tagKey: tagKey,
					tagVal: tagVal,
					rawTag: tag,
					file:   fieldFile,
					line:   fieldLine,
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
		typeName := s.obj.Pkg().Name() + "." + s.obj.Name()
		crossesBoundary := false
		var boundarySymbols []boundaryInfo
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
			for id, obj := range otherPkg.TypesInfo.Uses {
				if obj == s.obj {
					file, line, ok := e.position(id.Pos())
					if !ok {
						e.limitations = append(e.limitations, Limitation{
							Scope:  typeName,
							Reason: "boundary crossing location unresolved: package:" + otherPkg.PkgPath + " (position not found in fileset)",
						})
						continue
					}
					crossesBoundary = true
					boundarySymbols = append(boundarySymbols, boundaryInfo{
						symbol: "package:" + otherPkg.PkgPath,
						file:   file,
						line:   line,
					})
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
						file, line, ok := e.position(fn.Pos())
						if !ok {
							e.limitations = append(e.limitations, Limitation{
								Scope:  typeName,
								Reason: "boundary crossing location unresolved: " + objectSymbol(fn) + " (position not found in fileset)",
							})
							continue
						}
						crossesBoundary = true
						boundarySymbols = append(boundarySymbols, boundaryInfo{
							symbol: objectSymbol(fn),
							file:   file,
							line:   line,
						})
					}
				}
				if typeNameObj, ok := obj.(*types.TypeName); ok && typeNameObj.Exported() {
					if iface, ok := types.Unalias(typeNameObj.Type()).Underlying().(*types.Interface); ok {
						for i := 0; i < iface.NumMethods(); i++ {
							m := iface.Method(i)
							sig := m.Type().(*types.Signature)
							if signatureUsesType(sig, s.named) {
								file, line, ok := e.position(m.Pos())
								if !ok {
									e.limitations = append(e.limitations, Limitation{
										Scope:  typeName,
										Reason: "boundary crossing location unresolved: " + objectSymbol(typeNameObj) + "." + m.Name() + " (position not found in fileset)",
									})
									continue
								}
								crossesBoundary = true
								boundarySymbols = append(boundarySymbols, boundaryInfo{
									symbol: objectSymbol(typeNameObj) + "." + m.Name(),
									file:   file,
									line:   line,
								})
							}
						}
					}
					if namedType, ok := types.Unalias(typeNameObj.Type()).(*types.Named); ok {
						for i := 0; i < namedType.NumMethods(); i++ {
							m := namedType.Method(i)
							if m.Exported() {
								sig := m.Type().(*types.Signature)
								if signatureUsesType(sig, s.named) {
									file, line, ok := e.position(m.Pos())
									if !ok {
										e.limitations = append(e.limitations, Limitation{
											Scope:  typeName,
											Reason: "boundary crossing location unresolved: " + objectSymbol(m) + " (position not found in fileset)",
										})
										continue
									}
									crossesBoundary = true
									boundarySymbols = append(boundarySymbols, boundaryInfo{
										symbol: objectSymbol(m),
										file:   file,
										line:   line,
									})
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

			// Emit has_serialized_field for tagged, neutral has_field for untagged
			for _, f := range s.fields {
				fieldSymbol := typeName + "." + f.name
				if f.tagKey != "" {
					e.add(Observation{
						Kind:       "data_shape",
						Subject:    fieldSymbol,
						Predicate:  "has_serialized_field",
						Object:     f.tagVal,
						File:       f.file,
						Symbol:     fieldSymbol,
						Line:       f.line,
						Confidence: 0.98,
						Meta: map[string]string{
							"field_type":      f.typ,
							"tag":             f.tagKey,
							"serialized_name": f.tagVal,
						},
					})
				} else {
					e.add(Observation{
						Kind:       "data_shape",
						Subject:    fieldSymbol,
						Predicate:  "has_field",
						Object:     f.name,
						File:       f.file,
						Symbol:     fieldSymbol,
						Line:       f.line,
						Confidence: 0.98,
						Meta: map[string]string{
							"field_type": f.typ,
						},
					})
				}
			}

			// Emit uses_data_shape_across_boundary for each crossing
			seenBoundary := make(map[string]bool)
			for _, bs := range boundarySymbols {
				key := bs.symbol + "\x00" + bs.file + "\x00" + strconv.Itoa(bs.line)
				if seenBoundary[key] {
					continue
				}
				seenBoundary[key] = true
				e.add(Observation{
					Kind:       "data_shape",
					Subject:    typeName,
					Predicate:  "uses_data_shape_across_boundary",
					Object:     bs.symbol,
					File:       bs.file, // actual boundary file!
					Symbol:     typeName,
					Line:       bs.line, // actual boundary line!
					Confidence: 0.98,
					Meta:       map[string]string{"boundary": bs.symbol},
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
