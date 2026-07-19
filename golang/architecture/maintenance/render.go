// SPDX-License-Identifier: Apache-2.0

package maintenance

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"

	"github.com/globulario/sensei/golang/architecture"
	"gopkg.in/yaml.v3"
)

type reportEnvelope struct {
	ClaimTruthMaintenance Report `json:"claim_truth_maintenance" yaml:"claim_truth_maintenance"`
}

func MarshalCanonicalReportYAML(report Report) ([]byte, error) {
	report = normalizeReport(report)
	return yaml.Marshal(reportEnvelope{ClaimTruthMaintenance: report})
}

func MarshalCanonicalReportJSON(report Report) ([]byte, error) {
	report = normalizeReport(report)
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	if err := enc.Encode(reportEnvelope{ClaimTruthMaintenance: report}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func MarshalCanonicalClaimDocumentYAML(doc architecture.ClaimDocument) ([]byte, error) {
	return architecture.MarshalCanonicalClaimDocumentYAML(doc)
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
	if env.ClaimTruthMaintenance.SchemaVersion == "" && len(env.ClaimTruthMaintenance.ClaimEvaluations) == 0 {
		return Report{}, errors.New("missing claim_truth_maintenance report")
	}
	return normalizeReport(env.ClaimTruthMaintenance), nil
}
