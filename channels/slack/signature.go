// Package slack implements the Slack inbound adapter (Socket Mode v1, Events
// API webhook additive v2).
package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"
)

// signatureMaxSkew is Slack's documented replay window: reject requests whose
// X-Slack-Request-Timestamp is more than 5 minutes from now.
const signatureMaxSkew = 5 * time.Minute

// verifySignature checks Slack's v0 request signature.
//
// Slack computes HMAC-SHA256 over "v0:{timestamp}:{rawBody}" with the signing
// secret, hex-encodes it, and sends "v0=<hex>" in X-Slack-Signature. The
// timestamp arrives in X-Slack-Request-Timestamp and must be within
// signatureMaxSkew of now (replay protection).
//
// Dormant in v1 (Socket Mode needs no signature) but fully tested so v2 webhook
// mode is a route-mount away. Empty secret = dev-mode bypass (returns true);
// the adapter emits the startup warning, not this function.
func verifySignature(secret []byte, body []byte, sigHeader, tsHeader string, now time.Time) bool {
	if len(secret) == 0 {
		return true
	}
	if sigHeader == "" || tsHeader == "" {
		return false
	}

	ts, err := strconv.ParseInt(tsHeader, 10, 64)
	if err != nil {
		return false
	}
	if delta := now.Sub(time.Unix(ts, 0)); delta > signatureMaxSkew || delta < -signatureMaxSkew {
		return false
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("v0:" + tsHeader + ":"))
	mac.Write(body)
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sigHeader), []byte(expected))
}
