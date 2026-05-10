package zohocliq

import (
	"net/http"
	"time"

	"github.com/crashchat-ai/mio/attachment-downloader/internal/fetcher"
)

// MustRegister wires this package into the global fetcher registry.
// Caller passes the bot token + max-bytes from config.
func MustRegister(botToken string, maxBytes int64, timeout time.Duration) {
	c := &http.Client{Timeout: timeout}
	fetcher.Register(New(c, botToken, maxBytes))
}
