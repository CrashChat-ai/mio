package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"

	_ "github.com/crashchat-ai/mio/channels/all"
	"github.com/crashchat-ai/mio/pkg/channels"
	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"
	"github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1/adminv1connect"
)

func newRegistryServer(t *testing.T) (adminv1connect.AdminServiceClient, *httptest.Server) {
	t.Helper()
	srv := NewServer(Deps{
		Registry:  channels.RegisteredAdapters(),
		PublicURL: "https://mio.example.com",
		Logger:    nil,
	})
	path, handler := adminv1connect.NewAdminServiceHandler(srv)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	hs := httptest.NewServer(mux)
	t.Cleanup(hs.Close)
	return adminv1connect.NewAdminServiceClient(http.DefaultClient, hs.URL), hs
}

func TestGetStreamHealth_NoSDK(t *testing.T) {
	client, _ := newRegistryServer(t)
	_, err := client.GetStreamHealth(context.Background(), connect.NewRequest(&adminv1.GetStreamHealthRequest{}))
	if err == nil {
		t.Fatal("expected error when SDK not configured")
	}
}

func TestWebhookAliases_FromAdapterRouteAliaser(t *testing.T) {
	var aliases []string
	for _, a := range channels.RegisteredAdapters() {
		if a.ChannelType() != "zoho_cliq" {
			continue
		}
		if ra, ok := a.Inbound().(channels.RouteAliaser); ok {
			aliases = ra.RouteAliases()
		}
	}
	if len(aliases) == 0 || aliases[0] != "/cliq" {
		t.Fatalf("expected [/cliq] from adapter RouteAliaser, got %v", aliases)
	}
}

func TestSetupHint(t *testing.T) {
	if setupHint("oauth2_refresh") == "" {
		t.Fatal("expected non-empty hint for oauth2_refresh")
	}
	if setupHint("hmac_webhook") == "" {
		t.Fatal("expected non-empty hint for hmac_webhook")
	}
}
