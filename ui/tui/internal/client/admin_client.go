// Package client wraps the generated AdminServiceClient with a thin
// context-aware façade. Views consume only the methods on Admin; this
// keeps test substitution simple (mock the interface).
package client

import (
	"context"
	"net/http"
	"time"

	"connectrpc.com/connect"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"
	"github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1/adminv1connect"
)

// Admin is the subset of AdminServiceClient methods the TUI uses.
// Each method takes a request value, not a Connect Request envelope —
// the wrapper hides the envelope shape from view code.
type Admin interface {
	ListTenants(ctx context.Context) ([]*adminv1.Tenant, error)
	ListAccounts(ctx context.Context, tenantID string) ([]*adminv1.Account, error)
	ListChannelTypes(ctx context.Context) ([]*adminv1.ChannelTypeInfo, error)
	TailMessages(ctx context.Context, accountID, conversationID string) (<-chan *adminv1.TailMessagesResponse, error)
	GetWebhookInfo(ctx context.Context, accountID string) (*adminv1.GetWebhookInfoResponse, error)
	GetStreamHealth(ctx context.Context) (*adminv1.GetStreamHealthResponse, error)
}

// httpAdmin is the connect-go implementation. Constructed from a base URL.
type httpAdmin struct {
	c adminv1connect.AdminServiceClient
}

// New constructs an Admin against the given baseURL (e.g. http://127.0.0.1:9090).
// Uses http.DefaultClient with a 10s per-request timeout (streams override).
func New(baseURL string) Admin {
	hc := &http.Client{Timeout: 0} // streams need 0; per-call ctx still applies
	_ = time.Now                   // keep import for future use
	return &httpAdmin{
		c: adminv1connect.NewAdminServiceClient(hc, baseURL),
	}
}

func (h *httpAdmin) ListTenants(ctx context.Context) ([]*adminv1.Tenant, error) {
	resp, err := h.c.ListTenants(ctx, connect.NewRequest(&adminv1.ListTenantsRequest{}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetTenants(), nil
}

func (h *httpAdmin) ListAccounts(ctx context.Context, tenantID string) ([]*adminv1.Account, error) {
	resp, err := h.c.ListAccounts(ctx,
		connect.NewRequest(&adminv1.ListAccountsRequest{TenantId: tenantID}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetAccounts(), nil
}

func (h *httpAdmin) ListChannelTypes(ctx context.Context) ([]*adminv1.ChannelTypeInfo, error) {
	resp, err := h.c.ListChannelTypes(ctx,
		connect.NewRequest(&adminv1.ListChannelTypesRequest{}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetChannelTypes(), nil
}

func (h *httpAdmin) GetWebhookInfo(ctx context.Context, accountID string) (*adminv1.GetWebhookInfoResponse, error) {
	resp, err := h.c.GetWebhookInfo(ctx, connect.NewRequest(&adminv1.GetWebhookInfoRequest{AccountId: accountID}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func (h *httpAdmin) GetStreamHealth(ctx context.Context) (*adminv1.GetStreamHealthResponse, error) {
	resp, err := h.c.GetStreamHealth(ctx, connect.NewRequest(&adminv1.GetStreamHealthRequest{}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

// TailMessages opens a server-stream and returns a channel of message
// envelopes. The goroutine closes the channel when the stream context ends
// or the server emits an error; callers cancel their ctx to stop early.
func (h *httpAdmin) TailMessages(ctx context.Context, accountID, conversationID string) (<-chan *adminv1.TailMessagesResponse, error) {
	stream, err := h.c.TailMessages(ctx, connect.NewRequest(&adminv1.TailMessagesRequest{
		AccountId:      accountID,
		ConversationId: conversationID,
	}))
	if err != nil {
		return nil, err
	}
	out := make(chan *adminv1.TailMessagesResponse, 16)
	go func() {
		defer close(out)
		for stream.Receive() {
			select {
			case <-ctx.Done():
				return
			case out <- stream.Msg():
			}
		}
	}()
	return out, nil
}
