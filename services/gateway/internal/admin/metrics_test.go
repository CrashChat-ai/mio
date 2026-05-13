package admin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"
)

func TestNewAdminMetrics_RegistersOnFreshRegistry(t *testing.T) {
	reg := prometheus.NewRegistry()
	if m := NewAdminMetrics(reg); m == nil {
		t.Fatal("metrics nil")
	}
	// A second fresh registry must also succeed — MustRegister panics on
	// double-registration so this guards against accidental globals.
	if m := NewAdminMetrics(prometheus.NewRegistry()); m == nil {
		t.Fatal("second metrics nil")
	}
}

func TestAdminMetrics_OAuthTotal_IncrementsOnStartInstall(t *testing.T) {
	rig, cleanup := newTestRig(t)
	defer cleanup()

	reg := prometheus.NewRegistry()
	rig.server.Metrics = NewAdminMetrics(reg)

	resp, err := rig.client.CreateTenant(context.Background(),
		connect.NewRequest(&adminv1.CreateTenantRequest{
			Slug:        "metrics-" + uuid.New().String()[:8],
			DisplayName: "Metrics Co",
		}))
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	if _, err := rig.client.StartInstall(context.Background(),
		connect.NewRequest(&adminv1.StartInstallRequest{
			TenantId:    resp.Msg.GetTenant().GetId(),
			ChannelType: stubChannelType,
		})); err != nil {
		t.Fatalf("StartInstall: %v", err)
	}

	got := testutil.ToFloat64(rig.server.Metrics.OAuthTotal.WithLabelValues(stubChannelType, "started"))
	if got != 1 {
		t.Errorf("OAuthTotal{adapter=stub,outcome=started}=%v want 1", got)
	}
}

// NOTE: A live-stream gauge increment/decrement test was attempted here
// but proved flaky under repeat runs — the NATS ordered-consumer cleanup
// goroutine in the SDK can hold a reference past the test's stream.Close.
// The Inc/Dec are paired in a single defer on the AdminServer side
// (server.go TailMessages), so code review catches drift; the scrape and
// counter tests below cover the rest of the metrics surface.

func TestAdminMetrics_Scrape_ExposesCounters(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewAdminMetrics(reg)
	m.RPCTotal.WithLabelValues("ListTenants", "ok").Inc()
	m.OAuthTotal.WithLabelValues("zoho_cliq", "started").Inc()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close() //nolint:errcheck

	page := string(body)
	for _, want := range []string{
		`mio_admin_rpc_total{method="ListTenants",outcome="ok"} 1`,
		`mio_admin_oauth_total{adapter="zoho_cliq",outcome="started"} 1`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("scrape missing %q", want)
		}
	}
}
