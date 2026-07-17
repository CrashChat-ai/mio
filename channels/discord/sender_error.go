package discord

import (
	"errors"
	"net/http"

	"github.com/bwmarrin/discordgo"
)

// DeliveryError adapts a discordgo error to channels.DeliveryError so the
// gateway sender pool can route Nak (retry) vs Term (permanent) vs
// Nak-with-Retry-After (429) without importing discordgo.
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

// classifyDeliveryError maps a discordgo API error onto the Nak/Term taxonomy:
//   - *RateLimitError → 429 (Nak with Retry-After)
//   - *RESTError with a 4xx status → Term (permanent: bad token, missing
//     permissions, unknown channel/message, malformed payload)
//   - everything else (5xx, network, transient) → Nak
func classifyDeliveryError(err error) error {
	if err == nil {
		return nil
	}

	var rle *discordgo.RateLimitError
	if errors.As(err, &rle) {
		secs := int(rle.RetryAfter.Seconds())
		if secs < 1 {
			secs = 1
		}
		return &DeliveryError{err: err, rateLimited: true, retryAfterSecs: secs}
	}

	var rest *discordgo.RESTError
	if errors.As(err, &rest) && rest.Response != nil {
		code := rest.Response.StatusCode
		if code >= 400 && code < 500 && code != http.StatusTooManyRequests {
			return &DeliveryError{err: err, retryable: false}
		}
	}

	return &DeliveryError{err: err, retryable: true}
}
