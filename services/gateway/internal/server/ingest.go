package server

import (
	"context"
	"errors"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"github.com/crashchat-ai/mio/services/gateway/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"log/slog"
)

// ErrUnroutable is returned by Ingester.Ingest when no account matched and no
// env identity was set — a soft drop (the message is intentionally discarded,
// not failed). Callers that ACK a transport (Socket Mode) treat it as success.
var ErrUnroutable = errors.New("server: ingest: unroutable message (no account, no env identity)")

// Ingester runs the channel-agnostic produce/persist/publish tail shared by the
// HTTP webhook and the Socket Mode runner: route to account, ensure
// conversation, resolve the reply target, idempotent-upsert, publish. It never
// touches an http.ResponseWriter — the caller maps the outcome to a transport
// response. The HTTP path reuses an Ingester built from the pipeline's fields;
// the runner constructs one directly via NewIngester.
type Ingester struct {
	channelType string
	inbound     channels.InboundAdapter
	store       channels.Store
	pub         InboundPublisher
	accounts    AccountResolver
	tenantID    string
	accountID   string
	metrics     *gatewayMetrics
	logger      *slog.Logger
}

// NewIngester wires an Ingester from raw deps for the Socket Mode runner.
// reg is the runner's own Prometheus registry (kept separate from the HTTP
// server's so the shared inbound metric names do not double-register).
func NewIngester(
	channelType string,
	inbound channels.InboundAdapter,
	pg *pgxpool.Pool,
	pub InboundPublisher,
	accounts AccountResolver,
	tenantID, accountID string,
	reg prometheus.Registerer,
	logger *slog.Logger,
) *Ingester {
	if logger == nil {
		logger = slog.Default()
	}
	if reg == nil {
		reg = prometheus.NewRegistry()
	}
	return &Ingester{
		channelType: channelType,
		inbound:     inbound,
		store:       store.NewInboundStore(pg),
		pub:         pub,
		accounts:    accounts,
		tenantID:    tenantID,
		accountID:   accountID,
		metrics:     newGatewayMetrics(reg),
		logger:      logger,
	}
}

// Ingest persists and publishes one normalized message. The outcome string is
// the same vocabulary the HTTP pipeline maps to status codes
// ("success"/"dedup"/"unroutable"/"db_error"/"publish_error"). err is the
// underlying failure for the *_error outcomes (already logged); for "unroutable"
// it is ErrUnroutable so a transport caller can distinguish a soft drop.
func (in *Ingester) Ingest(ctx context.Context, msg *miov1.Message) (outcome string, err error) {
	tenantID, accountID, routed, err := in.resolveAccount(ctx, msg)
	if err != nil {
		in.logger.Error("ingest: account resolution failed", "channel", in.channelType, "err", err)
		return "db_error", err
	}
	if !routed {
		in.logger.Warn("ingest: unroutable — no matching account, no env identity",
			"channel", in.channelType,
			"source_message_id", msg.GetSourceMessageId())
		in.metrics.incUnroutable(in.channelType)
		return "unroutable", ErrUnroutable
	}
	msg.TenantId = tenantID
	msg.AccountId = accountID

	conv, err := in.store.EnsureConversation(ctx,
		uuid.New(),
		tenantID, accountID, in.channelType,
		msg.GetConversationKind().String(),
		msg.GetConversationExternalId(),
		nil,
		stringPtrIfNotEmpty(msg.GetParentConversationId()),
		stringPtrIfNotEmpty(msg.GetAttributes()[channels.AttrConversationDisplayName]),
		nil,
	)
	if err != nil {
		in.logger.Error("ingest: ensure conversation", "channel", in.channelType, "err", err)
		return "db_error", err
	}
	msg.ConversationId = conv.ID.String()

	threadRootMessageID := ""
	if target := msg.GetRelation().GetTargetExternalId(); target != "" {
		parentMsg, found, ferr := in.store.FindMessageBySource(ctx, accountID, target)
		if ferr != nil {
			in.logger.Error("ingest: resolve replied message",
				"channel", in.channelType, "err", ferr,
				"source_message_id", msg.GetSourceMessageId(),
				"parent_external_id", target)
			return "db_error", ferr
		}
		if found {
			msg.Relation.TargetMessageId = parentMsg.ID.String()
			threadRootMessageID = parentMsg.ThreadRootMessageID.String()
			msg.ThreadRootMessageId = threadRootMessageID
		} else {
			in.logger.Warn("ingest: replied message parent not found",
				"channel", in.channelType,
				"source_message_id", msg.GetSourceMessageId(),
				"parent_external_id", target,
				"account_id", accountID)
		}
	}

	dbMsgID, fresh, err := in.store.EnsureUniqueMessage(ctx,
		uuid.New(),
		tenantID, accountID,
		conv.ID.String(),
		stringPtrIfNotEmpty(threadRootMessageID),
		msg.GetSourceMessageId(),
		msg.GetSender().GetExternalId(),
		msg.GetText(),
		msg.GetAttributes(),
	)
	if err != nil {
		in.logger.Error("ingest: ensure unique message", "channel", in.channelType, "err", err)
		return "db_error", err
	}

	if !fresh {
		in.logger.Info("ingest: duplicate message suppressed",
			"channel", in.channelType,
			"source_message_id", msg.GetSourceMessageId(),
			"account_id", accountID)
		in.metrics.incDedup(in.channelType)
		return "dedup", nil
	}

	msg.Id = dbMsgID.String()

	if err := in.pub.PublishInbound(ctx, msg); err != nil {
		in.logger.Error("ingest: publish inbound", "channel", in.channelType, "err", err,
			"msg_id", dbMsgID, "conv_id", conv.ID)
		return "publish_error", err
	}

	in.logger.Info("ingest: message published",
		"channel", in.channelType,
		"msg_id", dbMsgID,
		"conv_id", conv.ID,
		"kind", msg.GetConversationKind().String(),
		"source_msg_id", msg.GetSourceMessageId(),
		"sender", msg.GetSender().GetExternalId(),
	)
	return "success", nil
}

// resolveAccount picks the routing identity: DB resolution first (workspace key
// from the adapter), env identity as compat fallback. Once DB resolution
// succeeds the env identity is never consulted, and a resolver ERROR aborts
// rather than falling back — a Postgres blip must not stamp the env tenant.
func (in *Ingester) resolveAccount(ctx context.Context, msg *miov1.Message) (tenantID, accountID string, ok bool, err error) {
	if in.accounts != nil {
		workspaceKey := ""
		if wk, isWK := in.inbound.(channels.WorkspaceKeyer); isWK {
			workspaceKey = wk.WorkspaceKey(msg)
		}
		res, resolved, rerr := in.accounts.Resolve(ctx, in.channelType, workspaceKey)
		if rerr != nil {
			return "", "", false, rerr
		}
		if resolved {
			return res.TenantID, res.AccountID, true, nil
		}
	}
	if in.tenantID == "" || in.accountID == "" {
		return "", "", false, nil
	}
	in.logger.Warn("ingest: using env identity fallback (MIO_TENANT_ID/MIO_ACCOUNT_ID) — "+
		"create an account row for DB-backed routing", "channel", in.channelType)
	return in.tenantID, in.accountID, true, nil
}
