package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"

	// Register Cliq via init() so RegisteredAdapters returns at least one.
	_ "github.com/crashchat-ai/mio/channels/all"
	"github.com/crashchat-ai/mio/services/gateway/sender"
	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"
	"github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1/adminv1connect"
)

// TestListChannelTypes_ReturnsCliqVerbatim spins up the connect server
// in-process (no DB) and verifies ListChannelTypes returns the zoho_cliq
// row with auth_kind=oauth2_refresh. This is the contract the TUI and
// admin UI consume.
func TestListChannelTypes_ReturnsCliqVerbatim(t *testing.T) {
	srv := NewServer(Deps{
		Registry: sender.RegisteredAdapters(),
		Logger:   nil,
	})

	path, handler := adminv1connect.NewAdminServiceHandler(srv)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	client := adminv1connect.NewAdminServiceClient(http.DefaultClient, httpSrv.URL)
	resp, err := client.ListChannelTypes(context.Background(),
		connect.NewRequest(&adminv1.ListChannelTypesRequest{}))
	if err != nil {
		t.Fatalf("rpc: %v", err)
	}
	got := resp.Msg.ChannelTypes
	if len(got) == 0 {
		t.Fatal("expected at least one channel type")
	}
	var cliq *adminv1.ChannelTypeInfo
	for _, c := range got {
		if c.GetSlug() == "zoho_cliq" {
			cliq = c
			break
		}
	}
	if cliq == nil {
		t.Fatal("zoho_cliq not present in response")
	}
	if cliq.GetCapabilities().GetAuthKind() != "oauth2_refresh" {
		t.Errorf("auth_kind: %q", cliq.GetCapabilities().GetAuthKind())
	}
	if !cliq.GetCapabilities().GetSupportsEdit() {
		t.Errorf("supports_edit should be true")
	}
}
