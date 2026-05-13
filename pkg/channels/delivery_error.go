package channels

// DeliveryError is the interface adapter errors must satisfy to allow the
// gateway-internal sender pool to route Nak vs Term without importing any
// concrete adapter package. Adapters return a concrete struct that
// implements this interface (e.g. zohocliq.HTTPError).
type DeliveryError interface {
	error
	// IsRetryable returns true for 5xx / transient errors (→ Nak).
	IsRetryable() bool
	// IsRateLimited returns true when the platform returned 429.
	IsRateLimited() bool
	// RetryAfterSeconds returns the Retry-After value in seconds (0 = not present).
	RetryAfterSeconds() int
}
