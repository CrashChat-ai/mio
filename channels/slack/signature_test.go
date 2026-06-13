package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"
)

func slackSign(secret []byte, ts string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("v0:" + ts + ":"))
	mac.Write(body)
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	secret := []byte("8f742231b10e8888abcd99yyyzzz85a5")
	body := []byte(`{"type":"event_callback"}`)
	now := time.Unix(1700000000, 0)
	ts := strconv.FormatInt(now.Unix(), 10)
	good := slackSign(secret, ts, body)

	cases := []struct {
		name string
		sig  string
		ts   string
		want bool
	}{
		{"valid", good, ts, true},
		{"wrong signature", "v0=deadbeef", ts, false},
		{"missing signature", "", ts, false},
		{"missing timestamp", good, "", false},
		{"non-numeric timestamp", good, "not-a-number", false},
		{"stale timestamp", slackSign(secret, "1699999000", body), "1699999000", false},
		{"future timestamp", slackSign(secret, "1700001000", body), "1700001000", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := verifySignature(secret, body, tc.sig, tc.ts, now); got != tc.want {
				t.Errorf("verifySignature = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestVerifySignatureDevModeBypass(t *testing.T) {
	if !verifySignature(nil, []byte(`{}`), "", "", time.Now()) {
		t.Error("empty secret must dev-mode bypass (return true)")
	}
}
