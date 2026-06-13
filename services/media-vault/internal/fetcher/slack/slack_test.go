package slack

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"

	"github.com/crashchat-ai/mio/services/media-vault/internal/fetcher"
)

func newTestFetcher(client *http.Client, maxBytes int64) *Fetcher {
	f := New(client, "xoxb-test", maxBytes)
	f.allowInsecureForTest = true
	return f
}

func TestChannelType(t *testing.T) {
	if (Fetcher{}).ChannelType() != "slack" {
		t.Fatal("wrong channel type slug")
	}
}

func TestFetchSuccessSendsBearer(t *testing.T) {
	body := []byte("fake png bytes")
	expectedSHA := sha256.Sum256(body)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer xoxb-test" {
			t.Errorf("auth header = %q, want Bearer xoxb-test", got)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	// httptest serves a 127.0.0.1 host; allowlist would reject it, so rewrite
	// the request to a slack-suffixed host pointed at the test server.
	client := srv.Client()
	client.Transport = rewriteTransport{client.Transport, srv.URL}
	f := newTestFetcher(client, 1024)

	var buf bytes.Buffer
	res, err := f.Fetch(t.Context(), &miov1.Attachment{Url: "http://files.slack.com/x"}, &buf)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if res.Bytes != int64(len(body)) {
		t.Errorf("bytes = %d, want %d", res.Bytes, len(body))
	}
	if res.SHA256Hex != hex.EncodeToString(expectedSHA[:]) {
		t.Errorf("sha mismatch")
	}
	if res.ContentType != "image/png" {
		t.Errorf("content-type = %q", res.ContentType)
	}
	if !bytes.Equal(buf.Bytes(), body) {
		t.Errorf("body mismatch")
	}
}

func TestAllowedHost(t *testing.T) {
	f := &Fetcher{}
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"files.slack.com", "https://files.slack.com/x", true},
		{"slack-edge cdn", "https://a.b.slack-edge.com/x", true},
		{"slack-files.com", "https://files.slack-files.com/x", true},
		{"case-insensitive", "https://FILES.SLACK.COM/x", true},
		{"with port", "https://files.slack.com:443/x", true},
		{"http blocked", "http://files.slack.com/x", false},
		{"evil host", "https://evil.com/x", false},
		{"slack.org not allowed", "https://files.slack.org/x", false},
		{"suffix spoof slack.com.evil.com", "https://slack.com.evil.com/x", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.allowedHost(tt.url); got != tt.want {
				t.Errorf("allowedHost(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestFetchRejectsNonAllowlistedHost(t *testing.T) {
	f := New(http.DefaultClient, "xoxb-test", 1024)
	_, err := f.Fetch(t.Context(), &miov1.Attachment{Url: "https://evil.com/file"}, &bytes.Buffer{})
	var fe *fetcher.Error
	if !errors.As(err, &fe) || fe.Code != miov1.Attachment_ERROR_CODE_FORBIDDEN {
		t.Fatalf("expected FORBIDDEN for non-allowlisted host, got %v", err)
	}
}

func TestFetchRejectsHTMLLoginPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html>sign in to slack</html>"))
	}))
	defer srv.Close()
	client := srv.Client()
	client.Transport = rewriteTransport{client.Transport, srv.URL}

	f := newTestFetcher(client, 1024)
	var buf bytes.Buffer
	_, err := f.Fetch(t.Context(), &miov1.Attachment{Url: "http://files.slack.com/x"}, &buf)
	var fe *fetcher.Error
	if !errors.As(err, &fe) || fe.Code != miov1.Attachment_ERROR_CODE_FORBIDDEN {
		t.Fatalf("expected FORBIDDEN for text/html, got %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("html body must not be written to dst, got %d bytes", buf.Len())
	}
}

func TestRedirectStripsAuthAndRevalidates(t *testing.T) {
	var got2ndAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/cdn") {
			got2ndAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("img"))
			return
		}
		// First hop redirects to a *different* slack-suffixed CDN host. The
		// transport rewrites slack hosts back to this test server.
		http.Redirect(w, r, "http://cdn.slack-edge.com/cdn", http.StatusFound)
	}))
	defer srv.Close()

	client := &http.Client{Transport: rewriteTransport{http.DefaultTransport, srv.URL}}
	f := newTestFetcher(client, 1024)

	var buf bytes.Buffer
	_, err := f.Fetch(t.Context(), &miov1.Attachment{Url: "http://files.slack.com/orig"}, &buf)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got2ndAuth != "" {
		t.Fatalf("Authorization must be stripped on redirect, got %q", got2ndAuth)
	}
}

func TestRedirectToUntrustedHostBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://evil.com/steal", http.StatusFound)
	}))
	defer srv.Close()

	client := &http.Client{Transport: rewriteTransport{http.DefaultTransport, srv.URL}}
	f := newTestFetcher(client, 1024)

	_, err := f.Fetch(t.Context(), &miov1.Attachment{Url: "http://files.slack.com/orig"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected redirect-to-untrusted-host error")
	}
	if !strings.Contains(err.Error(), "untrusted") {
		t.Fatalf("expected untrusted-host error, got %v", err)
	}
}

func TestFetchTooLargeByContentLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write(bytes.Repeat([]byte("x"), 100))
	}))
	defer srv.Close()
	client := srv.Client()
	client.Transport = rewriteTransport{client.Transport, srv.URL}

	f := newTestFetcher(client, 50)
	_, err := f.Fetch(t.Context(), &miov1.Attachment{Url: "http://files.slack.com/x"}, &bytes.Buffer{})
	var fe *fetcher.Error
	if !errors.As(err, &fe) || fe.Code != miov1.Attachment_ERROR_CODE_TOO_LARGE {
		t.Fatalf("expected TOO_LARGE, got %v", err)
	}
}

func TestFetchTooLargeByBodyOverflow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// no Content-Length: forces the LimitReader+1 overflow path.
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(bytes.Repeat([]byte("x"), 100))
	}))
	defer srv.Close()
	client := srv.Client()
	client.Transport = rewriteTransport{client.Transport, srv.URL}

	f := newTestFetcher(client, 50)
	_, err := f.Fetch(t.Context(), &miov1.Attachment{Url: "http://files.slack.com/x"}, &bytes.Buffer{})
	var fe *fetcher.Error
	if !errors.As(err, &fe) || fe.Code != miov1.Attachment_ERROR_CODE_TOO_LARGE {
		t.Fatalf("expected TOO_LARGE on body overflow, got %v", err)
	}
}

func TestFetchEmptyURL(t *testing.T) {
	f := New(http.DefaultClient, "xoxb-test", 1024)
	_, err := f.Fetch(t.Context(), &miov1.Attachment{}, &bytes.Buffer{})
	var fe *fetcher.Error
	if !errors.As(err, &fe) || fe.Code != miov1.Attachment_ERROR_CODE_NOT_FOUND {
		t.Fatalf("expected NOT_FOUND, got %v", err)
	}
}

func TestClassifyErrorMap(t *testing.T) {
	tests := []struct {
		status int
		want   miov1.Attachment_ErrorCode
		nilErr bool
	}{
		{http.StatusUnauthorized, miov1.Attachment_ERROR_CODE_FORBIDDEN, false},
		{http.StatusForbidden, miov1.Attachment_ERROR_CODE_FORBIDDEN, false},
		{http.StatusNotFound, miov1.Attachment_ERROR_CODE_NOT_FOUND, false},
		{http.StatusGone, miov1.Attachment_ERROR_CODE_NOT_FOUND, false},
		{http.StatusRequestEntityTooLarge, miov1.Attachment_ERROR_CODE_TOO_LARGE, false},
	}
	for _, tt := range tests {
		resp := &http.Response{StatusCode: tt.status, Header: http.Header{}}
		err := classify(resp)
		var fe *fetcher.Error
		if !errors.As(err, &fe) || fe.Code != tt.want {
			t.Errorf("classify(%d) = %v, want code %v", tt.status, err, tt.want)
		}
	}
}

func TestFetch5xxIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	client := srv.Client()
	client.Transport = rewriteTransport{client.Transport, srv.URL}

	f := newTestFetcher(client, 1024)
	_, err := f.Fetch(t.Context(), &miov1.Attachment{Url: "http://files.slack.com/x"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error")
	}
	var fe *fetcher.Error
	if errors.As(err, &fe) {
		t.Fatalf("5xx must be plain error (worker Naks), got typed FetchError: %v", err)
	}
}

// rewriteTransport redirects every request to target, preserving the path so
// the allowlist sees a slack host while the bytes come from httptest.
type rewriteTransport struct {
	inner  http.RoundTripper
	target string
}

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tu, _ := url.Parse(rt.target)
	req.URL.Scheme = tu.Scheme
	req.URL.Host = tu.Host
	inner := rt.inner
	if inner == nil {
		inner = http.DefaultTransport
	}
	return inner.RoundTrip(req)
}
