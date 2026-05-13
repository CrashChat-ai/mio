// Package zohocliq implements fetcher.Fetcher for Zoho Cliq attachments.
package zohocliq

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

const channelType = "zoho_cliq"

// Fetcher downloads attachment bytes from Cliq using a Zoho OAuth access
// token minted on demand from a long-lived refresh token (matches the
// gateway's auth flow).
type Fetcher struct {
	httpClient *http.Client
	tokens     *tokenSource
	maxBytes   int64
	// allowInsecureForTest relaxes the https-only check so httptest servers
	// (which serve http://) can drive unit tests. Never set in production.
	allowInsecureForTest bool
}

// New constructs a Cliq fetcher. tokens may be nil — in which case Fetch
// sends no Authorization header (useful for tests with public URLs). In
// production, tokens MUST be non-nil; main wires it up from config.
func New(httpClient *http.Client, tokens *tokenSource, maxBytes int64) *Fetcher {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 90 * time.Second}
	}
	if maxBytes <= 0 {
		maxBytes = 25 * 1024 * 1024
	}
	return &Fetcher{httpClient: httpClient, tokens: tokens, maxBytes: maxBytes}
}

// ChannelType returns the slug.
func (Fetcher) ChannelType() string { return channelType }

// Fetch streams att.Url to dst with sha-256 + size accounting.
func (f *Fetcher) Fetch(ctx context.Context, att *miov1.Attachment, dst io.Writer) (fetcher.Result, error) {
	rawURL := att.GetUrl()
	if rawURL == "" {
		return fetcher.Result{}, &fetcher.Error{Code: miov1.Attachment_ERROR_CODE_NOT_FOUND, Msg: "empty url"}
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fetcher.Result{}, &fetcher.Error{Code: miov1.Attachment_ERROR_CODE_NOT_FOUND, Msg: "bad url"}
	}
	if u.Scheme != "https" && !(f.allowInsecureForTest && u.Scheme == "http") {
		return fetcher.Result{}, &fetcher.Error{Code: miov1.Attachment_ERROR_CODE_NOT_FOUND, Msg: "non-https url"}
	}

	resp, err := f.do(ctx, rawURL)
	if err != nil {
		return fetcher.Result{}, err
	}
	// Self-heal once on 401: Zoho occasionally rotates a token early. Drop
	// the cache and retry with a freshly-minted access token.
	if resp.StatusCode == http.StatusUnauthorized && f.tokens != nil {
		_ = resp.Body.Close()
		f.tokens.Invalidate()
		resp, err = f.do(ctx, rawURL)
		if err != nil {
			return fetcher.Result{}, err
		}
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
		return fetcher.Result{}, fmt.Errorf("zohocliq: read body: %w", err)
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

// do issues a single GET against rawURL, attaching a freshly-minted Zoho
// access token (if a tokenSource is configured). OAuth refresh failures
// surface as a wrapped *refreshError so callers can distinguish auth-flow
// breakage from Cliq REST errors.
func (f *Fetcher) do(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("zohocliq: new request: %w", err)
	}
	if f.tokens != nil {
		tok, terr := f.tokens.Get(ctx)
		if terr != nil {
			return nil, fmt.Errorf("zohocliq: %w", terr)
		}
		req.Header.Set("Authorization", "Zoho-oauthtoken "+tok)
	}
	req.Header.Set("User-Agent", "mio-media-vault/1.0")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zohocliq: do: %w", err)
	}
	return resp, nil
}

// classify maps Cliq HTTP responses to typed FetchError or nil for 2xx.
func classify(resp *http.Response) error {
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return &fetcher.Error{Code: miov1.Attachment_ERROR_CODE_FORBIDDEN, Msg: resp.Status}
	case resp.StatusCode == http.StatusNotFound:
		return &fetcher.Error{Code: miov1.Attachment_ERROR_CODE_NOT_FOUND, Msg: resp.Status}
	case resp.StatusCode == http.StatusRequestEntityTooLarge:
		return &fetcher.Error{Code: miov1.Attachment_ERROR_CODE_TOO_LARGE, Msg: resp.Status}
	case resp.StatusCode == http.StatusBadRequest:
		// Cliq's "URL TTL elapsed" surfaces as a 400 with this token in the body.
		body, _ := readPeek(resp.Body, 512)
		if strings.Contains(string(body), "attachment_access_time_expired") {
			return &fetcher.Error{Code: miov1.Attachment_ERROR_CODE_EXPIRED, Msg: "url ttl elapsed"}
		}
		return &fetcher.Error{Code: miov1.Attachment_ERROR_CODE_NOT_FOUND, Msg: "400: " + string(body)}
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("zohocliq: upstream %s", resp.Status)
	}
	return fmt.Errorf("zohocliq: unexpected %s", resp.Status)
}

func readPeek(r io.Reader, n int) ([]byte, error) {
	buf := make([]byte, n)
	read, err := io.ReadFull(r, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return buf[:read], err
	}
	return buf[:read], nil
}
