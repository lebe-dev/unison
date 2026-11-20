package keystore

import "github.com/lebe-dev/unison/internal/keystorename"

// Service returns the Secret Service name of a data directory. The default data
// directory uses the shared name, every other directory a name of its own, so
// two daemons on one machine keep separate secrets.
func Service(dataDir, defaultDir string) string {
	return keystorename.Service(dataDir, defaultDir)
}
