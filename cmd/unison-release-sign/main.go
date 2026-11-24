// Command unison-release-sign writes the manifest of a release directory and
// signs it with Ed25519, and verifies an existing release against a public key.
//
// The manifest is the plain text SHA256SUMS file that `sha256sum -c` accepts.
// The signature covers the manifest behind a fixed prefix, so a signature of
// one document cannot be replayed as another.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	manifestName  = "SHA256SUMS"
	signatureName = "SHA256SUMS.sig"
	signatureHead = "unison/1 release\x00"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: unison-release-sign <keygen|sign|verify> [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = runKeygen(os.Args[2:])
	case "sign":
		err = runSign(os.Args[2:])
	case "verify":
		err = runVerify(os.Args[2:])
	default:
		err = fmt.Errorf("unknown subcommand %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "unison-release-sign:", err)
		os.Exit(1)
	}
}

// runKeygen writes a new private key seed and prints the public key.
func runKeygen(args []string) error {
	flags := flag.NewFlagSet("keygen", flag.ContinueOnError)
	key := flags.String("key", "", "path of the private key seed to write")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *key == "" {
		return errors.New("--key is required")
	}
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		return fmt.Errorf("cannot generate a key: %w", err)
	}
	seed := base64.StdEncoding.EncodeToString(private.Seed()) + "\n"
	if err := os.WriteFile(*key, []byte(seed), 0o600); err != nil {
		return fmt.Errorf("cannot write the private key: %w", err)
	}
	// An existing file keeps its old mode, so the seed is restricted again.
	if err := os.Chmod(*key, 0o600); err != nil {
		return fmt.Errorf("cannot restrict the private key: %w", err)
	}
	fmt.Println(base64.StdEncoding.EncodeToString(public))
	return nil
}

// runSign writes the manifest of the directory and its signature.
func runSign(args []string) error {
	flags := flag.NewFlagSet("sign", flag.ContinueOnError)
	dir := flags.String("dir", "release", "directory of the release artifacts")
	key := flags.String("key", "", "path of the private key seed")
	if err := flags.Parse(args); err != nil {
		return err
	}
	seed, err := loadSeed(*key, os.Getenv("UNISON_RELEASE_SIGN_KEY_FILE"), os.Getenv("UNISON_RELEASE_SIGN_KEY"))
	if err != nil {
		return err
	}
	return signRelease(seed, *dir)
}

// runVerify checks the signature and every hash of the directory.
func runVerify(args []string) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	dir := flags.String("dir", "release", "directory of the release artifacts")
	publicKey := flags.String("pubkey", filepath.Join("packaging", "release-public-key.txt"), "path of the public key")
	if err := flags.Parse(args); err != nil {
		return err
	}
	count, err := verifyReleaseFile(*publicKey, *dir)
	if err != nil {
		return err
	}
	fmt.Printf("verified %d artifacts in %s\n", count, *dir)
	return nil
}

// loadSeed reads the private key seed from the flag, the environment file or
// the environment value, in that order.
func loadSeed(path, envFile, envValue string) ([]byte, error) {
	switch {
	case path != "":
		return readSeedFile(path)
	case envFile != "":
		return readSeedFile(envFile)
	case envValue != "":
		return decodeSeed(envValue)
	default:
		return nil, errors.New("no private key: set --key, UNISON_RELEASE_SIGN_KEY_FILE or UNISON_RELEASE_SIGN_KEY")
	}
}

// readSeedFile reads a base64 encoded seed from a file.
func readSeedFile(path string) ([]byte, error) {
	// #nosec G304,G703 -- the caller names the key file.
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read the private key: %w", err)
	}
	return decodeSeed(string(text))
}

// decodeSeed decodes a base64 encoded 32 byte seed.
func decodeSeed(text string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil {
		return nil, fmt.Errorf("the private key is not base64: %w", err)
	}
	if len(raw) != ed25519.SeedSize {
		return nil, fmt.Errorf("the private key holds %d bytes, want %d", len(raw), ed25519.SeedSize)
	}
	return raw, nil
}

// decodePublicKey decodes a base64 encoded 32 byte public key.
func decodePublicKey(text string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil {
		return nil, fmt.Errorf("the public key is not base64: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("the public key holds %d bytes, want %d", len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

// signRelease writes the manifest and the signature of the directory.
func signRelease(seed []byte, dir string) error {
	private := ed25519.NewKeyFromSeed(seed)
	manifest, err := writeManifest(dir)
	if err != nil {
		return err
	}
	signature := ed25519.Sign(private, append([]byte(signatureHead), manifest...))
	text := base64.StdEncoding.EncodeToString(signature) + "\n"
	// #nosec G306,G703 -- the signature is published with the release.
	if err := os.WriteFile(filepath.Join(dir, signatureName), []byte(text), 0o644); err != nil {
		return fmt.Errorf("cannot write the signature: %w", err)
	}
	fmt.Printf("signed %s/%s\n", dir, manifestName)
	return nil
}

// writeManifest hashes every artifact of the directory and writes SHA256SUMS.
func writeManifest(dir string) ([]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read the release directory: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") || name == manifestName || name == signatureName {
			continue
		}
		if strings.ContainsAny(name, "\r\n") {
			return nil, fmt.Errorf("the artifact name %q holds a line break", name)
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("the release directory %s holds no artifacts", dir)
	}
	sort.Strings(names)

	var manifest bytes.Buffer
	for _, name := range names {
		sum, err := fileSum(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&manifest, "%s  %s\n", hex.EncodeToString(sum), name)
	}
	// #nosec G306 -- the manifest is published with the release.
	if err := os.WriteFile(filepath.Join(dir, manifestName), manifest.Bytes(), 0o644); err != nil {
		return nil, fmt.Errorf("cannot write the manifest: %w", err)
	}
	return manifest.Bytes(), nil
}

// verifyReleaseFile verifies the signature and the artifacts of a directory
// against a public key file. It returns the number of verified artifacts.
func verifyReleaseFile(publicKeyPath, dir string) (int, error) {
	// #nosec G304 -- the caller names the public key file.
	text, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return 0, fmt.Errorf("cannot read the public key: %w", err)
	}
	public, err := decodePublicKey(string(text))
	if err != nil {
		return 0, err
	}
	return verifyRelease(public, dir)
}

// verifyRelease verifies the signature and the artifacts of a directory.
func verifyRelease(public ed25519.PublicKey, dir string) (int, error) {
	// #nosec G304 -- the directory is the release directory the caller named.
	manifest, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return 0, fmt.Errorf("cannot read the manifest: %w", err)
	}
	// #nosec G304 -- the directory is the release directory the caller named.
	signatureText, err := os.ReadFile(filepath.Join(dir, signatureName))
	if err != nil {
		return 0, fmt.Errorf("cannot read the signature: %w", err)
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signatureText)))
	if err != nil {
		return 0, fmt.Errorf("the signature is not base64: %w", err)
	}
	if !ed25519.Verify(public, append([]byte(signatureHead), manifest...), signature) {
		return 0, errors.New("the signature does not match the manifest")
	}

	count := 0
	for _, line := range strings.Split(string(manifest), "\n") {
		if line == "" {
			continue
		}
		name, want, err := parseLine(line)
		if err != nil {
			return 0, err
		}
		got, err := fileSum(filepath.Join(dir, name))
		if err != nil {
			return 0, err
		}
		if !bytes.Equal(got, want) {
			return 0, fmt.Errorf("the hash of %s does not match the manifest", name)
		}
		count++
	}
	return count, nil
}

// parseLine reads one line of the manifest and refuses a name that leaves the
// directory.
func parseLine(line string) (string, []byte, error) {
	const hexSize = sha256.Size * 2
	if len(line) < hexSize+2 || line[hexSize:hexSize+2] != "  " {
		return "", nil, fmt.Errorf("the manifest line %q is malformed", line)
	}
	name := line[hexSize+2:]
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return "", nil, fmt.Errorf("the manifest names the artifact %q", name)
	}
	want, err := hex.DecodeString(line[:hexSize])
	if err != nil {
		return "", nil, fmt.Errorf("the manifest line %q is malformed", line)
	}
	return name, want, nil
}

// fileSum returns the SHA-256 hash of a file.
func fileSum(path string) ([]byte, error) {
	// #nosec G304,G703 -- the path is an artifact of the release directory.
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read the artifact: %w", err)
	}
	defer func() { _ = file.Close() }()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return nil, fmt.Errorf("cannot read the artifact: %w", err)
	}
	return sum.Sum(nil), nil
}
