package slack

import (
	"errors"
	"regexp"

	slackapi "github.com/slack-go/slack"
)

// nonRetryableAuthErrors matches Slack error codes that signal a permanent
// failure — no redelivery will fix them (ported from goclaw utils.go, plus
// channel_not_found per brief §6). The pool Terms these.
var nonRetryableAuthErrors = regexp.MustCompile(
	`(?i)(invalid_auth|token_revoked|account_inactive|not_authed|team_not_found|missing_scope|channel_not_found)`,
)

// DeliveryError adapts a slack-go error to channels.DeliveryError so the
// gateway sender pool can route Nak (retry) vs Term (permanent) vs
// Nak-with-Retry-After (429) without importing slack-go.
type DeliveryError struct {
	err            error
	retryable      bool
	rateLimited    bool
	retryAfterSecs int
}

func (e *DeliveryError) Error() string          { return e.err.Error() }
func (e *DeliveryError) Unwrap() error          { return e.err }
func (e *DeliveryError) IsRetryable() bool      { return e.retryable }
func (e *DeliveryError) IsRateLimited() bool    { return e.rateLimited }
func (e *DeliveryError) RetryAfterSeconds() int { return e.retryAfterSecs }

// classifyDeliveryError maps a slack-go API error onto the Nak/Term taxonomy:
//   - *RateLimitedError → 429 (Nak with Retry-After)
//   - permanent auth/scope/channel error string → Term (not retryable)
//   - everything else (5xx, network, transient) → Nak
func classifyDeliveryError(err error) error {
	if err == nil {
		return nil
	}

	var rle *slackapi.RateLimitedError
	if errors.As(err, &rle) {
		secs := int(rle.RetryAfter.Seconds())
		if secs < 1 {
			secs = 1
		}
		return &DeliveryError{err: err, rateLimited: true, retryAfterSecs: secs}
	}

	if nonRetryableAuthErrors.MatchString(err.Error()) {
		return &DeliveryError{err: err, retryable: false}
	}

	return &DeliveryError{err: err, retryable: true}
}
