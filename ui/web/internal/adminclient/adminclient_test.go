package adminclient

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

type stubAdminServer struct {
	adminv1connect.UnimplementedAdminServiceHandler

	tenants  []*adminv1.Tenant
	accounts []*adminv1.Account
	channels []*adminv1.ChannelTypeInfo
	stream   []*adminv1.TailMessagesResponse
}

func (s *stubAdminServer) ListTenants(context.Context, *connect.Request[adminv1.ListTenantsRequest]) (*connect.Response[adminv1.ListTenantsResponse], error) {
	return connect.NewResponse(&adminv1.ListTenantsResponse{Tenants: s.tenants}), nil
}

func (s *stubAdminServer) ListChannelTypes(context.Context, *connect.Request[adminv1.ListChannelTypesRequest]) (*connect.Response[adminv1.ListChannelTypesResponse], error) {
	return connect.NewResponse(&adminv1.ListChannelTypesResponse{ChannelTypes: s.channels}), nil
}

func (s *stubAdminServer) ListAccounts(_ context.Context, req *connect.Request[adminv1.ListAccountsRequest]) (*connect.Response[adminv1.ListAccountsResponse], error) {
	out := make([]*adminv1.Account, 0, len(s.accounts))
	for _, account := range s.accounts {
		if account.GetTenantId() == req.Msg.GetTenantId() {
			out = append(out, account)
		}
	}
	return connect.NewResponse(&adminv1.ListAccountsResponse{Accounts: out}), nil
}

func (s *stubAdminServer) TailMessages(_ context.Context, _ *connect.Request[adminv1.TailMessagesRequest], stream *connect.ServerStream[adminv1.TailMessagesResponse]) error {
	for _, msg := range s.stream {
		if err := stream.Send(msg); err != nil {
			return err
		}
	}
	return nil
}

func startStub(t *testing.T, stub *stubAdminServer) (*Client, func()) {
	t.Helper()
	_, handler := adminv1connect.NewAdminServiceHandler(stub)
	mux := http.NewServeMux()
	mux.Handle("/mio.admin.v1.AdminService/", handler)
	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()

	return NewWithHTTPClient(srv.Client(), srv.URL), srv.Close
}

func TestClientListReads(t *testing.T) {
	client, cleanup := startStub(t, &stubAdminServer{
		tenants: []*adminv1.Tenant{
			{Id: "t1", Slug: "acme"},
		},
		channels: []*adminv1.ChannelTypeInfo{
			{Slug: "zoho_cliq", Status: "active"},
		},
		accounts: []*adminv1.Account{
			{Id: "a1", TenantId: "t1"},
			{Id: "a2", TenantId: "t2"},
		},
	})
	defer cleanup()

	tenants, err := client.ListTenants(context.Background())
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(tenants) != 1 || tenants[0].GetSlug() != "acme" {
		t.Fatalf("tenants: %+v", tenants)
	}

	channels, err := client.ListChannelTypes(context.Background())
	if err != nil {
		t.Fatalf("ListChannelTypes: %v", err)
	}
	if len(channels) != 1 || channels[0].GetSlug() != "zoho_cliq" {
		t.Fatalf("channels: %+v", channels)
	}

	accounts, err := client.ListAccounts(context.Background(), "t1")
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(accounts) != 1 || accounts[0].GetId() != "a1" {
		t.Fatalf("accounts: %+v", accounts)
	}
}

func TestClientTailMessages(t *testing.T) {
	client, cleanup := startStub(t, &stubAdminServer{
		stream: []*adminv1.TailMessagesResponse{
			{Id: "m1", AccountId: "a1", Text: "first", ReceivedAt: timestamppb.Now()},
			{Id: "m2", AccountId: "a1", Text: "second", ReceivedAt: timestamppb.Now()},
		},
	})
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stream, err := client.TailMessages(ctx, "a1", "")
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	defer stream.Close() //nolint:errcheck

	var got []string
	for stream.Receive() {
		got = append(got, stream.Msg().GetText())
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("messages: %v", got)
	}
}
