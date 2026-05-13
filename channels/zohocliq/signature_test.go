package zohocliq

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestVerifySignature_ValidHex(t *testing.T) {
	secret := []byte("test-secret")
	body := []byte(`{"operation":"message_sent"}`)

	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	digest := mac.Sum(nil)
	header := "sha256=" + hex.EncodeToString(digest)

	if !VerifySignature(secret, body, header) {
		t.Fatal("expected valid hex signature to pass")
	}
}

func TestVerifySignature_ValidBase64(t *testing.T) {
	secret := []byte("test-secret")
	body := []byte(`{"operation":"message_sent"}`)

	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	digest := mac.Sum(nil)
	header := "sha256=" + base64.StdEncoding.EncodeToString(digest)

	if !VerifySignature(secret, body, header) {
		t.Fatal("expected valid base64 signature to pass")
	}
}

func TestVerifySignature_InvalidSignature(t *testing.T) {
	secret := []byte("test-secret")
	body := []byte(`{"operation":"message_sent"}`)
	header := "sha256=deadbeefdeadbeef"

	if VerifySignature(secret, body, header) {
		t.Fatal("expected forged signature to fail")
	}
}

func TestVerifySignature_EmptyHeader(t *testing.T) {
	secret := []byte("test-secret")
	body := []byte(`{}`)

	if VerifySignature(secret, body, "") {
		t.Fatal("expected empty header to fail")
	}
}

func TestVerifySignature_EmptySecret_DevMode(t *testing.T) {
	// No secret = dev mode; accepts any request.
	if !VerifySignature(nil, []byte(`{}`), "") {
		t.Fatal("expected dev mode (no secret) to accept all requests")
	}
}

func TestVerifySignature_NoPrefixHex(t *testing.T) {
	// Some callers omit the "sha256=" prefix.
	secret := []byte("test-secret")
	body := []byte(`{"op":"test"}`)

	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	digest := mac.Sum(nil)
	header := hex.EncodeToString(digest) // no prefix

	if !VerifySignature(secret, body, header) {
		t.Fatal("expected no-prefix hex to pass")
	}
}

func TestVerifySignature_WrongBody(t *testing.T) {
	secret := []byte("test-secret")
	body := []byte(`{"operation":"message_sent"}`)
	tamperedBody := []byte(`{"operation":"message_sent","injected":true}`)

	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	digest := mac.Sum(nil)
	header := "sha256=" + hex.EncodeToString(digest)

	// Verify against tampered body — must fail.
	if VerifySignature(secret, tamperedBody, header) {
		t.Fatal("expected tampered body to fail signature check")
	}
}
