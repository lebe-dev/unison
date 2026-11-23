//go:build !unix

package keystore

import "os"

// checkFile accepts every secret file: the platform has no owner and permission
// bits to check.
func checkFile(os.FileInfo) error { return nil }
