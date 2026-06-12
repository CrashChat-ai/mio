package admin

import (
	"context"
	"errors"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/crashchat-ai/mio/pkg/channels"
	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"
	"github.com/crashchat-ai/mio/services/gateway/store"
)

func (s *AdminServer) GetWebhookInfo(ctx context.Context, req *connect.Request[adminv1.GetWebhookInfoRequest]) (*connect.Response[adminv1.GetWebhookInfoResponse], error) {
	id, err := uuid.Parse(req.Msg.GetAccountId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	acct, err := store.GetAccount(ctx, s.Pool, id)
	if err != nil {
		if errors.Is(err, store.ErrAccountNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	adapter := s.adapterByChannelType(acct.ChannelType)
	authKind := ""
	var aliases []string
	if adapter != nil {
		authKind = adapter.Capabilities().GetAuthKind()
		if inb := adapter.Inbound(); inb != nil {
			if ra, ok := inb.(channels.RouteAliaser); ok {
				aliases = ra.RouteAliases()
			}
		}
	}

	base := s.gatewayPublicURL
	if base == "" {
		base = s.publicURL
	}
	urlSlug := strings.ReplaceAll(acct.ChannelType, "_", "-")
	webhookURL := ""
	if base != "" {
		webhookURL = strings.TrimSuffix(base, "/") + "/webhooks/" + urlSlug
	}

	hint := setupHint(authKind)

	return connect.NewResponse(&adminv1.GetWebhookInfoResponse{
		AccountId:    acct.ID.String(),
		ChannelType:  acct.ChannelType,
		WebhookUrl:   webhookURL,
		RouteAliases: aliases,
		AuthKind:     authKind,
		SetupHint:    hint,
	}), nil
}

func (s *AdminServer) GetStreamHealth(ctx context.Context, _ *connect.Request[adminv1.GetStreamHealthRequest]) (*connect.Response[adminv1.GetStreamHealthResponse], error) {
	if s.SDK == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("SDK client not configured"))
	}

	js := s.SDK.JetStream()
	out := &adminv1.GetStreamHealthResponse{}

	for _, streamName := range []string{store.StreamInbound, store.StreamOutbound} {
		stream, err := js.Stream(ctx, streamName)
		if err != nil {
			s.Logger.Warn("admin: stream health: stream lookup failed", "stream", streamName, "error", err)
			continue
		}

		lister := stream.ListConsumers(ctx)
		for ci := range lister.Info() {
			consumer := consumerHealthFromInfo(streamName, ci)
			out.Consumers = append(out.Consumers, consumer)
		}
		if err := lister.Err(); err != nil {
			s.Logger.Warn("admin: stream health: list consumers failed", "stream", streamName, "error", err)
		}
	}

	return connect.NewResponse(out), nil
}

func consumerHealthFromInfo(stream string, ci *jetstream.ConsumerInfo) *adminv1.ConsumerHealth {
	name := ci.Config.Durable
	if name == "" {
		name = ci.Name
	}
	h := &adminv1.ConsumerHealth{
		ConsumerName:  name,
		Stream:        stream,
		NumPending:    ci.NumPending,
		NumAckPending: uint64(ci.NumAckPending),
	}
	if ci.Delivered.Last != nil && !ci.Delivered.Last.IsZero() {
		h.LastDelivered = timestamppb.New(*ci.Delivered.Last)
	}
	return h
}

func setupHint(authKind string) string {
	switch authKind {
	case "oauth2_refresh":
		return "Click Start Install to begin the OAuth flow. After authorizing, click Complete Install to finalize."
	case "hmac_webhook":
		return "Configure the webhook URL in the platform dashboard and paste the signing secret in the credential form."
	case "bot_token":
		return "Paste the bot token into the credential form. No OAuth flow required."
	default:
		return "Configure the webhook URL in the platform and complete the install flow."
	}
}
