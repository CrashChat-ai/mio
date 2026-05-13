// Package zohocliq implements the inbound webhook handler and the outbound
// sender adapter for Zoho Cliq. The channels.Adapter interface is satisfied by
// Adapter; its init() in init.go self-registers with the channels registry so
// main.go only needs a blank import.
package zohocliq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	cliqChannelType = "zoho_cliq"
	cliqMaxDeliver  = 5

	// Cliq REST base URL — override via CLIQ_API_BASE_URL in tests.
	defaultCliqBaseURL = "https://cliq.zoho.com"
)

// cliqSendSelfHealedTotal counts Cliq REST calls that succeeded only after
// the self-heal path invalidated a stale-cached token and refreshed.
//
// outcome: "recovered" — second attempt with a fresh token returned 2xx
//
//	"exhausted" — second attempt also returned 401 (truly bad creds)
var cliqSendSelfHealedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "mio_gateway_cliq_send_self_healed_total",
	Help: "Cliq REST calls that hit a stale-token 401 and re-attempted with a fresh token.",
}, []string{"outcome"})

// Adapter implements channels.Adapter for Zoho Cliq.
// Constructed in init.go; all fields except tokens are read-only after construction.
type Adapter struct {
	baseURL    string
	botName    string // bot unique_name (required for channelsbyname endpoint)
	tokens     *tokenSource
	httpClient *http.Client
	logger     *slog.Logger
}

// NewAdapter builds an Adapter from environment variables.
// Called from init.go — panics are acceptable on partial config (startup failure
// signals broken deploy explicitly, instead of waiting for the first 401).
//
// Required for production:
//   - CLIQ_BOT_NAME: bot unique name (channelsbyname endpoint)
//   - CLIQ_CLIENT_ID, CLIQ_CLIENT_SECRET, CLIQ_REFRESH_TOKEN: Zoho OAuth creds
//
// Optional:
//   - CLIQ_API_BASE_URL: override Cliq base URL for tests
//
// If ALL three OAuth vars are absent, tokens remains nil — Send/Edit will
// return an explicit error. This keeps test imports of the package working
// without env wiring. If SOME are set but not all, panics — broken deploy.
func NewAdapter() *Adapter {
	clientID := os.Getenv("CLIQ_CLIENT_ID")
	clientSecret := os.Getenv("CLIQ_CLIENT_SECRET")
	refreshToken := os.Getenv("CLIQ_REFRESH_TOKEN")
	botName := os.Getenv("CLIQ_BOT_NAME")

	// Partial-config detection: any one set means the operator intended OAuth
	// but mis-typed the secret keys. Fail fast with a clear message.
	setCount := 0
	for _, v := range []string{clientID, clientSecret, refreshToken} {
		if v != "" {
			setCount++
		}
	}
	if setCount != 0 && setCount != 3 {
		panic(fmt.Sprintf("zohocliq: partial OAuth config — CLIQ_CLIENT_ID/CLIQ_CLIENT_SECRET/CLIQ_REFRESH_TOKEN must all be set or all empty (got %d/3)", setCount))
	}

	tokens := newTokenSource(clientID, clientSecret, refreshToken)

	baseURL := os.Getenv("CLIQ_API_BASE_URL")
	if baseURL == "" {
		baseURL = defaultCliqBaseURL
	}
	return &Adapter{
		baseURL: baseURL,
		botName: botName,
		tokens:  tokens,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: slog.Default(),
	}
}

// ChannelType returns the registry slug for this adapter.
func (a *Adapter) ChannelType() string { return cliqChannelType }

// MaxDeliver returns the max redelivery count for Cliq messages.
func (a *Adapter) MaxDeliver() int { return cliqMaxDeliver }

// RateLimitKey returns empty string — pool defaults to account_id.
// Cliq does not impose per-conversation limits in documented rate limits.
func (a *Adapter) RateLimitKey(_ *miov1.SendCommand) string { return "" }

// cliqSendRequest is the request body for POST /api/v2/chats/{chatid}/messages.
type cliqSendRequest struct {
	Text string `json:"text"`
}

// Send delivers a new outbound message to Cliq.
// Uses the bot endpoint POST /api/v2/channelsbyname/{name}/message?bot_unique_name={bot}
// (the /chats/{id}/messages endpoint is read-only / DM-only and rejects bot posts
// with request_method_invalid). Channel name must come from cmd.attributes
// "cliq_channel_name" (echo / MIU sets this from msg.conversation_display_name).
//
// Returns: best-effort message id. Cliq returns 204 No Content on success, so
// we synthesise from cmd.id — edit/replace flows that rely on the returned id
// will need an explicit lookup once Cliq exposes a write endpoint that returns
// the message id.
func (a *Adapter) Send(ctx context.Context, cmd *miov1.SendCommand) (string, error) {
	channelName := ""
	if attrs := cmd.GetAttributes(); attrs != nil {
		channelName = attrs["cliq_channel_name"]
	}
	if channelName == "" {
		return "", fmt.Errorf("cliq send: attributes.cliq_channel_name is required")
	}
	if a.botName == "" {
		return "", fmt.Errorf("cliq send: CLIQ_BOT_NAME env unset")
	}

	body := cliqSendRequest{Text: cmd.GetText()}
	reqBody, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("cliq send: marshal request: %w", err)
	}

	// Escape user-controlled segments — channel names with special chars
	// would otherwise corrupt the path. botName is operator-set so safe,
	// but escape for symmetry / future-proofing against rename.
	endpoint := fmt.Sprintf("%s/api/v2/channelsbyname/%s/message?bot_unique_name=%s",
		a.baseURL, url.PathEscape(channelName), url.QueryEscape(a.botName))

	resp, err := a.doWithSelfHeal(ctx, http.MethodPost, endpoint, reqBody)
	if err != nil {
		return "", err
	}

	// Bot endpoint returns 204 No Content on success — no message id available.
	// Fall back to cmd.id so callers have something to log; edit-flow callers
	// must look up the actual id separately (out of POC scope).
	syntheticID := cmd.GetId()
	a.logger.Info("cliq: sent outbound message",
		"cmd_id", cmd.GetId(),
		"channel_name", channelName,
		"http_status", resp.StatusCode,
		"synthetic_id", syntheticID,
	)
	return syntheticID, nil
}

// doWithSelfHeal performs a single Cliq REST call with one-shot 401 recovery.
//
// Algorithm:
//  1. Get a token (cached or fresh from OAuth refresh)
//  2. Build the request, send it
//  3. On 2xx: return success
//  4. On 401, FIRST attempt only: invalidate the cached token, loop to step 1
//     (a freshly-rotated Zoho token will be minted; this masks "Zoho rotated
//     my access token earlier than expires_in promised" races)
//  5. On 401 SECOND attempt: surface the typed HTTPError → pool Terms with
//     reason="auth" (genuine credential failure, manual rotation needed)
//  6. Any non-401 error: return immediately (no point retrying with new token)
//
// Loop is bounded at 2 iterations.
func (a *Adapter) doWithSelfHeal(ctx context.Context, method, url string, body []byte) (*http.Response, error) {
	if a.tokens == nil {
		return nil, fmt.Errorf("cliq: tokens not configured — set CLIQ_CLIENT_ID/CLIQ_CLIENT_SECRET/CLIQ_REFRESH_TOKEN")
	}

	var lastErr error
	for attempt := range 2 {
		token, err := a.tokens.Get(ctx)
		if err != nil {
			// refreshError already implements DeliveryError; surface as-is.
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("cliq: build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		// Cliq REST requires "Zoho-oauthtoken <token>", not standard Bearer.
		req.Header.Set("Authorization", "Zoho-oauthtoken "+token)

		resp, err := a.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("cliq: http: %w", err)
		}

		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		statusErr := checkHTTPStatus(resp, respBody)
		if statusErr == nil {
			if attempt > 0 {
				// We recovered after one 401 + token refresh.
				cliqSendSelfHealedTotal.WithLabelValues("recovered").Inc()
				a.logger.Info("cliq: self-healed 401 with refreshed token", "url", url)
			}
			return resp, nil
		}

		lastErr = statusErr
		httpErr, ok := statusErr.(*HTTPError)
		// Self-heal only on 401 first attempt. Any other status (or 401 on retry)
		// falls through to return.
		if !ok || httpErr.Code != http.StatusUnauthorized || attempt > 0 {
			if attempt > 0 && ok && httpErr.Code == http.StatusUnauthorized {
				cliqSendSelfHealedTotal.WithLabelValues("exhausted").Inc()
			}
			return resp, statusErr
		}

		// First-attempt 401: invalidate cache, loop to refresh token.
		a.tokens.Invalidate()
		a.logger.Warn("cliq: 401 with cached token, invalidating + retrying",
			"url", url)
	}
	// Unreachable: loop bound is 2, each branch returns. Defensive return.
	return nil, lastErr
}

// checkHTTPStatus converts non-2xx Cliq responses into typed errors.
// Captures the Retry-After header so the pool can honour it on 429.
func checkHTTPStatus(resp *http.Response, body []byte) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	retryAfter := 0
	if s := resp.Header.Get("Retry-After"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			retryAfter = n
		}
	}
	return &HTTPError{Code: resp.StatusCode, Body: string(body), RetryAfter: retryAfter}
}

// HTTPError carries the HTTP status code so the pool can route Nak vs Term.
// Implements channels.DeliveryError via the StatusCode/IsRetryable/IsRateLimited
// methods — no import cycle because pkg/channels defines the interface only.
type HTTPError struct {
	Code       int    // HTTP status code
	Body       string // raw response body (for logging)
	RetryAfter int    // Retry-After seconds (0 = header absent)
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("cliq: http %d: %s", e.Code, e.Body)
}

// StatusCode returns the HTTP status code (used by pool classify4xx via interface).
func (e *HTTPError) StatusCode() int { return e.Code }

// IsRetryable returns true for 5xx (transient) — pool Nak's these.
func (e *HTTPError) IsRetryable() bool { return e.Code >= 500 }

// IsRateLimited returns true when Cliq returned 429.
func (e *HTTPError) IsRateLimited() bool { return e.Code == http.StatusTooManyRequests }

// RetryAfterSeconds returns the Retry-After header value in seconds.
func (e *HTTPError) RetryAfterSeconds() int { return e.RetryAfter }
