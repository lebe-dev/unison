package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// base64Seed encodes a seed the way the key file stores it.
func base64Seed(seed []byte) string {
	return base64.StdEncoding.EncodeToString(seed)
}

// newKey returns a deterministic private seed for a test.
func newKey(seed byte) []byte {
	key := make([]byte, ed25519.SeedSize)
	for i := range key {
		key[i] = seed
	}
	return key
}

// writeFile writes a test artifact.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("cannot write %s: %v", name, err)
	}
}

// signedRelease writes the artifacts of a release and signs them.
func signedRelease(t *testing.T, seed byte) (string, ed25519.PublicKey) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "unison-v0.3.0-macos.dmg", "the disk image")
	writeFile(t, dir, "unison-v0.3.0-linux.deb", "the package")
	writeFile(t, dir, ".DS_Store", "a file of the file manager")
	private := newKey(seed)
	if err := signRelease(private, dir); err != nil {
		t.Fatalf("signRelease: %v", err)
	}
	return dir, ed25519.NewKeyFromSeed(private).Public().(ed25519.PublicKey)
}

func TestSignAndVerifyRoundTrip(t *testing.T) {
	dir, public := signedRelease(t, 1)

	count, err := verifyRelease(public, dir)
	if err != nil {
		t.Fatalf("verifyRelease: %v", err)
	}
	if count != 2 {
		t.Fatalf("verified %d artifacts, want 2", count)
	}

	manifest, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(manifest), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("the manifest holds %d lines, want 2", len(lines))
	}
	if !strings.HasSuffix(lines[0], "  unison-v0.3.0-linux.deb") {
		t.Fatalf("the manifest is not sorted by name: %q", lines[0])
	}
}

func TestTamperedArtifactFails(t *testing.T) {
	dir, public := signedRelease(t, 2)
	writeFile(t, dir, "unison-v0.3.0-macos.dmg", "something else")

	if _, err := verifyRelease(public, dir); err == nil {
		t.Fatal("a tampered artifact was accepted")
	}
}

func TestTamperedManifestFails(t *testing.T) {
	dir, public := signedRelease(t, 3)
	manifest := filepath.Join(dir, manifestName)
	text, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	text = []byte(strings.Replace(string(text), "unison-v0.3.0-linux.deb", "unison-v0.3.0-linux.deb ", 1))
	if err := os.WriteFile(manifest, text, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := verifyRelease(public, dir); err == nil {
		t.Fatal("a tampered manifest was accepted")
	}
}

func TestWrongKeyFails(t *testing.T) {
	dir, _ := signedRelease(t, 4)
	other := ed25519.NewKeyFromSeed(newKey(5)).Public().(ed25519.PublicKey)

	if _, err := verifyRelease(other, dir); err == nil {
		t.Fatal("a signature of another key was accepted")
	}
}

func TestManifestLeavesNoTraversal(t *testing.T) {
	for _, line := range []string{
		strings.Repeat("0", 64) + "  ../secret",
		strings.Repeat("0", 64) + "  dir/file",
		strings.Repeat("0", 64) + " file",
		"short",
	} {
		if _, _, err := parseLine(line); err == nil {
			t.Fatalf("the manifest line %q was accepted", line)
		}
	}
}

func TestLoadSeedFromEnvironmentValue(t *testing.T) {
	seed := newKey(6)
	value := base64Seed(seed)
	got, err := loadSeed("", "", value)
	if err != nil {
		t.Fatalf("loadSeed: %v", err)
	}
	if string(got) != string(seed) {
		t.Fatal("the seed was decoded incorrectly")
	}
}
