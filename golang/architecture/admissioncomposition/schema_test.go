// SPDX-License-Identifier: AGPL-3.0-only

package admissioncomposition

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedSchemasMatchCanonicalSource(t *testing.T) {
	root := filepath.Join("..", "..", "..", "docs", "schemas", "admissioncomposition", "v1")
	for _, filename := range []string{RequestSchemaFilename, ReceiptSchemaFilename} {
		canonical, err := os.ReadFile(filepath.Join(root, filename))
		if err != nil {
			t.Fatal(err)
		}
		embedded, err := embeddedSchemas.ReadFile("schemas/" + filename)
		if err != nil {
			t.Fatal(err)
		}
		if string(canonical) != string(embedded) {
			t.Fatalf("embedded %s drifted from canonical schema", filename)
		}
	}
}

func TestSchemasAcceptValidDocuments(t *testing.T) {
	in := validInput(t)
	req, concrete, err := ComposeRequest(in)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateRequestSchema(data); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}

	decision := canonicalDecision(t, *concrete, *req.AdmissionRequestIdentityDigestSHA256)
	receipt, err := ComposeDecisionReceipt(req, *concrete, decision, "2026-08-01T23:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateReceiptSchema(data); err != nil {
		t.Fatalf("valid decision receipt rejected: %v", err)
	}

	verified, err := AttachVerification(receipt, decision, canonicalVerification(t, decision), "2026-08-01T23:01:00Z")
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(verified)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateReceiptSchema(data); err != nil {
		t.Fatalf("valid verification receipt rejected: %v", err)
	}
}

func TestSchemasRejectUnknownFieldsAndInvalidPresence(t *testing.T) {
	in := validInput(t)
	req, concrete, err := ComposeRequest(in)
	if err != nil {
		t.Fatal(err)
	}
	requestMap := map[string]any{}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &requestMap); err != nil {
		t.Fatal(err)
	}
	requestMap["provider_says_correct"] = true
	data, err = json.Marshal(requestMap)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateRequestSchema(data); err == nil {
		t.Fatal("request schema accepted an unknown authority field")
	}

	decision := canonicalDecision(t, *concrete, *req.AdmissionRequestIdentityDigestSHA256)
	receipt, err := ComposeDecisionReceipt(req, *concrete, decision, "2026-08-01T23:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	receipt.AdmissionVerificationStatus = stringPointer("scope_compliant")
	data, err = json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateReceiptSchema(data); err == nil {
		t.Fatal("admission-decided receipt accepted premature verification evidence")
	}
}

func stringPointer(value string) *string { return &value }
