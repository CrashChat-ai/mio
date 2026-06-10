package gcs

import (
	"errors"
	"testing"
	"time"

	"google.golang.org/api/googleapi"

	gcs "cloud.google.com/go/storage"

	"github.com/crashchat-ai/mio/services/media-vault/internal/storage"
)

func TestNewRejectsEmptyBucket(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/dev/null")
	if _, err := New(t.Context(), ""); err == nil {
		t.Fatal("expected error on empty bucket")
	}
}

func TestMapErrTranslatesObjectNotExist(t *testing.T) {
	if got := mapErr(gcs.ErrObjectNotExist); !errors.Is(got, storage.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", got)
	}
}

func TestMapErrTranslatesPreconditionFailedToAlreadyExists(t *testing.T) {
	ge := &googleapi.Error{Code: 412, Message: "precondition failed"}
	if got := mapErr(ge); !errors.Is(got, storage.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", got)
	}
}

func TestMapErrTranslates404ToNotFound(t *testing.T) {
	ge := &googleapi.Error{Code: 404, Message: "not found"}
	if got := mapErr(ge); !errors.Is(got, storage.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", got)
	}
}

func TestMapErrPassesThroughOther(t *testing.T) {
	ge := &googleapi.Error{Code: 500, Message: "boom"}
	if got := mapErr(ge); errors.Is(got, storage.ErrNotFound) || errors.Is(got, storage.ErrAlreadyExists) {
		t.Fatalf("did not expect sentinel for 500: %v", got)
	}
}

func TestLifecycleEqualReflexive(t *testing.T) {
	rules := []gcs.LifecycleRule{
		{
			Action:    gcs.LifecycleAction{Type: gcs.DeleteAction},
			Condition: gcs.LifecycleCondition{AgeInDays: 7, MatchesPrefix: []string{"mio/attachments/"}},
		},
	}
	if !lifecycleEqual(rules, rules) {
		t.Fatal("expected equal")
	}
}

func TestLifecycleEqualDetectsDiff(t *testing.T) {
	a := []gcs.LifecycleRule{{
		Action:    gcs.LifecycleAction{Type: gcs.DeleteAction},
		Condition: gcs.LifecycleCondition{AgeInDays: 7, MatchesPrefix: []string{"a/"}},
	}}
	b := []gcs.LifecycleRule{{
		Action:    gcs.LifecycleAction{Type: gcs.DeleteAction},
		Condition: gcs.LifecycleCondition{AgeInDays: 14, MatchesPrefix: []string{"a/"}},
	}}
	if lifecycleEqual(a, b) {
		t.Fatal("expected non-equal AgeInDays")
	}
}

func TestAttrsToObjectPreservesCorrelationMetadata(t *testing.T) {
	attrs := &gcs.ObjectAttrs{
		Name:        "mio/attachments/object.png",
		Size:        123,
		ContentType: "image/png",
		Updated:     time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC),
		Metadata: map[string]string{
			storage.MetadataContentSHA256:          "abc123",
			storage.MetadataTenantID:               "tenant-1",
			storage.MetadataAccountID:              "account-1",
			storage.MetadataConversationID:         "conversation-1",
			storage.MetadataSourceMessageID:        "source-1",
			storage.MetadataMessageID:              "message-1",
			storage.MetadataAttachmentIndex:        "0",
			storage.MetadataStorageKey:             "gs://bucket/mio/attachments/object.png",
			storage.MetadataConversationExternalID: "conversation-ext-1",
		},
	}

	object := attrsToObject(attrs.Name, attrs)

	if object.SHA256Hex != "abc123" {
		t.Fatalf("SHA256Hex = %q; want abc123", object.SHA256Hex)
	}
	if object.TenantID != "tenant-1" || object.AccountID != "account-1" ||
		object.ConversationID != "conversation-1" || object.SourceMessageID != "source-1" {
		t.Fatalf("typed owner metadata not populated: %+v", object)
	}
	if got := object.Metadata[storage.MetadataStorageKey]; got != "gs://bucket/mio/attachments/object.png" {
		t.Fatalf("metadata storage_key = %q", got)
	}
}
