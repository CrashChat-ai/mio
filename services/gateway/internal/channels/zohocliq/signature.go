// Package zohocliq implements the Zoho Cliq inbound webhook adapter.
package zohocliq

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// VerifySignature checks the X-Webhook-Signature header against the raw body.
//
// Cliq signs with HMAC-SHA256. The header value is "sha256=<digest>" where
// digest is hex-encoded (confirmed from POC server.py:57 and FINDINGS.md Q6).
// Base64 encoding is also accepted as a fallback (server.py:58 checks both).
//
// Signs the raw body bytes BEFORE any JSON parsing — body must be buffered
// before calling this function.
//
// Returns true when signature is valid. Returns false (not error) when
// the signature is present but wrong — callers emit a metric and 401.
// Returns false when header is empty (missing signature).
func VerifySignature(secret []byte, body []byte, header string) bool {
	if len(secret) == 0 {
		// Dev mode: no secret configured — accept all (logged as warning by caller).
		return true
	}
	if header == "" {
		return false
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	digest := mac.Sum(nil)

	// Strip "sha256=" prefix if present.
	received := header
	if after, ok := strings.CutPrefix(header, "sha256="); ok {
		received = after
	}

	// Accept hex encoding (primary — confirmed from captures).
	expectedHex := hex.EncodeToString(digest)
	if hmac.Equal([]byte(received), []byte(expectedHex)) {
		return true
	}

	// Accept base64 encoding (fallback — Deluge zoho.encryption.hmacsha256 output).
	expectedB64 := base64.StdEncoding.EncodeToString(digest)
	return hmac.Equal([]byte(received), []byte(expectedB64))
}
