// SPDX-License-Identifier: AGPL-3.0-only

package interpretationclosure

import (
	"context"
	"fmt"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"
)

// GoProbeKind is the deliberately small first mechanically-checkable Go
// vocabulary. It checks facts, not arbitrary natural-language invariants.
// Claims outside this vocabulary remain TruthUnknown rather than being
// silently treated as unsupported authority.
type GoProbeKind string

const (
	GoProbeTypeExists          GoProbeKind = "go_type_exists"
	GoProbeUnderlyingTypeEquals GoProbeKind = "go_underlying_type_equals"
	GoProbeImplementsInterface GoProbeKind = "go_implements_interface"
)

type GoProbe struct {
	ClaimID         string
	Kind            GoProbeKind
	PackagePattern  string
	TypeName        string
	Pointer         bool
	Expected        string
	InterfacePackage string
	InterfaceName   string
	EvidenceReferences []string
}

// CheckGoTruth evaluates structured Go facts against the exact checkout at
// repositoryRoot. It is the initial Gate-1 implementation scope. Other
// languages must return unknown through UnknownTruth; they are not rejected
// merely because no language-specific checker exists yet.
func CheckGoTruth(ctx context.Context, repositoryRoot string, probes []GoProbe) []TruthFinding {
	out := make([]TruthFinding, 0, len(probes))
	for _, probe := range probes {
		out = append(out, checkGoProbe(ctx, repositoryRoot, probe))
	}
	return out
}

func UnknownTruth(claimID, language, checkKind, detail string, evidenceRefs ...string) TruthFinding {
	return TruthFinding{
		ClaimID:            claimID,
		Language:           language,
		CheckKind:          checkKind,
		Status:             TruthUnknown,
		EvidenceReferences: sortedUnique(evidenceRefs),
		Detail:             detail,
	}
}

func checkGoProbe(ctx context.Context, root string, p GoProbe) TruthFinding {
	base := TruthFinding{
		ClaimID:            p.ClaimID,
		Language:           "go",
		CheckKind:          string(p.Kind),
		Subject:            p.PackagePattern + "." + p.TypeName,
		EvidenceReferences: sortedUnique(p.EvidenceReferences),
	}
	if strings.TrimSpace(p.ClaimID) == "" || strings.TrimSpace(p.PackagePattern) == "" || strings.TrimSpace(p.TypeName) == "" {
		base.Status = TruthUnknown
		base.Detail = "probe is missing claim_id, package pattern, or type name"
		return base
	}

	patterns := []string{p.PackagePattern}
	if p.Kind == GoProbeImplementsInterface && p.InterfacePackage != "" && p.InterfacePackage != p.PackagePattern {
		patterns = append(patterns, p.InterfacePackage)
	}
	cfg := &packages.Config{Context: ctx, Dir: root, Mode: packages.NeedName | packages.NeedTypes | packages.NeedDeps}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil || packages.PrintErrors(pkgs) != 0 {
		base.Status = TruthUnknown
		if err != nil { base.Detail = "Go package load failed: " + err.Error() } else { base.Detail = "Go package load reported errors" }
		return base
	}
	targetPkg := findLoadedPackage(pkgs, p.PackagePattern)
	if targetPkg == nil || targetPkg.Types == nil {
		base.Status = TruthUnknown
		base.Detail = "target Go package was not resolved"
		return base
	}
	obj := targetPkg.Types.Scope().Lookup(p.TypeName)

	switch p.Kind {
	case GoProbeTypeExists:
		actual := obj != nil
		expected, ok := parseBool(p.Expected)
		if !ok {
			base.Status = TruthUnknown
			base.Detail = "type-exists probe expected value must be true or false"
			return base
		}
		base.Status = statusFor(actual == expected)
		base.Detail = fmt.Sprintf("type existence actual=%t expected=%t", actual, expected)
		return base

	case GoProbeUnderlyingTypeEquals:
		if obj == nil {
			base.Status = TruthContradicted
			base.Detail = "claimed type does not exist"
			return base
		}
		actual := types.TypeString(obj.Type().Underlying(), qualifier)
		base.Status = statusFor(actual == p.Expected)
		base.Detail = fmt.Sprintf("underlying type actual=%q expected=%q", actual, p.Expected)
		return base

	case GoProbeImplementsInterface:
		if obj == nil || p.InterfacePackage == "" || p.InterfaceName == "" {
			base.Status = TruthUnknown
			base.Detail = "implements-interface probe lacks a resolvable target or interface"
			return base
		}
		ifacePkg := findLoadedPackage(pkgs, p.InterfacePackage)
		if ifacePkg == nil || ifacePkg.Types == nil {
			base.Status = TruthUnknown
			base.Detail = "interface package was not resolved"
			return base
		}
		ifaceObj := ifacePkg.Types.Scope().Lookup(p.InterfaceName)
		if ifaceObj == nil {
			base.Status = TruthContradicted
			base.Detail = "claimed interface does not exist"
			return base
		}
		iface, ok := ifaceObj.Type().Underlying().(*types.Interface)
		if !ok {
			base.Status = TruthContradicted
			base.Detail = "named interface subject is not an interface"
			return base
		}
		var target types.Type = obj.Type()
		if p.Pointer { target = types.NewPointer(target) }
		actual := types.Implements(target, iface.Complete())
		expected, ok := parseBool(p.Expected)
		if !ok {
			base.Status = TruthUnknown
			base.Detail = "implements-interface probe expected value must be true or false"
			return base
		}
		base.Status = statusFor(actual == expected)
		base.Detail = fmt.Sprintf("implements %s.%s actual=%t expected=%t", p.InterfacePackage, p.InterfaceName, actual, expected)
		return base

	default:
		base.Status = TruthUnknown
		base.Detail = "Go truth probe kind is not implemented"
		return base
	}
}

func findLoadedPackage(pkgs []*packages.Package, pattern string) *packages.Package {
	for _, pkg := range pkgs {
		if pkg.PkgPath == pattern || pkg.ID == pattern || pkg.Name == pattern {
			return pkg
		}
	}
	if len(pkgs) == 1 { return pkgs[0] }
	return nil
}

func qualifier(p *types.Package) string {
	if p == nil { return "" }
	return p.Name()
}

func statusFor(equal bool) TruthStatus {
	if equal { return TruthSupported }
	return TruthContradicted
}

func parseBool(s string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true": return true, true
	case "false": return false, true
	default: return false, false
	}
}
