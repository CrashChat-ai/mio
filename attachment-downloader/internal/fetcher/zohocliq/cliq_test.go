package zohocliq

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"

	"github.com/crashchat-ai/mio/attachment-downloader/internal/fetcher"
)

func TestFetchSuccess(t *testing.T) {
	body := []byte("hello world bytes")
	expectedSHA := sha256.Sum256(body)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Zoho-oauthtoken tok" {
			t.Errorf("auth header = %q", got)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	// httptest serves http://; remap to https requirement is too strict for unit
	// tests — relax here by patching the URL parser path. We use a special
	// constructor in tests via direct call:
	f := New(srv.Client(), staticTokenSource("tok"), 1024)
	f.allowInsecureForTest = true

	var buf bytes.Buffer
	res, err := f.Fetch(t.Context(), &miov1.Attachment{Url: srv.URL + "/file"}, &buf)
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

func TestFetchExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"attachment_access_time_expired"}`))
	}))
	defer srv.Close()

	f := New(srv.Client(), staticTokenSource("tok"), 1024)
	f.allowInsecureForTest = true
	_, err := f.Fetch(t.Context(), &miov1.Attachment{Url: srv.URL}, &bytes.Buffer{})
	var fe *fetcher.Error
	if !errors.As(err, &fe) || fe.Code != miov1.Attachment_ERROR_CODE_EXPIRED {
		t.Fatalf("expected ERROR_CODE_EXPIRED, got %v", err)
	}
}

func TestFetchForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	f := New(srv.Client(), staticTokenSource("tok"), 1024)
	f.allowInsecureForTest = true
	_, err := f.Fetch(t.Context(), &miov1.Attachment{Url: srv.URL}, &bytes.Buffer{})
	var fe *fetcher.Error
	if !errors.As(err, &fe) || fe.Code != miov1.Attachment_ERROR_CODE_FORBIDDEN {
		t.Fatalf("expected ERROR_CODE_FORBIDDEN, got %v", err)
	}
}

func TestFetch5xxIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := New(srv.Client(), staticTokenSource("tok"), 1024)
	f.allowInsecureForTest = true
	_, err := f.Fetch(t.Context(), &miov1.Attachment{Url: srv.URL}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error")
	}
	var fe *fetcher.Error
	if errors.As(err, &fe) {
		t.Fatalf("5xx must be plain error (worker Naks), got typed FetchError: %v", err)
	}
}

func TestFetchTooLargeByContentLength(t *testing.T) {
	body := strings.Repeat("x", 100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	f := New(srv.Client(), staticTokenSource("tok"), 50)
	f.allowInsecureForTest = true
	_, err := f.Fetch(t.Context(), &miov1.Attachment{Url: srv.URL}, &bytes.Buffer{})
	var fe *fetcher.Error
	if !errors.As(err, &fe) || fe.Code != miov1.Attachment_ERROR_CODE_TOO_LARGE {
		t.Fatalf("expected TOO_LARGE, got %v", err)
	}
}

func TestFetchEmptyURL(t *testing.T) {
	f := New(http.DefaultClient, staticTokenSource("tok"), 1024)
	_, err := f.Fetch(t.Context(), &miov1.Attachment{}, &bytes.Buffer{})
	var fe *fetcher.Error
	if !errors.As(err, &fe) || fe.Code != miov1.Attachment_ERROR_CODE_NOT_FOUND {
		t.Fatalf("expected NOT_FOUND, got %v", err)
	}
}

func TestChannelType(t *testing.T) {
	if (Fetcher{}).ChannelType() != "zoho_cliq" {
		t.Fatal("wrong channel type slug")
	}
}
