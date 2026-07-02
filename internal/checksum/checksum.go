// Package checksum verifies file integrity against an "<algo>:<hex>" spec, where
// algo is one of md5, sha1, or sha256. It supports both streaming verification
// (hash while you write) and verifying a file already on disk.
package checksum

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"
)

// NewHash returns a fresh hasher for algo (md5|sha1|sha256, case-insensitive).
func NewHash(algo string) (hash.Hash, error) {
	switch strings.ToLower(algo) {
	case "md5":
		return md5.New(), nil
	case "sha1":
		return sha1.New(), nil
	case "sha256":
		return sha256.New(), nil
	}
	return nil, fmt.Errorf("unsupported checksum algorithm %q (want md5|sha1|sha256)", algo)
}

// Parse splits "<algo>:<hex-or-url>" into a lowercased algo, the value, and whether
// the value is an http(s) URL (a checksum file to fetch). A hex value is validated
// against the algo's digest size; a URL value is returned for later resolution.
func Parse(spec string) (algo, value string, isURL bool, err error) {
	a, v, ok := strings.Cut(spec, ":")
	if !ok {
		return "", "", false, fmt.Errorf("bad checksum %q: want <algo>:<hex-or-url>", spec)
	}
	h, err := NewHash(a)
	if err != nil {
		return "", "", false, err
	}
	algo = strings.ToLower(a)
	value = strings.TrimSpace(v)
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return algo, value, true, nil
	}
	value = strings.ToLower(value)
	if raw, derr := hex.DecodeString(value); derr != nil || len(raw) != h.Size() {
		return "", "", false, fmt.Errorf("bad %s digest %q: want %d hex chars", algo, value, h.Size()*2)
	}
	return algo, value, false, nil
}

// ValidateSpec reports whether spec is a well-formed checksum (hex or URL). An
// empty spec is valid (it means "no checksum configured").
func ValidateSpec(spec string) error {
	if spec == "" {
		return nil
	}
	_, _, _, err := Parse(spec)
	return err
}

// Verifier hashes everything written to it and checks it against the expected
// digest. A nil *Verifier is a valid no-op (used when no checksum is configured).
type Verifier struct {
	h    hash.Hash
	want string
}

// New returns a Verifier for a concrete (hex) spec, or nil when spec is empty. A
// URL spec must be resolved to its hex form first (see internal/fetch).
func New(spec string) (*Verifier, error) {
	if spec == "" {
		return nil, nil
	}
	algo, value, isURL, err := Parse(spec)
	if err != nil {
		return nil, err
	}
	if isURL {
		return nil, fmt.Errorf("checksum %q is a URL; resolve it to a hex digest before verifying", spec)
	}
	h, _ := NewHash(algo)
	return &Verifier{h: h, want: value}, nil
}

// Write feeds bytes to the underlying hash. A nil Verifier discards them.
func (v *Verifier) Write(p []byte) (int, error) {
	if v == nil {
		return len(p), nil
	}
	return v.h.Write(p)
}

// Check compares the accumulated digest against the expected one. A nil Verifier
// always passes.
func (v *Verifier) Check() error {
	if v == nil {
		return nil
	}
	got := hex.EncodeToString(v.h.Sum(nil))
	if got != v.want {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, v.want)
	}
	return nil
}

// Verify hashes a file already on disk against spec. An empty spec is a no-op.
func Verify(path, spec string) error {
	v, err := New(spec)
	if err != nil || v == nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(v, f); err != nil {
		return err
	}
	return v.Check()
}
