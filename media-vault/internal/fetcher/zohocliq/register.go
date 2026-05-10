package zohocliq

import (
	"net/http"
	"time"

	"github.com/crashchat-ai/mio/media-vault/internal/fetcher"
)

// MustRegister wires this package into the global fetcher registry.
// Caller passes Zoho OAuth credentials + max-bytes from config. The Fetcher
// mints fresh access tokens on demand from the refresh token.
//
// Passing all-empty credentials is allowed: the resulting fetcher attaches
// no Authorization header. Cliq fetches will then 401 — this mode exists
// only for tests / local dev with public URLs.
func MustRegister(clientID, clientSecret, refreshToken string, maxBytes int64, timeout time.Duration) {
	c := &http.Client{Timeout: timeout}
	tokens := newTokenSource(clientID, clientSecret, refreshToken)
	fetcher.Register(New(c, tokens, maxBytes))
}
