package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"
	"github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1/adminv1connect"
)

// stubServer is a connect AdminServiceHandler with canned responses.
// Embeds UnimplementedAdminServiceHandler so untested RPCs return
// CodeUnimplemented rather than panicking.
type stubServer struct {
	adminv1connect.UnimplementedAdminServiceHandler

	tenants  []*adminv1.Tenant
	accounts []*adminv1.Account
	channels []*adminv1.ChannelTypeInfo
	stream   []*adminv1.TailMessagesResponse
}

func (s *stubServer) ListTenants(_ context.Context, _ *connect.Request[adminv1.ListTenantsRequest]) (*connect.Response[adminv1.ListTenantsResponse], error) {
	return connect.NewResponse(&adminv1.ListTenantsResponse{Tenants: s.tenants}), nil
}

func (s *stubServer) ListAccounts(_ context.Context, req *connect.Request[adminv1.ListAccountsRequest]) (*connect.Response[adminv1.ListAccountsResponse], error) {
	out := make([]*adminv1.Account, 0, len(s.accounts))
	for _, a := range s.accounts {
		if req.Msg.GetTenantId() == "" || a.GetTenantId() == req.Msg.GetTenantId() {
			out = append(out, a)
		}
	}
	return connect.NewResponse(&adminv1.ListAccountsResponse{Accounts: out}), nil
}

func (s *stubServer) ListChannelTypes(_ context.Context, _ *connect.Request[adminv1.ListChannelTypesRequest]) (*connect.Response[adminv1.ListChannelTypesResponse], error) {
	return connect.NewResponse(&adminv1.ListChannelTypesResponse{ChannelTypes: s.channels}), nil
}

func (s *stubServer) TailMessages(_ context.Context, _ *connect.Request[adminv1.TailMessagesRequest], stream *connect.ServerStream[adminv1.TailMessagesResponse]) error {
	for _, m := range s.stream {
		if err := stream.Send(m); err != nil {
			return err
		}
	}
	return nil
}

// startStub mounts the stub handler on an httptest.Server with HTTP/2 +
// TLS so server-stream framing works without h2c plumbing. The returned
// Admin uses the server's auto-generated cert via srv.Client().
func startStub(t *testing.T, stub *stubServer) (Admin, func()) {
	t.Helper()
	_, handler := adminv1connect.NewAdminServiceHandler(stub)
	mux := http.NewServeMux()
	mux.Handle("/mio.admin.v1.AdminService/", handler)
	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	c := &httpAdmin{
		c: adminv1connect.NewAdminServiceClient(srv.Client(), srv.URL),
	}
	return c, srv.Close
}

func TestHTTPAdmin_ListTenants_UnwrapsResponse(t *testing.T) {
	admin, cleanup := startStub(t, &stubServer{tenants: []*adminv1.Tenant{
		{Id: "id-1", Slug: "acme"},
		{Id: "id-2", Slug: "globex"},
	}})
	defer cleanup()

	got, err := admin.ListTenants(context.Background())
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(got) != 2 || got[0].GetSlug() != "acme" || got[1].GetSlug() != "globex" {
		t.Errorf("unwrap: %+v", got)
	}
}

func TestHTTPAdmin_ListAccounts_FiltersByTenant(t *testing.T) {
	admin, cleanup := startStub(t, &stubServer{accounts: []*adminv1.Account{
		{Id: "a1", TenantId: "t-1", ChannelType: "zoho_cliq"},
		{Id: "a2", TenantId: "t-2", ChannelType: "zoho_cliq"},
		{Id: "a3", TenantId: "t-1", ChannelType: "slack"},
	}})
	defer cleanup()

	got, err := admin.ListAccounts(context.Background(), "t-1")
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len: %d want 2", len(got))
	}
	for _, a := range got {
		if a.GetTenantId() != "t-1" {
			t.Errorf("tenant filter leak: %q", a.GetTenantId())
		}
	}
}

func TestHTTPAdmin_ListChannelTypes_Unwraps(t *testing.T) {
	admin, cleanup := startStub(t, &stubServer{channels: []*adminv1.ChannelTypeInfo{
		{Slug: "zoho_cliq", Status: "active"},
	}})
	defer cleanup()

	got, err := admin.ListChannelTypes(context.Background())
	if err != nil {
		t.Fatalf("ListChannelTypes: %v", err)
	}
	if len(got) != 1 || got[0].GetSlug() != "zoho_cliq" {
		t.Errorf("unwrap: %+v", got)
	}
}

func TestHTTPAdmin_TailMessages_StreamUnwrapsAndCloses(t *testing.T) {
	stub := &stubServer{stream: []*adminv1.TailMessagesResponse{
		{Id: "m1", AccountId: "acct", Text: "one", ReceivedAt: timestamppb.Now()},
		{Id: "m2", AccountId: "acct", Text: "two", ReceivedAt: timestamppb.Now()},
		{Id: "m3", AccountId: "acct", Text: "three", ReceivedAt: timestamppb.Now()},
	}}
	admin, cleanup := startStub(t, stub)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, err := admin.TailMessages(ctx, "acct", "")
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}

	got := make([]string, 0, 3)
	for resp := range ch {
		got = append(got, resp.GetText())
	}
	if len(got) != 3 || got[0] != "one" || got[2] != "three" {
		t.Errorf("stream order/contents: %v", got)
	}
}

func TestHTTPAdmin_TailMessages_CtxCancelStopsStream(t *testing.T) {
	// Stub with no messages — the server handler returns immediately so the
	// channel closes. Still useful to confirm cancel doesn't deadlock.
	admin, cleanup := startStub(t, &stubServer{})
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := admin.TailMessages(ctx, "acct", "")
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	cancel()
	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("channel did not close after ctx cancel")
	}
}
