package crypto

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
)

// writeAgeKey generates a fresh X25519 identity and writes it to a tmp
// file in the standard age `identity` format. Returns the path.
func writeAgeKey(t *testing.T) string {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "key.txt")
	body := "# created: test\n# public key: " + id.Recipient().String() + "\n" + id.String() + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return path
}

func TestAgeFileCipher_EncryptDecryptRoundtrip(t *testing.T) {
	keyPath := writeAgeKey(t)
	c, err := NewAgeFileCipher(keyPath, 1)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	if c.KeyVersion() != 1 {
		t.Errorf("KeyVersion: %d", c.KeyVersion())
	}

	plain := []byte(`{"access_token":"abc","refresh_token":"xyz"}`)
	ct, err := c.Encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if bytes.Equal(plain, ct) {
		t.Fatal("ciphertext equals plaintext — encryption no-op")
	}
	got, err := c.Decrypt(ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("roundtrip mismatch: got %q want %q", got, plain)
	}
}

func TestAgeFileCipher_MissingKeyFile(t *testing.T) {
	t.Setenv("MIO_AGE_KEY_FILE", "")
	_, err := NewAgeFileCipher("", 1)
	if err == nil {
		t.Fatal("expected ErrAgeKeyFileNotSet")
	}
	if !strings.Contains(err.Error(), "MIO_AGE_KEY_FILE") {
		t.Errorf("error should mention env var: %v", err)
	}
}

func TestAgeFileCipher_VersionTracked(t *testing.T) {
	keyPath := writeAgeKey(t)
	for _, v := range []int{1, 2, 5} {
		c, err := NewAgeFileCipher(keyPath, v)
		if err != nil {
			t.Fatalf("v=%d: %v", v, err)
		}
		if got := c.KeyVersion(); got != v {
			t.Errorf("v=%d: got %d", v, got)
		}
	}
}

func TestNoopCipher_DevPasses(t *testing.T) {
	c := NewNoopCipher("dev")
	if c.KeyVersion() != 0 {
		t.Errorf("KeyVersion: %d", c.KeyVersion())
	}
	plain := []byte("hello")
	ct, err := c.Encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !bytes.Equal(ct, plain) {
		t.Errorf("Noop should round-trip identity")
	}
	got, err := c.Decrypt(ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("decrypt mismatch")
	}
}

func TestNoopCipher_PanicsNonDev(t *testing.T) {
	for _, env := range []string{"staging", "prod"} {
		t.Run(env, func(t *testing.T) {
			c := NewNoopCipher(env)
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("expected panic for env=%s", env)
				}
				if !strings.Contains(r.(string), "NoopCipher") {
					t.Errorf("panic msg: %v", r)
				}
			}()
			_, _ = c.Encrypt([]byte("nope"))
		})
	}
}

func TestNoopCipher_PanicsNonDev_OnDecrypt(t *testing.T) {
	c := NewNoopCipher("staging")
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on Decrypt")
		}
	}()
	_, _ = c.Decrypt([]byte("nope"))
}
