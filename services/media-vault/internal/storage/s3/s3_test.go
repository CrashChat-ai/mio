package s3

import (
	"errors"
	"testing"

	"github.com/minio/minio-go/v7"

	"github.com/crashchat-ai/mio/services/media-vault/internal/storage"
)

func TestNewRejectsEmptyBucket(t *testing.T) {
	if _, err := New(t.Context(), &Config{Bucket: ""}); err == nil {
		t.Fatal("expected error on empty bucket")
	}
}

func TestConfigFromEnvRejectsEmptyBucket(t *testing.T) {
	if _, err := ConfigFromEnv(""); err == nil {
		t.Fatal("expected error on empty bucket")
	}
}

func TestConfigFromEnvDefaultSSL(t *testing.T) {
	t.Setenv("MIO_STORAGE_S3_USE_SSL", "")
	cfg, err := ConfigFromEnv("my-bucket")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.UseSSL {
		t.Error("UseSSL should default to true")
	}
}

func TestConfigFromEnvSSLFalse(t *testing.T) {
	t.Setenv("MIO_STORAGE_S3_USE_SSL", "false")
	cfg, err := ConfigFromEnv("my-bucket")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.UseSSL {
		t.Error("UseSSL should be false when set to 'false'")
	}
}

func TestMapErrNoSuchKey(t *testing.T) {
	err := minio.ErrorResponse{Code: "NoSuchKey", Message: "not found"}
	got := mapErr(err)
	if !errors.Is(got, storage.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", got)
	}
}

func TestMapErrNoSuchBucket(t *testing.T) {
	err := minio.ErrorResponse{Code: "NoSuchBucket", Message: "not found"}
	got := mapErr(err)
	if !errors.Is(got, storage.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", got)
	}
}

func TestMapErrPreconditionFailed(t *testing.T) {
	err := minio.ErrorResponse{Code: "PreconditionFailed", StatusCode: 412}
	got := mapErr(err)
	if !errors.Is(got, storage.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", got)
	}
}

func TestMapErrPassesThroughOther(t *testing.T) {
	err := minio.ErrorResponse{Code: "InternalError", StatusCode: 500}
	got := mapErr(err)
	if errors.Is(got, storage.ErrNotFound) || errors.Is(got, storage.ErrAlreadyExists) {
		t.Fatalf("did not expect sentinel for 500: %v", got)
	}
}

func TestMapErrNil(t *testing.T) {
	if mapErr(nil) != nil {
		t.Error("mapErr(nil) should return nil")
	}
}

func TestSanitizeID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"mio/attachments/", "mio-attachments"},
		{"prefix", "prefix"},
		{"/leading/", "leading"},
	}
	for _, tc := range cases {
		if got := sanitizeID(tc.in); got != tc.want {
			t.Errorf("sanitizeID(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestIntoToObjectPopulatesFields(t *testing.T) {
	info := minio.ObjectInfo{
		Key:  "mio/attachments/file.png",
		Size: 42,
		UserMetadata: map[string]string{
			"Sha256":            "abc123",
			"Tenant_id":         "t1",
			"Account_id":        "a1",
			"Conversation_id":   "c1",
			"Source_message_id": "s1",
		},
	}
	obj := infoToObject("mio/attachments/file.png", info)
	if obj.SHA256Hex != "abc123" {
		t.Errorf("SHA256Hex = %q; want abc123", obj.SHA256Hex)
	}
	if obj.TenantID != "t1" {
		t.Errorf("TenantID = %q; want t1", obj.TenantID)
	}
	if obj.AccountID != "a1" {
		t.Errorf("AccountID = %q; want a1", obj.AccountID)
	}
	if obj.ConversationID != "c1" {
		t.Errorf("ConversationID = %q; want c1", obj.ConversationID)
	}
	if obj.SourceMessageID != "s1" {
		t.Errorf("SourceMessageID = %q; want s1", obj.SourceMessageID)
	}
}

func TestBuildUserMetadataFiltersEmpty(t *testing.T) {
	meta := buildUserMetadata(storage.PutOptions{
		SHA256Hex:      "abc",
		TenantID:       "t1",
		AccountID:      "a1",
		ConversationID: "",
		Metadata:       map[string]string{"key": "", "other": "val"},
	})
	if _, ok := meta[""]; ok {
		t.Error("empty key must not be written")
	}
	if v := meta["sha256"]; v != "abc" {
		t.Errorf("sha256 = %q; want abc", v)
	}
	if _, ok := meta["conversation_id"]; ok {
		t.Error("empty conversation_id must not be written")
	}
	if v := meta["other"]; v != "val" {
		t.Errorf("other = %q; want val", v)
	}
}
