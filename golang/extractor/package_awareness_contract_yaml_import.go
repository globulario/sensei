// SPDX-License-Identifier: AGPL-3.0-only

package extractor

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness-graph/golang/rdf"
)

type packageAwarenessContract struct {
	APIVersion        string                       `yaml:"apiVersion"`
	Kind              string                       `yaml:"kind"`
	Service           string                       `yaml:"service"`
	Package           string                       `yaml:"package"`
	PackageKind       string                       `yaml:"package_kind"`
	Summary           string                       `yaml:"summary"`
	Owns              packageAwarenessStateSurface `yaml:"owns"`
	Reads             packageAwarenessStateSurface `yaml:"reads"`
	Writes            packageAwarenessStateSurface `yaml:"writes"`
	DependsOn         []packageAwarenessDependency `yaml:"depends_on"`
	Invariants        []string                     `yaml:"invariants"`
	ForbiddenFixes    []string                     `yaml:"forbidden_fixes"`
	KnownFailureModes []packageAwarenessFailure    `yaml:"known_failure_modes"`
	SafeDegradedModes []string                     `yaml:"safe_degraded_modes"`
	RemediationFlows  []string                     `yaml:"remediation_workflows"`
	RequiredTests     []string                     `yaml:"required_tests"`
	RequiredPerms     []string                     `yaml:"required_permissions"`
	Admission         packageAwarenessAdmission    `yaml:"admission"`
}

type packageAwarenessStateSurface struct {
	EtcdKeys        []string `yaml:"etcd_keys"`
	SystemdUnits    []string `yaml:"systemd_units"`
	FilesystemPaths []string `yaml:"filesystem_paths"`
	EventTypes      []string `yaml:"event_types"`
}

type packageAwarenessDependency struct {
	Service  string `yaml:"service"`
	Phase    string `yaml:"phase"`
	Required *bool  `yaml:"required"`
	Reason   string `yaml:"reason"`
}

type packageAwarenessFailure struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
	Diagnosis   string `yaml:"diagnosis"`
	Remedy      string `yaml:"remedy"`
}

type packageAwarenessAdmission struct {
	Strict                     *bool `yaml:"strict"`
	AllowUnknownDependencies   *bool `yaml:"allow_unknown_dependencies"`
	AllowPrivilegedStateWrites *bool `yaml:"allow_privileged_state_writes"`
}

func importPackageAwarenessContract(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var c packageAwarenessContract
	if err := yaml.Unmarshal(data, &c); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if c.APIVersion != "awareness.globular.io/v1" || c.Kind != "AwarenessContract" {
		return nil
	}

	pkg := strings.TrimSpace(coalesce(c.Package, c.Service))
	if pkg == "" {
		return fmt.Errorf("package awareness contract missing package/service")
	}
	service := strings.TrimSpace(coalesce(c.Service, pkg))
	subj := rdf.MintIRI(rdf.ClassContract, "contract.package."+pkg+".awareness")
	component := rdf.MintIRI(rdf.ClassComponent, "component.package."+service)

	e.Typed(subj, rdf.ClassContract)
	e.Typed(component, rdf.ClassComponent)
	e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(pkg+" package awareness"))
	emitOptLit(e, subj, rdf.PropComment, strings.Join(strings.Fields(c.Summary), " "))
	emitOptLit(e, subj, rdf.PropKind, c.PackageKind)
	e.Triple(subj, rdf.IRI(rdf.PropAssertionMethod), rdf.Lit("declared"))
	e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
	e.Triple(subj, rdf.IRI(rdf.PropExposedBy), component)
	e.Triple(component, rdf.IRI(rdf.PropExposesContract), subj)

	emitBareEdges(e, subj, rdf.PropConstrainedByInvariant, rdf.ClassInvariant, c.Invariants)
	emitBareEdges(e, subj, rdf.PropForbids, rdf.ClassForbiddenFix, c.ForbiddenFixes)
	emitBareEdges(e, subj, rdf.PropRequiresTest, rdf.ClassTest, c.RequiredTests)
	for _, fm := range c.KnownFailureModes {
		if id := strings.TrimSpace(fm.ID); id != "" {
			e.Triple(subj, rdf.IRI(rdf.PropViolatedBy), rdf.MintIRI(rdf.ClassFailureMode, id))
		}
		emitOptLit(e, subj, rdf.PropRequiresVerification, strings.TrimSpace(fm.Diagnosis))
		emitOptLit(e, subj, rdf.PropRequiresVerification, strings.TrimSpace(fm.Remedy))
	}
	for _, d := range c.DependsOn {
		dep := strings.TrimSpace(d.Service)
		if dep == "" {
			continue
		}
		depIRI := rdf.MintIRI(rdf.ClassComponent, "component.package."+dep)
		e.Triple(subj, rdf.IRI(rdf.PropDependsOn), depIRI)
		emitOptLit(e, subj, rdf.PropRequiresVerification, dependencySummary(d))
	}

	for _, s := range stateSurfaceSummaries("owns", c.Owns) {
		e.Triple(subj, rdf.IRI(rdf.PropOwnsState), rdf.Lit(s))
	}
	for _, s := range stateSurfaceSummaries("reads", c.Reads) {
		e.Triple(subj, rdf.IRI(rdf.PropRequiresVerification), rdf.Lit(s))
	}
	for _, s := range stateSurfaceSummaries("writes", c.Writes) {
		e.Triple(subj, rdf.IRI(rdf.PropRequiresVerification), rdf.Lit(s))
	}
	emitOptLits(e, subj, rdf.PropRequiresVerification, c.SafeDegradedModes)
	emitOptLits(e, subj, rdf.PropRequiresVerification, c.RemediationFlows)
	emitOptLits(e, subj, rdf.PropRequiresVerification, c.RequiredPerms)
	emitOptLit(e, subj, rdf.PropRequiresVerification, admissionSummary(c.Admission))
	return nil
}

func dependencySummary(d packageAwarenessDependency) string {
	service := strings.TrimSpace(d.Service)
	if service == "" {
		return ""
	}
	parts := []string{"depends_on=" + service}
	if phase := strings.TrimSpace(d.Phase); phase != "" {
		parts = append(parts, "phase="+phase)
	}
	if d.Required != nil {
		parts = append(parts, fmt.Sprintf("required=%t", *d.Required))
	}
	if reason := strings.Join(strings.Fields(d.Reason), " "); reason != "" {
		parts = append(parts, "reason="+reason)
	}
	return strings.Join(parts, "; ")
}

func stateSurfaceSummaries(prefix string, s packageAwarenessStateSurface) []string {
	var out []string
	appendAll := func(kind string, values []string) {
		for _, v := range values {
			if v = strings.TrimSpace(v); v != "" {
				out = append(out, prefix+"."+kind+"="+v)
			}
		}
	}
	appendAll("etcd", s.EtcdKeys)
	appendAll("systemd", s.SystemdUnits)
	appendAll("filesystem", s.FilesystemPaths)
	appendAll("event", s.EventTypes)
	return out
}

func admissionSummary(a packageAwarenessAdmission) string {
	var parts []string
	if a.Strict != nil {
		parts = append(parts, fmt.Sprintf("admission.strict=%t", *a.Strict))
	}
	if a.AllowUnknownDependencies != nil {
		parts = append(parts, fmt.Sprintf("admission.allow_unknown_dependencies=%t", *a.AllowUnknownDependencies))
	}
	if a.AllowPrivilegedStateWrites != nil {
		parts = append(parts, fmt.Sprintf("admission.allow_privileged_state_writes=%t", *a.AllowPrivilegedStateWrites))
	}
	return strings.Join(parts, "; ")
}
