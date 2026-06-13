// Package slack implements fetcher.Fetcher for Slack file attachments.
package slack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"

	"github.com/crashchat-ai/mio/services/media-vault/internal/fetcher"
)

const channelType = "slack"

// downloadAllowlist is goclaw's verbatim SSRF host-suffix allowlist.
var downloadAllowlist = []string{".slack.com", ".slack-edge.com", ".slack-files.com"}

// Fetcher downloads bytes from a Slack url_private_download with a static
// xoxb bot token. Unlike Cliq there is no OAuth refresh — the bot token is
// long-lived, so it is held inline rather than via a token source.
type Fetcher struct {
	httpClient *http.Client
	botToken   string
	maxBytes   int64
	// allowInsecureForTest relaxes the https-only check so httptest servers
	// (which serve http://) can drive unit tests. Never set in production.
	allowInsecureForTest bool
}

// New constructs a Slack fetcher. botToken may be empty — Fetch then sends no
// Authorization header (Slack will return its login page, caught by the
// text/html guard). In production main wires the token from config.
func New(httpClient *http.Client, botToken string, maxBytes int64) *Fetcher {
	f := &Fetcher{botToken: botToken, maxBytes: maxBytes}
	if maxBytes <= 0 {
		f.maxBytes = 25 * 1024 * 1024
	}
	f.httpClient = httpClient
	if f.httpClient == nil {
		f.httpClient = &http.Client{Timeout: 90 * time.Second}
	}
	f.httpClient.CheckRedirect = f.checkRedirect
	return f
}

// ChannelType returns the slug.
func (Fetcher) ChannelType() string { return channelType }

// Fetch streams att.Url to dst with sha-256 + size accounting. Rejects hosts
// outside the Slack allowlist and text/html bodies (login-page corruption).
func (f *Fetcher) Fetch(ctx context.Context, att *miov1.Attachment, dst io.Writer) (fetcher.Result, error) {
	rawURL := att.GetUrl()
	if rawURL == "" {
		return fetcher.Result{}, &fetcher.Error{Code: miov1.Attachment_ERROR_CODE_NOT_FOUND, Msg: "empty url"}
	}
	if !f.allowedHost(rawURL) {
		return fetcher.Result{}, &fetcher.Error{Code: miov1.Attachment_ERROR_CODE_FORBIDDEN, Msg: "host not in slack allowlist"}
	}

	resp, err := f.do(ctx, rawURL)
	if err != nil {
		return fetcher.Result{}, err
	}
	defer resp.Body.Close()

	if resp.ContentLength > 0 && resp.ContentLength > f.maxBytes {
		return fetcher.Result{}, &fetcher.Error{Code: miov1.Attachment_ERROR_CODE_TOO_LARGE,
			Msg: fmt.Sprintf("content-length %d > cap %d", resp.ContentLength, f.maxBytes)}
	}

	if err := classify(resp); err != nil {
		return fetcher.Result{}, err
	}

	h := sha256.New()
	mw := io.MultiWriter(dst, h)
	limited := io.LimitReader(resp.Body, f.maxBytes+1)

	n, err := io.Copy(mw, limited)
	if err != nil {
		return fetcher.Result{}, fmt.Errorf("slack: read body: %w", err)
	}
	if n > f.maxBytes {
		return fetcher.Result{}, &fetcher.Error{Code: miov1.Attachment_ERROR_CODE_TOO_LARGE,
			Msg: fmt.Sprintf("body exceeded cap %d", f.maxBytes)}
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = att.GetMime()
	}

	return fetcher.Result{
		Bytes:       n,
		SHA256Hex:   hex.EncodeToString(h.Sum(nil)),
		ContentType: ct,
	}, nil
}

func (f *Fetcher) do(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("slack: new request: %w", err)
	}
	if f.botToken != "" {
		req.Header.Set("Authorization", "Bearer "+f.botToken)
	}
	req.Header.Set("User-Agent", "mio-media-vault/1.0")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("slack: do: %w", err)
	}
	return resp, nil
}

// checkRedirect strips the bearer token and re-validates the target against
// the allowlist — Slack redirects to presigned CDN URLs that must not receive
// the token, and an open redirect must never reach an attacker host.
func (f *Fetcher) checkRedirect(req *http.Request, via []*http.Request) error {
	req.Header.Del("Authorization")
	if len(via) >= 3 {
		return fmt.Errorf("slack: too many redirects")
	}
	if !f.allowedHost(req.URL.String()) {
		return fmt.Errorf("slack: redirect to untrusted host: %s", req.URL.Host)
	}
	return nil
}

func (f *Fetcher) allowedHost(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme != "https" && !(f.allowInsecureForTest && u.Scheme == "http") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, suffix := range downloadAllowlist {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// classify maps Slack HTTP responses to typed FetchError or nil for 2xx. A
// text/html 200 is Slack's unauthenticated login page — reject it so corrupt
// HTML is never persisted as media bytes.
func classify(resp *http.Response) error {
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		if isHTML(resp.Header.Get("Content-Type")) {
			return &fetcher.Error{Code: miov1.Attachment_ERROR_CODE_FORBIDDEN, Msg: "slack login page (text/html)"}
		}
		return nil
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return &fetcher.Error{Code: miov1.Attachment_ERROR_CODE_FORBIDDEN, Msg: resp.Status}
	case resp.StatusCode == http.StatusNotFound, resp.StatusCode == http.StatusGone:
		return &fetcher.Error{Code: miov1.Attachment_ERROR_CODE_NOT_FOUND, Msg: resp.Status}
	case resp.StatusCode == http.StatusRequestEntityTooLarge:
		return &fetcher.Error{Code: miov1.Attachment_ERROR_CODE_TOO_LARGE, Msg: resp.Status}
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("slack: upstream %s", resp.Status)
	}
	return fmt.Errorf("slack: unexpected %s", resp.Status)
}

func isHTML(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "text/html")
}
