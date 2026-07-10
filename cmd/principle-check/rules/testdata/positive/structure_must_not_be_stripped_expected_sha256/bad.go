// Positive-control fixture for structure_must_not_be_stripped_expected_sha256.
// An identifier literally named expectedSHA256 passed across a call boundary.
package badfix

func verifyBundle(path string, expectedSHA256 string) error { return nil }

func install(path string) error {
	expectedSHA256 := "deadbeef"
	return verifyBundle(path, expectedSHA256) // BAD: subject-stripped digest name
}
