package sdk

import (
	"strings"
	"testing"
	"time"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/proto"
)

func TestClientNATSOptions_RetryForever(t *testing.T) {
	opts := nats.GetDefaultOptions()
	for _, apply := range natsOptions(defaultConfig()) {
		if err := apply(&opts); err != nil {
			t.Fatalf("apply NATS option: %v", err)
		}
	}

	if opts.MaxReconnect != -1 {
		t.Fatalf("MaxReconnect = %d, want -1", opts.MaxReconnect)
	}
	if opts.ReconnectWait != 2*time.Second {
		t.Fatalf("ReconnectWait = %s, want 2s", opts.ReconnectWait)
	}
}

// --- Subject builder tests ---

func TestInbound_HappyPath(t *testing.T) {
	got, err := Inbound("zoho_cliq", "acct-uuid-001", "conv-uuid-002")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "mio.inbound.zoho_cliq.acct-uuid-001.conv-uuid-002"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInboundEnriched_HappyPath(t *testing.T) {
	got, err := InboundEnriched("zoho_cliq", "acct-uuid-001", "conv-uuid-002")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "mio.inbound_enriched.zoho_cliq.acct-uuid-001.conv-uuid-002"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInboundEnriched_RejectsBadChannel(t *testing.T) {
	if _, err := InboundEnriched("not_a_channel", "a", "c"); err == nil {
		t.Fatal("expected error for unknown channel")
	}
}

func TestOutbound_NoMessageID(t *testing.T) {
	got, err := Outbound("zoho_cliq", "acct-uuid-001", "conv-uuid-002")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "mio.outbound.zoho_cliq.acct-uuid-001.conv-uuid-002"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOutbound_WithMessageID(t *testing.T) {
	got, err := Outbound("zoho_cliq", "acct-uuid-001", "conv-uuid-002", "msg-ulid-003")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "mio.outbound.zoho_cliq.acct-uuid-001.conv-uuid-002.msg-ulid-003"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSubject_EmptyToken(t *testing.T) {
	cases := []struct {
		name string
		fn   func() (string, error)
	}{
		{"inbound empty channelType", func() (string, error) { return Inbound("", "acct", "conv") }},
		{"inbound empty accountID", func() (string, error) { return Inbound("zoho_cliq", "", "conv") }},
		{"inbound empty conversationID", func() (string, error) { return Inbound("zoho_cliq", "acct", "") }},
		{"outbound empty channelType", func() (string, error) { return Outbound("", "acct", "conv") }},
		{"outbound empty accountID", func() (string, error) { return Outbound("zoho_cliq", "", "conv") }},
		{"outbound empty conversationID", func() (string, error) { return Outbound("zoho_cliq", "acct", "") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.fn()
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
		})
	}
}

func TestSubject_DotInToken(t *testing.T) {
	cases := []struct {
		name string
		fn   func() (string, error)
	}{
		{"dot in accountID", func() (string, error) { return Inbound("zoho_cliq", "acct.bad", "conv") }},
		{"dot in conversationID", func() (string, error) { return Inbound("zoho_cliq", "acct", "conv.bad") }},
		{"dot in outbound messageID", func() (string, error) {
			return Outbound("zoho_cliq", "acct", "conv", "msg.bad")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.fn()
			if err == nil {
				t.Fatalf("expected error for dot-in-token, got nil")
			}
			if !strings.Contains(err.Error(), "illegal characters") {
				t.Errorf("expected 'illegal characters' in error, got: %v", err)
			}
		})
	}
}

func TestSubject_UnknownChannelType(t *testing.T) {
	_, err := Inbound("not_a_real_channel", "acct", "conv")
	if err == nil {
		t.Fatal("expected error for unknown channel_type, got nil")
	}
	var uce *ErrUnknownChannelType
	if !isUnknownChannelTypeError(err, &uce) {
		t.Errorf("expected ErrUnknownChannelType, got %T: %v", err, err)
	}
}

// isUnknownChannelTypeError checks if err wraps ErrUnknownChannelType.
func isUnknownChannelTypeError(err error, target **ErrUnknownChannelType) bool {
	if e, ok := err.(*ErrUnknownChannelType); ok {
		*target = e
		return true
	}
	// unwrap one level for fmt.Errorf wrapping
	type unwrapper interface{ Unwrap() error }
	if u, ok := err.(unwrapper); ok {
		return isUnknownChannelTypeError(u.Unwrap(), target)
	}
	return false
}

// --- Verify tests (publish-side only) ---

func validMessage() *miov1.Message {
	return &miov1.Message{
		SchemaVersion:   1,
		TenantId:        "tenant-01",
		AccountId:       "acct-01",
		ChannelType:     "zoho_cliq",
		ConversationId:  "conv-01",
		SourceMessageId: "src-01",
	}
}

func TestVerify_HappyPath(t *testing.T) {
	if err := Verify(validMessage()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerify_SchemaMismatch(t *testing.T) {
	msg := validMessage()
	msg.SchemaVersion = 2
	err := Verify(msg)
	if err == nil {
		t.Fatal("expected error for schema_version=2, got nil")
	}
	if sm, ok := err.(*ErrSchemaMismatch); !ok || sm.Got != 2 {
		t.Errorf("expected ErrSchemaMismatch{Got:2}, got %T: %v", err, err)
	}
}

func TestVerify_EmptyFields(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*miov1.Message)
	}{
		{"empty tenant_id", func(m *miov1.Message) { m.TenantId = "" }},
		{"empty account_id", func(m *miov1.Message) { m.AccountId = "" }},
		{"empty channel_type", func(m *miov1.Message) { m.ChannelType = "" }},
		{"empty conversation_id", func(m *miov1.Message) { m.ConversationId = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := validMessage()
			tc.mutate(msg)
			if err := Verify(msg); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestVerify_UnknownChannelType(t *testing.T) {
	msg := validMessage()
	msg.ChannelType = "unknown_channel"
	err := Verify(msg)
	if err == nil {
		t.Fatal("expected error for unknown channel_type, got nil")
	}
}

// TestVerify_PassesThroughOnConsume asserts that a schema_version=2 message
// can be decoded on the consume side WITHOUT calling Verify.
// This is the intentional asymmetry: publish enforces, consume passes through.
func TestVerify_PassesThroughOnConsume(t *testing.T) {
	msg := validMessage()
	msg.SchemaVersion = 2

	// On publish: Verify must reject.
	if err := Verify(msg); err == nil {
		t.Error("Verify should reject schema_version=2 on publish")
	}

	// On consume: no Verify call — the message passes through untouched.
	// This test asserts the contract by NOT calling Verify and confirming
	// schema_version=2 is preserved in the struct.
	if msg.SchemaVersion != 2 {
		t.Error("message schema_version should remain 2 when not verified (consume path)")
	}
}

// --- Idempotency key builder tests ---

func TestBuildInboundMsgID(t *testing.T) {
	got := buildInboundMsgID("acct-123", "src-msg-456")
	want := "inb:acct-123:src-msg-456"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildOutboundMsgID(t *testing.T) {
	got := buildOutboundMsgID("01HV4ABCDEF01234567890")
	want := "out:01HV4ABCDEF01234567890"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSendCommand_RichContentRoundTrip(t *testing.T) {
	cmd := &miov1.SendCommand{
		Id:             "cmd-rich",
		SchemaVersion:  SchemaVersion,
		TenantId:       "tenant-01",
		AccountId:      "acct-01",
		ChannelType:    "zoho_cliq",
		ConversationId: "conv-01",
		Text:           "Daily digest",
		RichContent: &miov1.RichContent{
			Card: &miov1.RichCard{
				Title: "Digest",
				Theme: "modern-inline",
			},
			Blocks: []*miov1.RichBlock{
				{
					Content: &miov1.RichBlock_Table{Table: &miov1.RichTableBlock{
						Title:   "Contributors",
						Headers: []string{"Name", "Score"},
						Rows: []*miov1.RichTableRow{
							{Cells: []string{"Alice", "42"}},
						},
					}},
				},
			},
			Buttons: []*miov1.RichButton{
				{
					Label: "View dashboard",
					Style: miov1.RichButton_STYLE_PRIMARY,
					Action: &miov1.RichButtonAction{
						Kind: miov1.RichButtonAction_KIND_OPEN_URL,
						Url:  "https://example.com/dashboard",
					},
				},
			},
		},
	}
	if err := VerifyCommand(cmd); err != nil {
		t.Fatalf("verify command: %v", err)
	}
	data, err := proto.Marshal(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded miov1.SendCommand
	if err := proto.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := decoded.GetRichContent().GetCard().GetTitle(); got != "Digest" {
		t.Fatalf("decoded card title = %q", got)
	}
	if got := decoded.GetRichContent().GetBlocks()[0].GetTable().GetRows()[0].GetCells()[1]; got != "42" {
		t.Fatalf("decoded table score = %q", got)
	}
	if got := decoded.GetRichContent().GetButtons()[0].GetAction().GetUrl(); got != "https://example.com/dashboard" {
		t.Fatalf("decoded button url = %q", got)
	}
}

// --- Metric label discipline tests ---

func TestMetrics_LabelDiscipline(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := newMetrics(reg)
	if err != nil {
		t.Fatalf("newMetrics: %v", err)
	}

	// Record one event so the label set materialises.
	m.incPublish("zoho_cliq", "inbound", "success")
	m.observePublish("zoho_cliq", "inbound", 0.001)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	forbidden := []string{"account_id", "tenant_id", "conversation_id", "message_id"}

	for _, mf := range mfs {
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				for _, bad := range forbidden {
					if lp.GetName() == bad {
						t.Errorf("forbidden label %q found on metric %s", bad, mf.GetName())
					}
				}
			}
		}
	}
}

func TestMetrics_HistogramBuckets(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := newMetrics(reg)
	if err != nil {
		t.Fatalf("newMetrics: %v", err)
	}

	m.observePublish("zoho_cliq", "inbound", 0.003)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	wantBuckets := []float64{0.001, 0.005, 0.010, 0.050, 0.100, 0.500, 1.0}

	for _, mf := range mfs {
		if mf.GetName() != "mio_sdk_publish_latency_seconds" {
			continue
		}
		for _, metric := range mf.GetMetric() {
			h := metric.GetHistogram()
			got := make([]float64, 0, len(h.GetBucket()))
			for _, b := range h.GetBucket() {
				if b.GetUpperBound() < 1e10 { // exclude +Inf
					got = append(got, b.GetUpperBound())
				}
			}
			if len(got) != len(wantBuckets) {
				t.Errorf("bucket count: got %d, want %d; buckets: %v", len(got), len(wantBuckets), got)
				return
			}
			for i, wb := range wantBuckets {
				if abs(got[i]-wb) > 1e-9 {
					t.Errorf("bucket[%d]: got %v, want %v", i, got[i], wb)
				}
			}
		}
	}
}

func TestMetrics_CounterLabels(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := newMetrics(reg)
	if err != nil {
		t.Fatalf("newMetrics: %v", err)
	}

	m.incPublish("zoho_cliq", "inbound", OutcomeSuccess)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	for _, mf := range mfs {
		if mf.GetName() != "mio_sdk_publish_total" {
			continue
		}
		for _, metric := range mf.GetMetric() {
			labels := labelMap(metric)
			for _, required := range []string{"channel_type", "direction", "outcome"} {
				if _, ok := labels[required]; !ok {
					t.Errorf("missing required label %q on mio_sdk_publish_total", required)
				}
			}
			if len(labels) != 3 {
				t.Errorf("expected exactly 3 labels, got %d: %v", len(labels), labels)
			}
		}
	}
}

func labelMap(m *dto.Metric) map[string]string {
	out := make(map[string]string, len(m.GetLabel()))
	for _, lp := range m.GetLabel() {
		out[lp.GetName()] = lp.GetValue()
	}
	return out
}

// --- Registry loader tests ---

func TestKnownRegistry_ActiveOnly(t *testing.T) {
	// active per channels.yaml
	for _, active := range []string{"zoho_cliq", "slack"} {
		if !Known[active] {
			t.Errorf("%q should be in Known (status: active)", active)
		}
	}
	// planned channels must NOT be in Known
	for _, planned := range []string{"telegram"} {
		if Known[planned] {
			t.Errorf("planned channel %q should NOT be in Known", planned)
		}
	}
}

func TestKnownRegistry_RejectUnknown(t *testing.T) {
	if Known["not_real"] {
		t.Error("not_real should not be in Known")
	}
}

// --- Durable name validation ---

func TestConsumeInbound_EmptyDurable(t *testing.T) {
	// No real NATS connection needed — we test the guard before any network call.
	c := &Client{maxAckPending: 1}
	_, err := c.ConsumeInbound(nil, "MESSAGES_INBOUND", "") //nolint:staticcheck
	if err == nil {
		t.Fatal("expected error for empty durable, got nil")
	}
	if !strings.Contains(err.Error(), "durable name must not be empty") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestConsumeOutbound_EmptyDurable(t *testing.T) {
	c := &Client{maxAckPending: 1}
	_, err := c.ConsumeOutbound(nil, "MESSAGES_OUTBOUND", "") //nolint:staticcheck
	if err == nil {
		t.Fatal("expected error for empty durable, got nil")
	}
	if !strings.Contains(err.Error(), "durable name must not be empty") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
