// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestParsePortFromServiceConfigJSON_Number(t *testing.T) {
	port, err := parsePortFromServiceConfigJSON([]byte(`{"Port":10120}`))
	if err != nil {
		t.Fatalf("parsePortFromServiceConfigJSON number: %v", err)
	}
	if port != 10120 {
		t.Fatalf("port=%d, want 10120", port)
	}
}

func TestParsePortFromServiceConfigJSON_String(t *testing.T) {
	port, err := parsePortFromServiceConfigJSON([]byte(`{"Port":"10120"}`))
	if err != nil {
		t.Fatalf("parsePortFromServiceConfigJSON string: %v", err)
	}
	if port != 10120 {
		t.Fatalf("port=%d, want 10120", port)
	}
}

func TestParsePortFromServiceConfigJSON_Missing(t *testing.T) {
	_, err := parsePortFromServiceConfigJSON([]byte(`{"Name":"x"}`))
	if err == nil {
		t.Fatal("expected error for missing Port")
	}
}

func TestParsePortFromServiceConfigJSON_OutOfRange(t *testing.T) {
	_, err := parsePortFromServiceConfigJSON([]byte(`{"Port":70000}`))
	if err == nil {
		t.Fatal("expected error for out-of-range Port")
	}
}
