package transport_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every place that builds a gRPC client or server must carry the shared
// ceiling.
//
// This exists because the first attempt at the repair missed one. golang/client
// has TWO dial sites -- Dial and DialConn -- and the gate uses DialConn. Patching
// only the other one left the gate on gRPC's 4 MiB default while the tests for
// the constant all passed, which is a repair that looks complete and changes
// nothing where it matters.
func TestEveryDialSiteCarriesTheCeiling(t *testing.T) {
	const (
		client = "grpc.NewClient("
		server = "grpc.NewServer("
	)
	roots := []string{"../../golang", "../../cmd"}
	found := 0
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			// The lint rules describe gRPC calls in pattern strings; they do not
			// make them.
			if strings.Contains(path, filepath.Join("cmd", "principle-check")) {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			src := string(b)
			if !strings.Contains(src, client) && !strings.Contains(src, server) {
				return nil
			}
			found++
			if !strings.Contains(src, "transport.MaxMessageBytes") {
				t.Errorf("%s builds a gRPC client or server but does not carry transport.MaxMessageBytes: "+
					"large evidence will fail at this end while every other end accepts it", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if found == 0 {
		t.Fatal("no dial or serve site found at all, so this guard proves nothing")
	}
}
