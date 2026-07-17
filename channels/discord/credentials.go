package discord

import (
	"context"
	"errors"

	"github.com/crashchat-ai/mio/pkg/channels"
)

// ErrNoOAuth is returned by AuthorizeURL/ExchangeCode: Discord v1 uses a
// static bot token pasted by the operator from the developer portal, not an
// OAuth code dance. The admin layer branches on auth_kind="bot_token".
var ErrNoOAuth = errors.New("discord: bot_token auth has no OAuth authorize/exchange step (paste the bot token)")

// botTokenCredentials satisfies channels.CredentialAdapter for static tokens.
type botTokenCredentials struct{}

func (c *botTokenCredentials) AuthorizeURL(string) string { return "" }

func (c *botTokenCredentials) ExchangeCode(context.Context, string) (channels.Credential, error) {
	return channels.Credential{}, ErrNoOAuth
}

// RefreshCredential is a no-op: bot tokens are long-lived and rotated by the
// operator, not refreshed.
func (c *botTokenCredentials) RefreshCredential(_ context.Context, cur channels.Credential) (channels.Credential, error) {
	return cur, nil
}
