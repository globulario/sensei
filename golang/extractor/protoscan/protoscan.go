// SPDX-License-Identifier: Apache-2.0

// Package protoscan parses .proto files into architecture Contract nodes.
//
// One Contract per gRPC service (uml.kind Interface) and one per RPC method
// (uml.kind Operation). Read/write/mutation semantics are inferred from the
// method name; uncertain → "unknown" rather than guessed. Every contract
// carries assertion: inferred — derived from the proto, never hand-authored.
// It never invents APIs: a contract exists only if the .proto declares it.
//
// This is the reusable core behind both the `proto-scan` CLI and `awg bootstrap`.
package protoscan

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/emicklei/proto"
	yaml "gopkg.in/yaml.v3"
)

// Contract mirrors the fields the architecture_contracts importer
// (golang/extractor.importArchitectureContracts) reads. Field order is the YAML
// key order — keep it stable so generated files are deterministic.
type Contract struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Kind        string   `yaml:"kind"`
	Stability   string   `yaml:"stability,omitempty"`
	ReadOrWrite string   `yaml:"read_or_write"`
	Assertion   string   `yaml:"assertion"`
	ExposedBy   []string `yaml:"exposed_by,omitempty"`
	SourceFiles []string `yaml:"source_files"`
	Uml         *UML     `yaml:"uml,omitempty"`
}

// UML is the optional UML profile block emitted on inferred contracts.
type UML struct {
	Kind       string `yaml:"kind"`
	Stereotype string `yaml:"stereotype"`
	View       string `yaml:"view"`
	Confidence string `yaml:"confidence"`
}

// Doc is the top-level `contracts:` document.
type Doc struct {
	Contracts []Contract `yaml:"contracts"`
}

// ParseComponentMap parses repeatable "Service=component.id" pairs.
func ParseComponentMap(pairs []string) (map[string]string, error) {
	m := map[string]string{}
	for _, p := range pairs {
		i := strings.IndexByte(p, '=')
		if i <= 0 || i+1 >= len(p) {
			return nil, fmt.Errorf("invalid component map %q (want Service=component.id)", p)
		}
		m[strings.TrimSpace(p[:i])] = strings.TrimSpace(p[i+1:])
	}
	return m, nil
}

// excludedDir reports whether a directory should be skipped during proto
// discovery (vendor / generated / VCS / build output).
func excludedDir(name string) bool {
	switch name {
	case "vendor", "node_modules", ".git", "dist", "build", "bin", "out",
		"third_party", "thirdparty", "generated", ".awg", "testdata":
		return true
	}
	return false
}

// FindProtoFiles walks root and returns absolute paths to every .proto file,
// skipping common generated/vendor/build directories. Sorted for determinism.
func FindProtoFiles(root string) ([]string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	var out []string
	walkErr := filepath.WalkDir(absRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // tolerate unreadable entries
		}
		if d.IsDir() {
			if p != absRoot && excludedDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".proto") {
			out = append(out, p)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Strings(out)
	return out, nil
}

// ScanProto parses one .proto file and returns its service + RPC contracts.
func ScanProto(path, repoRoot string, componentByService map[string]string) ([]Contract, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	definition, err := proto.NewParser(f).Parse()
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	relPath := path
	if r, err := filepath.Rel(repoRoot, path); err == nil {
		relPath = filepath.ToSlash(r)
	}

	var out []Contract
	for _, el := range definition.Elements {
		svc, ok := el.(*proto.Service)
		if !ok {
			continue
		}
		component := componentByService[svc.Name]
		var exposedBy []string
		if component != "" {
			exposedBy = []string{component}
		}
		svcSnake := snake(svc.Name)

		// Collect this service's RPCs in declaration order.
		var rpcs []*proto.RPC
		for _, se := range svc.Elements {
			if r, ok := se.(*proto.RPC); ok {
				rpcs = append(rpcs, r)
			}
		}

		// Service-level contract.
		svcRW := aggregateReadWrite(rpcs)
		svcDesc := strings.TrimSpace(commentText(svc.Comment))
		methods := make([]string, 0, len(rpcs))
		for _, r := range rpcs {
			methods = append(methods, r.Name)
		}
		svcDesc = joinNonEmpty(svcDesc, fmt.Sprintf("gRPC service %s with %d RPC(s): %s.",
			svc.Name, len(rpcs), strings.Join(methods, ", ")))
		out = append(out, Contract{
			ID:          "contract." + svcSnake,
			Name:        svc.Name,
			Description: svcDesc,
			Kind:        "grpc",
			Stability:   "stable",
			ReadOrWrite: svcRW,
			Assertion:   "inferred",
			ExposedBy:   exposedBy,
			SourceFiles: []string{relPath},
			// UML: a gRPC service maps to a UML Interface in the interaction view.
			Uml: &UML{Kind: "Interface", Stereotype: "grpc_service", View: "interaction", Confidence: "inferred"},
		})

		// One contract per RPC.
		for _, r := range rpcs {
			desc := strings.TrimSpace(commentText(r.Comment))
			desc = joinNonEmpty(desc, signature(r))
			stability := "stable"
			if isDeprecated(r) {
				stability = "deprecated"
			}
			// UML: an RPC maps to a UML Operation; a streaming RPC is stereotyped
			// rpc_stream so interaction diagrams can distinguish it.
			rpcStereotype := "rpc"
			if r.StreamsRequest || r.StreamsReturns {
				rpcStereotype = "rpc_stream"
			}
			out = append(out, Contract{
				ID:          "contract." + svcSnake + "." + snake(r.Name),
				Name:        svc.Name + "." + r.Name,
				Description: desc,
				Kind:        "grpc",
				Stability:   stability,
				ReadOrWrite: classifyReadWrite(r.Name),
				Assertion:   "inferred",
				ExposedBy:   exposedBy,
				SourceFiles: []string{relPath},
				Uml:         &UML{Kind: "Operation", Stereotype: rpcStereotype, View: "interaction", Confidence: "inferred"},
			})
		}
	}
	return out, nil
}

// Render produces the deterministic generated YAML for a contracts document.
// The header + encoding are byte-stable (CI gates compare the committed output).
func Render(doc Doc) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("# GENERATED by cmd/proto-scan — DO NOT EDIT.\n")
	buf.WriteString("# Run `make proto-contracts` to regenerate from the .proto sources.\n")
	buf.WriteString("# Contract nodes inferred from gRPC proto (assertion: inferred).\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	enc.Close()
	return buf.Bytes(), nil
}

// ── name-based read/write inference ────────────────────────────────────────

var readVerbs = map[string]bool{
	"get": true, "list": true, "query": true, "resolve": true, "describe": true,
	"watch": true, "search": true, "check": true, "lookup": true, "fetch": true,
	"read": true, "metadata": true, "briefing": true, "impact": true,
	"preflight": true, "stream": true, "count": true, "status": true, "health": true,
	"info": true, "explain": true, "validate": true, "verify": true, "probe": true,
	"diagnose": true, "snapshot": true, "inspect": true, "show": true, "view": true,
	"peek": true, "ping": true, "subscribe": true, "tail": true,
}

var writeVerbs = map[string]bool{
	"create": true, "update": true, "delete": true, "set": true, "put": true,
	"add": true, "remove": true, "write": true, "promote": true, "apply": true,
	"mutate": true, "register": true, "deregister": true, "patch": true, "save": true,
	"restore": true, "publish": true, "start": true, "stop": true, "enable": true,
	"disable": true, "reload": true, "drain": true, "rotate": true, "sync": true,
	"push": true, "insert": true, "upsert": true, "provision": true, "deprovision": true,
	"attach": true, "detach": true, "grant": true, "revoke": true, "approve": true,
	"reject": true, "cancel": true, "retry": true, "trigger": true, "execute": true,
	"run": true, "advance": true, "rollback": true, "commit": true, "abort": true,
	"scale": true, "migrate": true, "import": true, "export": true, "install": true,
	"uninstall": true, "deploy": true, "send": true, "store": true,
}

// classifyReadWrite infers read | write | unknown from the RPC's leading
// CamelCase word. Honest about uncertainty: a verb in neither set → unknown.
func classifyReadWrite(rpcName string) string {
	w := strings.ToLower(leadingWord(rpcName))
	switch {
	case writeVerbs[w]:
		return "write"
	case readVerbs[w]:
		return "read"
	default:
		return "unknown"
	}
}

// aggregateReadWrite summarises a service: any write surface → read_write;
// all-read → read; otherwise unknown (don't overclaim).
func aggregateReadWrite(rpcs []*proto.RPC) string {
	hasWrite, hasRead, hasUnknown := false, false, false
	for _, r := range rpcs {
		switch classifyReadWrite(r.Name) {
		case "write":
			hasWrite = true
		case "read":
			hasRead = true
		default:
			hasUnknown = true
		}
	}
	switch {
	case hasWrite && (hasRead || hasUnknown):
		return "read_write"
	case hasWrite:
		return "write"
	case hasRead && !hasUnknown:
		return "read"
	case hasRead:
		return "read" // reads + unknowns, no writes → still a read surface
	default:
		return "unknown"
	}
}

// leadingWord returns the first CamelCase word of s (e.g. "EditCheck" -> "Edit").
func leadingWord(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	out := []rune{r[0]}
	for i := 1; i < len(r); i++ {
		if unicode.IsUpper(r[i]) {
			break
		}
		out = append(out, r[i])
	}
	return string(out)
}

// Snake exposes the CamelCase→snake_case conversion used to mint contract ids,
// so other extractors (e.g. grpcwebscan, which links consumption edges to these
// contracts) can derive byte-identical contract.<service> ids.
func Snake(s string) string { return snake(s) }

// snake converts CamelCase to snake_case ("AwarenessGraph" -> "awareness_graph").
func snake(s string) string {
	var b strings.Builder
	r := []rune(s)
	for i, c := range r {
		if unicode.IsUpper(c) {
			if i > 0 && (!unicode.IsUpper(r[i-1]) || (i+1 < len(r) && unicode.IsLower(r[i+1]))) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(c))
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}

func signature(r *proto.RPC) string {
	req := r.RequestType
	if r.StreamsRequest {
		req = "stream " + req
	}
	resp := r.ReturnsType
	if r.StreamsReturns {
		resp = "stream " + resp
	}
	return fmt.Sprintf("rpc %s(%s) returns (%s)", r.Name, req, resp)
}

func isDeprecated(r *proto.RPC) bool {
	for _, o := range r.Options {
		if o.Name == "deprecated" && strings.EqualFold(o.Constant.Source, "true") {
			return true
		}
	}
	return false
}

func commentText(c *proto.Comment) string {
	if c == nil {
		return ""
	}
	lines := make([]string, 0, len(c.Lines))
	for _, l := range c.Lines {
		lines = append(lines, strings.TrimSpace(l))
	}
	return strings.TrimSpace(strings.Join(lines, " "))
}

func joinNonEmpty(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, strings.TrimSpace(p))
		}
	}
	return strings.Join(out, " — ")
}
