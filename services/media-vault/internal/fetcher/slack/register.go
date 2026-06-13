package slack

import (
	"net/http"
	"time"

	"github.com/crashchat-ai/mio/services/media-vault/internal/fetcher"
)

// MustRegister wires this package into the global fetcher registry. botToken is
// the static xoxb credential; an empty token yields a fetcher that sends no
// Authorization header (Slack then returns its login page, caught by the
// text/html guard) — useful only for tests / local dev with public URLs.
func MustRegister(botToken string, maxBytes int64, timeout time.Duration) {
	c := &http.Client{Timeout: timeout}
	fetcher.Register(New(c, botToken, maxBytes))
}
