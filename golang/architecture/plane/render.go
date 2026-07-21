// SPDX-License-Identifier: AGPL-3.0-only

package plane

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"

	"gopkg.in/yaml.v3"
)

type reportEnvelope struct {
	ArchitecturalPlaneAssessment Report `json:"architectural_plane_assessment" yaml:"architectural_plane_assessment"`
}

func MarshalCanonicalReportYAML(report Report) ([]byte, error) {
	report = canonicalReport(report)
	return yaml.Marshal(reportEnvelope{ArchitecturalPlaneAssessment: report})
}

func MarshalCanonicalReportJSON(report Report) ([]byte, error) {
	report = canonicalReport(report)
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	if err := enc.Encode(reportEnvelope{ArchitecturalPlaneAssessment: report}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func LoadReport(path string) (Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	return UnmarshalReportYAML(data)
}

func UnmarshalReportYAML(data []byte) (Report, error) {
	var env reportEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return Report{}, err
	}
	if env.ArchitecturalPlaneAssessment.SchemaVersion == "" && len(env.ArchitecturalPlaneAssessment.ClaimAssessments) == 0 {
		return Report{}, errors.New("missing architectural_plane_assessment report")
	}
	return normalizeReport(env.ArchitecturalPlaneAssessment), nil
}
