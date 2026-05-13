// Package crypto provides an envelope-encryption Cipher abstraction so the
// admin store can persist OAuth credentials without ever holding the
// plaintext on disk. The Cipher interface has two production-ready
// implementations:
//
//   - AgeFileCipher reads an age identity from a local key file
//     (operator-mounted secret). Used in dev + small single-node deploys
//     and as the default for the all-in-one binary.
//   - NoopCipher panics outside dev mode; intended for tests + local
//     bring-up where no key file exists yet.
//
// The interface is deliberately narrow (Encrypt / Decrypt / KeyVersion)
// so a future KMS-backed implementation drops in without touching
// store/credentials.go.
package crypto

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"filippo.io/age"
)

// Cipher is the boundary between credential storage and the encryption
// implementation. KeyVersion is exposed so PutCredential can write it
// alongside the ciphertext: rotation flows compare current cipher version
// against stored version to decide whether to re-encrypt.
type Cipher interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
	KeyVersion() int
}

// ErrAgeKeyFileNotSet is returned by NewAgeFileCipher when MIO_AGE_KEY_FILE
// is unset and no path is supplied. Callers (typically cmd/gateway and
// cmd/admin bootstrap) decide whether to fall back to NoopCipher (dev) or
// fail-fast (staging/prod).
var ErrAgeKeyFileNotSet = errors.New("crypto: MIO_AGE_KEY_FILE not set")

// AgeFileCipher encrypts to a list of recipients derived from a local
// identity file. The same identity file holds both the recipient (public
// half, used for Encrypt) and the identity (private half, used for Decrypt).
// Single-key deploys keep one identity; rotation introduces a second file
// with KeyVersion=2 and re-encrypts each row.
type AgeFileCipher struct {
	identity   *age.X25519Identity
	recipients []age.Recipient
	keyVersion int
}

// NewAgeFileCipher loads an age identity from path (or MIO_AGE_KEY_FILE if
// path == ""). version is written to ciphertext rows verbatim — callers
// choose 1 for the initial deploy, bump for each rotation.
//
// The key file format is the standard age identity:
//
//	# created: ...
//	# public key: age1...
//	AGE-SECRET-KEY-...
func NewAgeFileCipher(path string, version int) (*AgeFileCipher, error) {
	if path == "" {
		path = os.Getenv("MIO_AGE_KEY_FILE")
	}
	if path == "" {
		return nil, ErrAgeKeyFileNotSet
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("crypto: open age key file %s: %w", path, err)
	}
	defer f.Close() //nolint:errcheck

	identities, err := age.ParseIdentities(f)
	if err != nil {
		return nil, fmt.Errorf("crypto: parse age identities: %w", err)
	}
	if len(identities) == 0 {
		return nil, fmt.Errorf("crypto: age key file %s contains no identities", path)
	}
	x25519, ok := identities[0].(*age.X25519Identity)
	if !ok {
		return nil, fmt.Errorf("crypto: first identity in %s is not X25519", path)
	}
	return &AgeFileCipher{
		identity:   x25519,
		recipients: []age.Recipient{x25519.Recipient()},
		keyVersion: version,
	}, nil
}

// Encrypt wraps plaintext with age (X25519 + ChaCha20-Poly1305).
func (a *AgeFileCipher) Encrypt(plaintext []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, a.recipients...)
	if err != nil {
		return nil, fmt.Errorf("crypto: age encrypt init: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("crypto: age encrypt write: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("crypto: age encrypt close: %w", err)
	}
	return buf.Bytes(), nil
}

// Decrypt unwraps ciphertext produced by Encrypt or a compatible age
// recipient (operator-supplied key files).
func (a *AgeFileCipher) Decrypt(ciphertext []byte) ([]byte, error) {
	r, err := age.Decrypt(bytes.NewReader(ciphertext), a.identity)
	if err != nil {
		return nil, fmt.Errorf("crypto: age decrypt: %w", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("crypto: age decrypt read: %w", err)
	}
	return out, nil
}

// KeyVersion returns the version this Cipher was constructed with.
func (a *AgeFileCipher) KeyVersion() int { return a.keyVersion }

// NoopCipher is a pass-through impl for dev / tests. Encrypt + Decrypt
// return the input unchanged. KeyVersion returns 0 (callers can compare
// against age-cipher versions to detect "still on Noop").
//
// In any environment other than "dev", Encrypt + Decrypt panic so a
// misconfigured staging/prod deploy cannot silently persist plaintext.
type NoopCipher struct {
	env string // gateway/internal/config.Config.Env value
}

// NewNoopCipher wires the deploy env so the panic guard knows whether to
// fire. env values follow gateway/internal/config: dev|staging|prod.
func NewNoopCipher(env string) *NoopCipher { return &NoopCipher{env: env} }

// Encrypt returns the input unchanged. Panics in non-dev.
func (n *NoopCipher) Encrypt(plaintext []byte) ([]byte, error) {
	n.assertDevOrPanic()
	out := make([]byte, len(plaintext))
	copy(out, plaintext)
	return out, nil
}

// Decrypt returns the input unchanged. Panics in non-dev.
func (n *NoopCipher) Decrypt(ciphertext []byte) ([]byte, error) {
	n.assertDevOrPanic()
	out := make([]byte, len(ciphertext))
	copy(out, ciphertext)
	return out, nil
}

// KeyVersion is always 0 for NoopCipher.
func (n *NoopCipher) KeyVersion() int { return 0 }

func (n *NoopCipher) assertDevOrPanic() {
	if n.env != "dev" {
		panic(fmt.Sprintf("crypto: NoopCipher used in non-dev mode (MIO_ENV=%q) — wire MIO_AGE_KEY_FILE", n.env))
	}
}
