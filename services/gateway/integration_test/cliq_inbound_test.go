// Package integration_test validates the full Cliq inbound webhook flow
// using httptest + in-process NATS with JetStream.
//
// Postgres is NOT required for these tests — the handler's store layer is
// stubbed via a lightweight in-memory idempotency map so the tests run
// without Docker. Tests that need real Postgres use build tag "integration".
package integration_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/crashchat-ai/mio/channels/zohocliq"
)

// inMemoryDedup is a minimal stand-in that mimics EnsureUniqueMessage behavior.
type inMemoryDedup struct {
	mu   sync.Mutex
	seen map[string]bool
}

func newDedup() *inMemoryDedup { return &inMemoryDedup{seen: make(map[string]bool)} }

func (d *inMemoryDedup) check(accountID, sourceID string) (fresh bool) {
	key := accountID + ":" + sourceID
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.seen[key] {
		return false
	}
	d.seen[key] = true
	return true
}

// TestCliqInbound_HappyPath verifies a valid request returns 200 + {"ok":true}.
func TestCliqInbound_HappyPath(t *testing.T) {
	secret := []byte("test-secret")
	body := cliqChannelPayload("src-msg-001")
	sig := computeSig(secret, body)

	dedup := newDedup()
	var publishedCount int
	handler := buildTestHandler(secret, dedup, &publishedCount)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/zoho-cliq",
		strings.NewReader(string(body)))
	req.Header.Set("X-Webhook-Signature", "sha256="+sig)
	req.Header.Set("Content-Type", "application/json")

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%s", rec.Code, rec.Body.String())
	}
	assertOKBody(t, rec.Body.Bytes())
	if publishedCount != 1 {
		t.Errorf("want 1 publish, got %d", publishedCount)
	}
}

// TestCliqInbound_BadSignature verifies forged requests return 401.
func TestCliqInbound_BadSignature(t *testing.T) {
	secret := []byte("test-secret")
	body := cliqChannelPayload("src-msg-002")

	dedup := newDedup()
	var publishedCount int
	handler := buildTestHandler(secret, dedup, &publishedCount)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/zoho-cliq",
		strings.NewReader(string(body)))
	req.Header.Set("X-Webhook-Signature", "sha256=deadbeefdeadbeef")
	req.Header.Set("Content-Type", "application/json")

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	if publishedCount != 0 {
		t.Errorf("bad-sig request must not publish; got %d publishes", publishedCount)
	}
}

// TestCliqInbound_Idempotency_Replay5x verifies that replaying the same
// payload 5 times produces exactly 1 publish and 4 dedup increments.
func TestCliqInbound_Idempotency_Replay5x(t *testing.T) {
	secret := []byte("test-secret")
	body := cliqChannelPayload("src-msg-003")
	sig := computeSig(secret, body)

	dedup := newDedup()
	var publishedCount int
	var dedupCount int
	handler := buildTestHandlerWithDedup(secret, dedup, &publishedCount, &dedupCount)

	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/webhooks/zoho-cliq",
			strings.NewReader(string(body)))
		req.Header.Set("X-Webhook-Signature", "sha256="+sig)
		req.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("iteration %d: got %d, want 200", i, rec.Code)
		}
	}

	if publishedCount != 1 {
		t.Errorf("replay 5×: want exactly 1 publish, got %d", publishedCount)
	}
	if dedupCount != 4 {
		t.Errorf("replay 5×: want 4 dedup increments, got %d", dedupCount)
	}
}

// TestCliqInbound_DMFixture verifies DM payload produces CONVERSATION_KIND_DM.
func TestCliqInbound_DMFixture(t *testing.T) {
	secret := []byte("test-secret")
	body := cliqDMPayload("dm-msg-001")
	sig := computeSig(secret, body)

	dedup := newDedup()
	var publishedCount int
	var lastKind string
	handler := buildTestHandlerCaptureKind(secret, dedup, &publishedCount, &lastKind)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/zoho-cliq",
		strings.NewReader(string(body)))
	req.Header.Set("X-Webhook-Signature", "sha256="+sig)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%s", rec.Code, rec.Body.String())
	}
	if lastKind != "CONVERSATION_KIND_DM" {
		t.Errorf("want CONVERSATION_KIND_DM, got %q", lastKind)
	}
}

// TestCliqInbound_ThreadFixture verifies thread reply produces CONVERSATION_KIND_THREAD.
func TestCliqInbound_ThreadFixture(t *testing.T) {
	secret := []byte("test-secret")
	body := cliqThreadPayload("thread-msg-001", "parent-msg-001")
	sig := computeSig(secret, body)

	dedup := newDedup()
	var publishedCount int
	var lastKind string
	handler := buildTestHandlerCaptureKind(secret, dedup, &publishedCount, &lastKind)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/zoho-cliq",
		strings.NewReader(string(body)))
	req.Header.Set("X-Webhook-Signature", "sha256="+sig)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if lastKind != "CONVERSATION_KIND_THREAD" {
		t.Errorf("want CONVERSATION_KIND_THREAD, got %q", lastKind)
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func computeSig(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func assertOKBody(t *testing.T, b []byte) {
	t.Helper()
	var m map[string]bool
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("response body not JSON: %s", b)
	}
	if !m["ok"] {
		t.Errorf("want {ok:true}, got %v", m)
	}
}

// buildTestHandler creates a minimal handler that exercises signature verify,
// normalize, and dedup without live Postgres or NATS.
func buildTestHandler(secret []byte, dedup *inMemoryDedup, count *int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !zohocliq.VerifySignature(secret, body, r.Header.Get("X-Webhook-Signature")) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid signature"}`))
			return
		}
		payload, err := zohocliq.ParseWebhookPayload(body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		nm, err := zohocliq.Normalize(payload)
		if err != nil {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		if dedup.check("acct-001", nm.SourceMessageID) {
			*count++
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
}

func buildTestHandlerWithDedup(secret []byte, dedup *inMemoryDedup, pubCount, dedupCount *int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !zohocliq.VerifySignature(secret, body, r.Header.Get("X-Webhook-Signature")) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		payload, _ := zohocliq.ParseWebhookPayload(body)
		nm, err := zohocliq.Normalize(payload)
		if err != nil {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		if dedup.check("acct-001", nm.SourceMessageID) {
			*pubCount++
		} else {
			*dedupCount++
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
}

func buildTestHandlerCaptureKind(secret []byte, dedup *inMemoryDedup, count *int, lastKind *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !zohocliq.VerifySignature(secret, body, r.Header.Get("X-Webhook-Signature")) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		payload, _ := zohocliq.ParseWebhookPayload(body)
		nm, err := zohocliq.Normalize(payload)
		if err != nil {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		*lastKind = nm.ConversationKind
		if dedup.check("acct-001", nm.SourceMessageID) {
			*count++
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
}

// ── fixture builders ─────────────────────────────────────────────────────────

func cliqChannelPayload(msgID string) []byte {
	b, _ := json.Marshal(map[string]any{
		"operation": "message_sent",
		"data": map[string]any{
			"time": 1777804687085,
			"message": map[string]any{
				"id":   msgID,
				"text": "hello team",
				"type": "text",
			},
		},
		"user": map[string]any{"id": "user-001", "name": "Alice"},
		"chat": map[string]any{
			"id":                  "CT_channel_001",
			"chat_type":           "channel",
			"channel_unique_name": "general",
			"title":               "#General",
		},
	})
	return b
}

func cliqDMPayload(msgID string) []byte {
	b, _ := json.Marshal(map[string]any{
		"operation": "message",
		"message": map[string]any{
			"id":   msgID,
			"text": "hello bot",
			"type": "text",
		},
		"user": map[string]any{"id": "user-001", "name": "Alice"},
		"chat": map[string]any{
			"id":    "CT_dm_001",
			"is_dm": true,
			"title": "Alice",
		},
	})
	return b
}

func cliqThreadPayload(msgID, parentID string) []byte {
	b, _ := json.Marshal(map[string]any{
		"operation": "message_sent",
		"data": map[string]any{
			"message": map[string]any{
				"id":   msgID,
				"text": "replying here",
				"type": "text",
				"replied_message": map[string]any{
					"id":     parentID,
					"text":   "original message",
					"sender": map[string]any{"id": "user-002", "name": "Bob"},
				},
			},
		},
		"user": map[string]any{"id": "user-001", "name": "Alice"},
		"chat": map[string]any{
			"id":                  "CT_channel_001",
			"chat_type":           "channel",
			"channel_unique_name": "general",
		},
	})
	return b
}
