package adminclient

import (
	"context"
	"net/http"

	"connectrpc.com/connect"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"
	"github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1/adminv1connect"
)

type Admin interface {
	ListTenants(ctx context.Context) ([]*adminv1.Tenant, error)
	ListChannelTypes(ctx context.Context) ([]*adminv1.ChannelTypeInfo, error)
	ListAccounts(ctx context.Context, tenantID string) ([]*adminv1.Account, error)
	TailMessages(ctx context.Context, accountID, conversationID string) (MessageStream, error)
}

type MessageStream interface {
	Receive() bool
	Msg() *adminv1.TailMessagesResponse
	Err() error
	Close() error
}

type Client struct {
	c adminv1connect.AdminServiceClient
}

func New(baseURL string) *Client {
	return NewWithHTTPClient(http.DefaultClient, baseURL)
}

func NewWithHTTPClient(hc *http.Client, baseURL string) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{
		c: adminv1connect.NewAdminServiceClient(hc, baseURL),
	}
}

func (c *Client) ListTenants(ctx context.Context) ([]*adminv1.Tenant, error) {
	resp, err := c.c.ListTenants(ctx, connect.NewRequest(&adminv1.ListTenantsRequest{}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetTenants(), nil
}

func (c *Client) ListChannelTypes(ctx context.Context) ([]*adminv1.ChannelTypeInfo, error) {
	resp, err := c.c.ListChannelTypes(ctx, connect.NewRequest(&adminv1.ListChannelTypesRequest{}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetChannelTypes(), nil
}

func (c *Client) ListAccounts(ctx context.Context, tenantID string) ([]*adminv1.Account, error) {
	resp, err := c.c.ListAccounts(ctx, connect.NewRequest(&adminv1.ListAccountsRequest{
		TenantId: tenantID,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetAccounts(), nil
}

func (c *Client) TailMessages(ctx context.Context, accountID, conversationID string) (MessageStream, error) {
	return c.c.TailMessages(ctx, connect.NewRequest(&adminv1.TailMessagesRequest{
		AccountId:      accountID,
		ConversationId: conversationID,
	}))
}
