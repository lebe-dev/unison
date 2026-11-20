package keystore

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// Service returns the Secret Service name of a data directory. The default data
// directory uses the shared name, every other directory a name of its own, so
// two daemons on one machine keep separate secrets.
func Service(dataDir, defaultDir string) string {
	if defaultDir != "" && filepath.Clean(dataDir) == filepath.Clean(defaultDir) {
		return DefaultService
	}
	base := filepath.Base(filepath.Clean(dataDir))
	slug := serviceSlug(base)
	if slug == "" {
		slug = "dir"
	}
	// A name the slug had to change may share the slug with another directory,
	// so the path tells them apart.
	if slug != strings.ToLower(base) {
		return DefaultService + ".dev." + slug + "-" + shortHash(dataDir)
	}
	return DefaultService + ".dev." + slug
}

// serviceSlugLimit caps the readable part of a service name.
const serviceSlugLimit = 24

// serviceSlug turns a directory name into a service label of lower case
// letters, digits and single dashes.
func serviceSlug(name string) string {
	var b strings.Builder
	separator := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			separator = false
			continue
		}
		if b.Len() > 0 && !separator {
			b.WriteByte('-')
			separator = true
		}
	}
	slug := strings.TrimRight(b.String(), "-")
	if len(slug) > serviceSlugLimit {
		slug = strings.TrimRight(slug[:serviceSlugLimit], "-")
	}
	return slug
}

// shortHash returns four bytes of the hash of a path, which tells the data
// directories of two daemons apart.
func shortHash(value string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(value)))
	return hex.EncodeToString(sum[:4])
}
