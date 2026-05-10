package keygen

import (
	"strings"
	"testing"
	"time"
)

func TestBuildShape(t *testing.T) {
	at := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	sha := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	got := Build("mio/attachments/", "zoho_cliq", sha, "image/png", "x.png", at)
	want := "mio/attachments/zoho_cliq/yyyy=2026/mm=05/dd=09/ab/" + sha + ".png"
	if got != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildAddsTrailingSlashToPrefix(t *testing.T) {
	got := Build("mio/attachments", "x", "00", "", "", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if !strings.HasPrefix(got, "mio/attachments/") {
		t.Errorf("missing trailing slash: %s", got)
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	at := time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC)
	a := Build("p/", "ch", "deadbeef", "image/jpeg", "", at)
	b := Build("p/", "ch", "deadbeef", "image/jpeg", "", at)
	if a != b {
		t.Errorf("non-deterministic: %s vs %s", a, b)
	}
}

func TestBuildContentTypeWinsOverFilename(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got := Build("p/", "ch", "00", "image/png", "weird.bin", at)
	if !strings.HasSuffix(got, ".png") {
		t.Errorf("expected png-derived ext, got %s", got)
	}
}

func TestBuildFallsBackToFilenameExt(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got := Build("p/", "ch", "00", "", "image.heic", at)
	if !strings.HasSuffix(got, ".heic") {
		t.Errorf("expected .heic ext, got %s", got)
	}
}

func TestBuildEmptyExtWhenUnknown(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got := Build("p/", "ch", "00", "", "no-ext", at)
	if !strings.HasSuffix(got, "/00") {
		t.Errorf("expected no extension, got %s", got)
	}
}

func TestBuildHandlesShortSha(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got := Build("p/", "ch", "x", "", "", at)
	if !strings.Contains(got, "/x/x") {
		t.Errorf("short sha branch should still produce a key: %s", got)
	}
}
