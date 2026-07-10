// Positive-control fixture for identity_field_writers_preserve_semantic.
// _.UpdatedUnix = time.Now().Unix() and _.InstalledUnix = time.Now().Unix().
package badfix

import "time"

type installedPackage struct {
	UpdatedUnix   int64
	InstalledUnix int64
}

func observe(existing *installedPackage) {
	existing.UpdatedUnix = time.Now().Unix()   // BAD: wall-clock stomp of install anchor
	existing.InstalledUnix = time.Now().Unix() // BAD: same
}
