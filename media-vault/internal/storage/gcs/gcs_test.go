package gcs

import (
	"errors"
	"testing"

	"google.golang.org/api/googleapi"

	gcs "cloud.google.com/go/storage"

	"github.com/crashchat-ai/mio/media-vault/internal/storage"
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
