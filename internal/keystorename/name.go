// Package keystorename names the keystore items of a data directory.
//
// The name tells the secrets of two daemons on one machine apart: the default
// data directory shares one name, every other directory a name of its own that
// carries the directory in a readable part and in a hash. A directory always
// maps to the same name, so repeated runs edit the same items.
package keystorename

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// DefaultService is the name of the secrets of the default data directory.
const DefaultService = "app.unison"

// SlugLimit caps the readable part of a name.
const SlugLimit = 24

// Service returns the name of the secrets of a data directory. An empty
// defaultDir means that no directory is the default one.
func Service(dataDir, defaultDir string) string {
	if defaultDir != "" && filepath.Clean(dataDir) == filepath.Clean(defaultDir) {
		return DefaultService
	}
	slug := Slug(filepath.Base(filepath.Clean(dataDir)))
	if slug == "" {
		slug = "dir"
	}
	// Two directories can share a name, so the hash of the whole path tells
	// them apart.
	return DefaultService + ".dev." + slug + "-" + ShortHash(dataDir)
}

// Slug turns a name into a label of lower case letters, digits and single
// dashes, cut to SlugLimit characters.
func Slug(name string) string {
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
	if len(slug) > SlugLimit {
		slug = strings.TrimRight(slug[:SlugLimit], "-")
	}
	return slug
}

// ShortHash returns four bytes of the hash of a path.
func ShortHash(value string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(value)))
	return hex.EncodeToString(sum[:4])
}
