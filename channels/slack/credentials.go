package slack

import (
	"context"
	"errors"

	"github.com/crashchat-ai/mio/pkg/channels"
)

// ErrNoOAuth is returned by AuthorizeURL/ExchangeCode: Slack v1 uses static
// bot/app tokens (xoxb/xapp) pasted by the operator, not an OAuth code dance.
// The admin layer branches on auth_kind="bot_token".
var ErrNoOAuth = errors.New("slack: bot_token auth has no OAuth authorize/exchange step (paste xoxb/xapp)")

// botTokenCredentials satisfies channels.CredentialAdapter for static tokens.
type botTokenCredentials struct{}

func (c *botTokenCredentials) AuthorizeURL(string) string { return "" }

func (c *botTokenCredentials) ExchangeCode(context.Context, string) (channels.Credential, error) {
	return channels.Credential{}, ErrNoOAuth
}

// RefreshCredential is a no-op: xoxb/xapp tokens are long-lived and rotated by
// the operator, not refreshed.
func (c *botTokenCredentials) RefreshCredential(_ context.Context, cur channels.Credential) (channels.Credential, error) {
	return cur, nil
}
