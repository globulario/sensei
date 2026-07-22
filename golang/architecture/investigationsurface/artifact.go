// SPDX-License-Identifier: AGPL-3.0-only

// Package investigationsurface exposes read-only and derived-artifact views over
// the Phase 10 investigation owners. It never promotes claims or mutates authored
// awareness.
package investigationsurface

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/globulario/sensei/golang/architecture/investigation"
	"github.com/globulario/sensei/golang/architecture/investigator"
	"gopkg.in/yaml.v3"
)

type ArtifactKind string

const (
	ArtifactInvestigation ArtifactKind = "investigation_document"
	ArtifactResult        ArtifactKind = "investigator_result"
	ArtifactGrounding     ArtifactKind = "grounding_snapshot"
	ArtifactEvidence      ArtifactKind = "evidence_snapshot"
	ArtifactUnknown       ArtifactKind = "unknown"
)

type ValidationReport struct {
	SchemaVersion string       `json:"schema_version" yaml:"schema_version"`
	ArtifactKind  ArtifactKind `json:"artifact_kind" yaml:"artifact_kind"`
	Path          string       `json:"path" yaml:"path"`
	Valid         bool         `json:"valid" yaml:"valid"`
	Mode          string       `json:"mode,omitempty" yaml:"mode,omitempty"`
	DigestSHA256  string       `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
	Error         string       `json:"error,omitempty" yaml:"error,omitempty"`
}

func ReadArtifact(path string, out any) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("artifact path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	target := reflect.ValueOf(out)
	if target.Kind() != reflect.Pointer || target.IsNil() {
		return errors.New("artifact destination must be a non-nil pointer")
	}
	decode := func(unmarshal func([]byte, any) error) error {
		temporary := reflect.New(target.Elem().Type()).Interface()
		if err := unmarshal(data, temporary); err != nil {
			return err
		}
		target.Elem().Set(reflect.ValueOf(temporary).Elem())
		return nil
	}
	if err := decode(json.Unmarshal); err == nil {
		return nil
	}
	if err := decode(yaml.Unmarshal); err == nil {
		return nil
	}
	return fmt.Errorf("%s is neither valid JSON nor YAML for the requested artifact", path)
}

func LoadDocument(path string) (investigation.Document, error) {
	var doc investigation.Document
	if err := ReadArtifact(path, &doc); err != nil {
		return investigation.Document{}, err
	}
	if err := investigation.Validate(doc); err != nil {
		return investigation.Document{}, err
	}
	return doc, nil
}

func LoadResult(path string) (investigator.Result, error) {
	var result investigator.Result
	if err := ReadArtifact(path, &result); err != nil {
		return investigator.Result{}, err
	}
	grounding := GroundingFromResult(result)
	if err := investigator.Validate(result, grounding); err != nil {
		return investigator.Result{}, err
	}
	return result, nil
}

func LoadGrounding(path string) (investigator.GroundingSnapshot, error) {
	var grounding investigator.GroundingSnapshot
	if err := ReadArtifact(path, &grounding); err != nil {
		return investigator.GroundingSnapshot{}, err
	}
	if _, err := investigator.GroundingSnapshotDigest(grounding); err != nil {
		return investigator.GroundingSnapshot{}, err
	}
	return grounding, nil
}

func WriteArtifact(path, format string, value any) error {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "json"
	}
	var (
		data []byte
		err  error
	)
	switch format {
	case "json":
		data, err = json.MarshalIndent(value, "", "  ")
		if err == nil {
			data = append(data, '\n')
		}
	case "yaml", "yml":
		data, err = yaml.Marshal(value)
	default:
		return fmt.Errorf("format must be json or yaml, got %q", format)
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(path) == "" || path == "-" {
		_, err = os.Stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func ValidateArtifact(path string) ValidationReport {
	report := ValidationReport{SchemaVersion: "investigation.surface.validation.v1", Path: path}
	var probe struct {
		Mode           investigation.Mode `json:"mode" yaml:"mode"`
		SchemaVersion  string             `json:"schema_version" yaml:"schema_version"`
		Candidates     json.RawMessage    `json:"candidates" yaml:"-"`
		Files          []string           `json:"files" yaml:"files"`
		SnapshotDigest string             `json:"snapshot_digest_sha256" yaml:"snapshot_digest_sha256"`
	}
	if err := ReadArtifact(path, &probe); err != nil {
		report.Error = err.Error()
		return report
	}
	if probe.Mode != "" {
		doc, err := LoadDocument(path)
		report.ArtifactKind = ArtifactInvestigation
		report.Mode = string(doc.Mode)
		if err != nil {
			report.Error = err.Error()
			return report
		}
		digest, err := investigation.CalculateDocumentDigest(doc)
		if err != nil {
			report.Error = err.Error()
			return report
		}
		report.Valid = true
		report.DigestSHA256 = digest
		return report
	}
	var grounding investigator.GroundingSnapshot
	if err := ReadArtifact(path, &grounding); err == nil && groundingSnapshotPresent(grounding) {
		report.ArtifactKind = ArtifactGrounding
		digest, err := investigator.GroundingSnapshotDigest(grounding)
		if err != nil {
			report.Error = err.Error()
			return report
		}
		report.Valid = true
		report.DigestSHA256 = digest
		return report
	}
	var result investigator.Result
	if err := ReadArtifact(path, &result); err == nil && result.SchemaVersion == investigator.ComposerSchemaVersion {
		report.ArtifactKind = ArtifactResult
		grounding := GroundingFromResult(result)
		if err := investigator.Validate(result, grounding); err != nil {
			report.Error = err.Error()
			return report
		}
		digest, err := investigator.ResultDigest(result)
		if err != nil {
			report.Error = err.Error()
			return report
		}
		report.Valid = true
		report.DigestSHA256 = digest
		return report
	}
	var snapshot EvidenceSnapshot
	if err := ReadArtifact(path, &snapshot); err == nil && snapshot.SchemaVersion == EvidenceSnapshotSchemaVersion {
		report.ArtifactKind = ArtifactEvidence
		if err := ValidateEvidenceSnapshot(snapshot); err != nil {
			report.Error = err.Error()
			return report
		}
		report.Valid = true
		report.DigestSHA256 = snapshot.SnapshotDigestSHA256
		return report
	}
	report.ArtifactKind = ArtifactUnknown
	report.Error = "artifact schema is not recognized by Phase 10.7"
	return report
}

func groundingSnapshotPresent(snapshot investigator.GroundingSnapshot) bool {
	return len(snapshot.Files) > 0 || len(snapshot.Symbols) > 0 || len(snapshot.GraphNodeIDs) > 0 ||
		len(snapshot.ClaimIDs) > 0 || len(snapshot.ObservationIDs) > 0 ||
		len(snapshot.EvidenceReceiptIDs) > 0 || len(snapshot.ExistingQuestionIDs) > 0
}
