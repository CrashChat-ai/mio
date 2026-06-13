package slack

import (
	"errors"
	"testing"
	"time"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	slackapi "github.com/slack-go/slack"
)

func TestClassifyDeliveryError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		rateLimited bool
		retryable   bool
		retryAfter  int
	}{
		{"rate limited", &slackapi.RateLimitedError{RetryAfter: 5 * time.Second}, true, false, 5},
		{"rate limited rounds up sub-second", &slackapi.RateLimitedError{RetryAfter: 200 * time.Millisecond}, true, false, 1},
		{"invalid_auth term", errors.New("invalid_auth"), false, false, 0},
		{"token_revoked term", slackapi.SlackErrorResponse{Err: "token_revoked"}, false, false, 0},
		{"missing_scope term", errors.New("missing_scope"), false, false, 0},
		{"channel_not_found term", errors.New("channel_not_found"), false, false, 0},
		{"5xx retryable", slackapi.StatusCodeError{Code: 503, Status: "503"}, false, true, 0},
		{"unknown transient retryable", errors.New("ratelimited_unexpected_blip"), false, true, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := classifyDeliveryError(tc.err)
			de, ok := out.(channels.DeliveryError)
			if !ok {
				t.Fatalf("classify returned %T, not channels.DeliveryError", out)
			}
			if de.IsRateLimited() != tc.rateLimited {
				t.Errorf("IsRateLimited = %v, want %v", de.IsRateLimited(), tc.rateLimited)
			}
			if de.IsRetryable() != tc.retryable {
				t.Errorf("IsRetryable = %v, want %v", de.IsRetryable(), tc.retryable)
			}
			if de.RetryAfterSeconds() != tc.retryAfter {
				t.Errorf("RetryAfterSeconds = %d, want %d", de.RetryAfterSeconds(), tc.retryAfter)
			}
		})
	}
}

func TestClassifyDeliveryError_Nil(t *testing.T) {
	if classifyDeliveryError(nil) != nil {
		t.Error("nil error must classify to nil")
	}
}

func TestRateLimitKey(t *testing.T) {
	a := newTestAdapter()
	cmd := &miov1.SendCommand{AccountId: "acct-1", ConversationExternalId: "C123"}
	if got := a.RateLimitKey(cmd); got != "acct-1:C123" {
		t.Errorf("RateLimitKey = %q, want acct-1:C123", got)
	}
	if got := a.RateLimitKey(&miov1.SendCommand{AccountId: "acct-1"}); got != "" {
		t.Errorf("empty conversation must yield empty key, got %q", got)
	}
}
