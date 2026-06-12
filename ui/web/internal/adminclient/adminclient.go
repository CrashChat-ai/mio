package adminclient

import (
	"context"
	"net/http"

	"connectrpc.com/connect"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"
	"github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1/adminv1connect"
)

type Admin interface {
	CreateTenant(ctx context.Context, slug, displayName string) (*adminv1.Tenant, error)
	ListTenants(ctx context.Context) ([]*adminv1.Tenant, error)
	ListChannelTypes(ctx context.Context) ([]*adminv1.ChannelTypeInfo, error)
	StartInstall(ctx context.Context, tenantID, channelType, provider string) (*adminv1.StartInstallResponse, error)
	CompleteInstall(ctx context.Context, installID string) (*adminv1.Account, error)
	GetAccount(ctx context.Context, accountID string) (*adminv1.Account, error)
	UpdateAccount(ctx context.Context, accountID, displayName, externalID string) (*adminv1.Account, error)
	SetRateLimit(ctx context.Context, accountID string, perSecond int32, scope string) (*adminv1.Account, error)
	GetCredentialMetadata(ctx context.Context, accountID string) (*adminv1.GetCredentialMetadataResponse, error)
	ListAccounts(ctx context.Context, tenantID string) ([]*adminv1.Account, error)
	DisableAccount(ctx context.Context, accountID string) error
	RotateCredential(ctx context.Context, accountID string) error
	TailMessages(ctx context.Context, accountID, conversationID string) (MessageStream, error)
	GetWebhookInfo(ctx context.Context, accountID string) (*adminv1.GetWebhookInfoResponse, error)
	GetStreamHealth(ctx context.Context) (*adminv1.GetStreamHealthResponse, error)
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

func (c *Client) CreateTenant(ctx context.Context, slug, displayName string) (*adminv1.Tenant, error) {
	resp, err := c.c.CreateTenant(ctx, connect.NewRequest(&adminv1.CreateTenantRequest{
		Slug:        slug,
		DisplayName: displayName,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetTenant(), nil
}

func (c *Client) ListChannelTypes(ctx context.Context) ([]*adminv1.ChannelTypeInfo, error) {
	resp, err := c.c.ListChannelTypes(ctx, connect.NewRequest(&adminv1.ListChannelTypesRequest{}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetChannelTypes(), nil
}

func (c *Client) StartInstall(ctx context.Context, tenantID, channelType, provider string) (*adminv1.StartInstallResponse, error) {
	resp, err := c.c.StartInstall(ctx, connect.NewRequest(&adminv1.StartInstallRequest{
		TenantId:    tenantID,
		ChannelType: channelType,
		Provider:    provider,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func (c *Client) CompleteInstall(ctx context.Context, installID string) (*adminv1.Account, error) {
	resp, err := c.c.CompleteInstall(ctx, connect.NewRequest(&adminv1.CompleteInstallRequest{
		InstallId: installID,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetAccount(), nil
}

func (c *Client) GetAccount(ctx context.Context, accountID string) (*adminv1.Account, error) {
	resp, err := c.c.GetAccount(ctx, connect.NewRequest(&adminv1.GetAccountRequest{
		AccountId: accountID,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetAccount(), nil
}

func (c *Client) UpdateAccount(ctx context.Context, accountID, displayName, externalID string) (*adminv1.Account, error) {
	resp, err := c.c.UpdateAccount(ctx, connect.NewRequest(&adminv1.UpdateAccountRequest{
		AccountId:   accountID,
		DisplayName: displayName,
		ExternalId:  externalID,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetAccount(), nil
}

func (c *Client) SetRateLimit(ctx context.Context, accountID string, perSecond int32, scope string) (*adminv1.Account, error) {
	resp, err := c.c.SetRateLimit(ctx, connect.NewRequest(&adminv1.SetRateLimitRequest{
		AccountId:          accountID,
		RateLimitPerSecond: perSecond,
		RateLimitScope:     scope,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetAccount(), nil
}

func (c *Client) GetCredentialMetadata(ctx context.Context, accountID string) (*adminv1.GetCredentialMetadataResponse, error) {
	resp, err := c.c.GetCredentialMetadata(ctx, connect.NewRequest(&adminv1.GetCredentialMetadataRequest{
		AccountId: accountID,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
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

func (c *Client) DisableAccount(ctx context.Context, accountID string) error {
	_, err := c.c.DisableAccount(ctx, connect.NewRequest(&adminv1.DisableAccountRequest{
		AccountId: accountID,
	}))
	return err
}

func (c *Client) RotateCredential(ctx context.Context, accountID string) error {
	_, err := c.c.RotateCredential(ctx, connect.NewRequest(&adminv1.RotateCredentialRequest{
		AccountId: accountID,
	}))
	return err
}

func (c *Client) TailMessages(ctx context.Context, accountID, conversationID string) (MessageStream, error) {
	return c.c.TailMessages(ctx, connect.NewRequest(&adminv1.TailMessagesRequest{
		AccountId:      accountID,
		ConversationId: conversationID,
	}))
}

func (c *Client) GetWebhookInfo(ctx context.Context, accountID string) (*adminv1.GetWebhookInfoResponse, error) {
	resp, err := c.c.GetWebhookInfo(ctx, connect.NewRequest(&adminv1.GetWebhookInfoRequest{
		AccountId: accountID,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func (c *Client) GetStreamHealth(ctx context.Context) (*adminv1.GetStreamHealthResponse, error) {
	resp, err := c.c.GetStreamHealth(ctx, connect.NewRequest(&adminv1.GetStreamHealthRequest{}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}
