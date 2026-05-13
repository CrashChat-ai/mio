package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"
	"github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1/adminv1connect"

	"github.com/crashchat-ai/mio/pkg/channels"
	"github.com/crashchat-ai/mio/services/gateway/internal/crypto"
	"github.com/crashchat-ai/mio/services/gateway/store"
	sdk "github.com/crashchat-ai/mio/sdk-go"
)

// AdminServer satisfies adminv1connect.AdminServiceHandler. Constructed
// once at boot; safe for concurrent use across goroutines (all state is
// either immutable references or independently-thread-safe).
type AdminServer struct {
	adminv1connect.UnimplementedAdminServiceHandler

	Pool     *pgxpool.Pool
	Cipher   crypto.Cipher
	SDK      *sdk.Client
	Registry []channels.Adapter
	Metrics  *AdminMetrics
	Logger   *slog.Logger

	stash     *installStash
	publicURL string // e.g. http://127.0.0.1:9090 — used for redirect_uri
}

// Deps groups required dependencies for NewServer.
type Deps struct {
	Pool      *pgxpool.Pool
	Cipher    crypto.Cipher
	SDK       *sdk.Client
	Registry  []channels.Adapter
	Metrics   *AdminMetrics
	Logger    *slog.Logger
	PublicURL string
}

// NewServer constructs an AdminServer with sane defaults. Logger defaults
// to slog.Default(); Metrics may be nil (no-op).
func NewServer(d Deps) *AdminServer {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	return &AdminServer{
		Pool:      d.Pool,
		Cipher:    d.Cipher,
		SDK:       d.SDK,
		Registry:  d.Registry,
		Metrics:   d.Metrics,
		Logger:    d.Logger,
		stash:     newInstallStash(),
		publicURL: d.PublicURL,
	}
}

// PublicURL returns the configured external URL (e.g. for callback links).
func (s *AdminServer) PublicURL() string { return s.publicURL }

// Stash returns the install code stash so the callback HTTP handler (in
// oauth_callback.go) can capture codes against reserved state nonces.
func (s *AdminServer) Stash() *installStash { return s.stash }

// StartBackground launches background goroutines tied to ctx's lifetime.
// Currently: a ticker that sweeps expired install-stash entries so
// abandoned StartInstall flows don't leak state nonces. Call exactly once
// per AdminServer after wiring; it returns immediately.
func (s *AdminServer) StartBackground(ctx context.Context) {
	go s.stash.startStashPurger(ctx, installStashPurgeInterval)
}

// adapterByChannelType walks the registry; returns nil on miss.
func (s *AdminServer) adapterByChannelType(slug string) channels.Adapter {
	for _, a := range s.Registry {
		if a.ChannelType() == slug {
			return a
		}
	}
	return nil
}

// ── Tenants ────────────────────────────────────────────────────────────────

func (s *AdminServer) CreateTenant(ctx context.Context, req *connect.Request[adminv1.CreateTenantRequest]) (*connect.Response[adminv1.CreateTenantResponse], error) {
	slug := req.Msg.GetSlug()
	if slug == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("slug required"))
	}
	id := uuid.New()
	t, err := store.EnsureTenant(ctx, s.Pool, id, slug, req.Msg.GetDisplayName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&adminv1.CreateTenantResponse{Tenant: tenantToProto(t)}), nil
}

func (s *AdminServer) ListTenants(ctx context.Context, _ *connect.Request[adminv1.ListTenantsRequest]) (*connect.Response[adminv1.ListTenantsResponse], error) {
	list, err := store.ListTenants(ctx, s.Pool)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &adminv1.ListTenantsResponse{}
	for _, t := range list {
		out.Tenants = append(out.Tenants, tenantToProto(t))
	}
	return connect.NewResponse(out), nil
}

func (s *AdminServer) GetTenant(ctx context.Context, req *connect.Request[adminv1.GetTenantRequest]) (*connect.Response[adminv1.GetTenantResponse], error) {
	id, err := uuid.Parse(req.Msg.GetId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	t, err := store.GetTenant(ctx, s.Pool, id)
	if err != nil {
		if errors.Is(err, store.ErrTenantNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&adminv1.GetTenantResponse{Tenant: tenantToProto(t)}), nil
}

// ── Channel types ──────────────────────────────────────────────────────────

func (s *AdminServer) ListChannelTypes(ctx context.Context, _ *connect.Request[adminv1.ListChannelTypesRequest]) (*connect.Response[adminv1.ListChannelTypesResponse], error) {
	out := &adminv1.ListChannelTypesResponse{}
	for _, a := range s.Registry {
		out.ChannelTypes = append(out.ChannelTypes, &adminv1.ChannelTypeInfo{
			Slug:         a.ChannelType(),
			Capabilities: a.Capabilities(),
			Status:       "active",
		})
	}
	return connect.NewResponse(out), nil
}

// ── Installs / OAuth ───────────────────────────────────────────────────────

// randHex returns a crypto/rand 32-byte hex string (state nonce / install id
// state token). Falls back to UUID-derived bytes on extreme entropy failure.
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is catastrophic; surface via uuid v4 fallback
		// rather than crashing the admin server.
		return uuid.New().String()
	}
	return hex.EncodeToString(b)
}

func (s *AdminServer) StartInstall(ctx context.Context, req *connect.Request[adminv1.StartInstallRequest]) (*connect.Response[adminv1.StartInstallResponse], error) {
	tenantID, err := uuid.Parse(req.Msg.GetTenantId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	channelType := req.Msg.GetChannelType()
	if channelType == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("channel_type required"))
	}
	adapter := s.adapterByChannelType(channelType)
	if adapter == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("unknown channel_type %q", channelType))
	}

	// Reserve install_id + state nonce; persist an installs row in `pending`.
	installID := uuid.New()
	state := randHex(32)

	provider := req.Msg.GetProvider()
	if provider == "" {
		provider = "default"
	}

	// installs.account_id has a NOT NULL FK to accounts(id), so the account
	// must exist before we can write the installs row. We allocate a
	// placeholder account row with external_id="pending:<state[:8]>" so the
	// FK holds; CompleteInstall promotes external_id once the platform's
	// canonical id is discovered post-exchange (Cliq doesn't return it in
	// the token response; a separate introspection call is a follow-up).
	if _, err := store.CreateAccount(ctx, s.Pool, installID, tenantID, channelType,
		provider, "pending:"+state[:8], "pending install", nil); err != nil {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("create placeholder account: %w", err))
	}
	if _, err := s.Pool.Exec(ctx, `
		INSERT INTO installs (id, account_id, state, created_at)
		VALUES ($1, $2, 'pending', NOW())`, installID, installID); err != nil {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("insert installs row: %w", err))
	}

	s.stash.reserve(installID.String(), state)

	creds := adapter.Credentials()
	authURL := ""
	if creds != nil {
		authURL = creds.AuthorizeURL(state)
	}

	redirectURI := s.publicURL + "/oauth/callback"
	if s.Metrics != nil {
		s.Metrics.OAuthTotal.WithLabelValues(channelType, "started").Inc()
	}
	return connect.NewResponse(&adminv1.StartInstallResponse{
		InstallId:   installID.String(),
		OauthUrl:    authURL,
		RedirectUri: redirectURI,
	}), nil
}

func (s *AdminServer) CompleteInstall(ctx context.Context, req *connect.Request[adminv1.CompleteInstallRequest]) (*connect.Response[adminv1.CompleteInstallResponse], error) {
	installID := req.Msg.GetInstallId()
	if installID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("install_id required"))
	}
	sc, ok := s.stash.consume(installID)
	if !ok {
		if s.Metrics != nil {
			s.Metrics.OAuthTotal.WithLabelValues("unknown", "failed").Inc()
		}
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("no stashed OAuth code for install_id (callback never received or expired)"))
	}

	// Look up the install row to find the placeholder account + channel_type.
	// installState is the lifecycle column on installs (pending|active|failed),
	// distinct from the OAuth `state` nonce held in the stash.
	var (
		accountID    uuid.UUID
		installState string
		channelTyp   string
	)
	if err := s.Pool.QueryRow(ctx, `
		SELECT i.account_id, i.state, a.channel_type
		FROM installs i JOIN accounts a ON a.id = i.account_id
		WHERE i.id = $1`, installID).Scan(&accountID, &installState, &channelTyp); err != nil {
		return nil, connect.NewError(connect.CodeNotFound,
			fmt.Errorf("install row missing for id=%s: %w", installID, err))
	}
	_ = installState // currently unused; future check could reject re-completing an already-active install
	adapter := s.adapterByChannelType(channelTyp)
	if adapter == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("adapter %q no longer registered", channelTyp))
	}
	creds := adapter.Credentials()
	if creds == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("adapter has no credential flow"))
	}
	cred, err := creds.ExchangeCode(ctx, sc.code)
	if err != nil {
		if s.Metrics != nil {
			s.Metrics.OAuthTotal.WithLabelValues(channelTyp, "failed").Inc()
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("exchange code: %w", err))
	}

	// Persist encrypted credential.
	payload := store.CredentialPayload{
		AccessToken:  cred.AccessToken,
		RefreshToken: cred.RefreshToken,
		ExpiresAt:    cred.ExpiresAt,
		Extras:       cred.Extras,
	}
	if err := store.PutCredential(ctx, s.Pool, s.Cipher, accountID,
		adapter.Capabilities().GetAuthKind(), payload); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("put credential: %w", err))
	}

	// Flip install state to active, stamp installed_at.
	if _, err := s.Pool.Exec(ctx, `
		UPDATE installs SET state='active', installed_at=NOW(), error_reason=NULL
		WHERE id = $1`, installID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	acct, err := store.GetAccount(ctx, s.Pool, accountID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if s.Metrics != nil {
		s.Metrics.OAuthTotal.WithLabelValues(channelTyp, "completed").Inc()
	}
	s.Logger.Info("admin: install completed",
		"tenant_id", acct.TenantID,
		"account_id", acct.ID,
		"channel_type", channelTyp)
	return connect.NewResponse(&adminv1.CompleteInstallResponse{Account: accountToProto(acct)}), nil
}

// ── Accounts ───────────────────────────────────────────────────────────────

func (s *AdminServer) ListAccounts(ctx context.Context, req *connect.Request[adminv1.ListAccountsRequest]) (*connect.Response[adminv1.ListAccountsResponse], error) {
	tid, err := uuid.Parse(req.Msg.GetTenantId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	list, err := store.ListAccounts(ctx, s.Pool, tid)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &adminv1.ListAccountsResponse{}
	for _, a := range list {
		out.Accounts = append(out.Accounts, accountToProto(a))
	}
	return connect.NewResponse(out), nil
}

func (s *AdminServer) DisableAccount(ctx context.Context, req *connect.Request[adminv1.DisableAccountRequest]) (*connect.Response[adminv1.DisableAccountResponse], error) {
	id, err := uuid.Parse(req.Msg.GetAccountId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := store.DisableAccount(ctx, s.Pool, id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.Logger.Info("admin: account disabled", "account_id", id)
	return connect.NewResponse(&adminv1.DisableAccountResponse{}), nil
}

func (s *AdminServer) RotateCredential(ctx context.Context, req *connect.Request[adminv1.RotateCredentialRequest]) (*connect.Response[adminv1.RotateCredentialResponse], error) {
	id, err := uuid.Parse(req.Msg.GetAccountId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	// Read current credential, refresh via the adapter, re-persist.
	row, err := store.GetCredential(ctx, s.Pool, s.Cipher, id, s.Logger)
	if err != nil {
		if errors.Is(err, store.ErrCredentialNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	acct, err := store.GetAccount(ctx, s.Pool, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	adapter := s.adapterByChannelType(acct.ChannelType)
	if adapter == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("adapter %q no longer registered", acct.ChannelType))
	}
	creds := adapter.Credentials()
	if creds == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("adapter has no credential flow"))
	}
	cur := channels.Credential{
		AccessToken:  row.Plaintext.AccessToken,
		RefreshToken: row.Plaintext.RefreshToken,
		ExpiresAt:    row.Plaintext.ExpiresAt,
		Extras:       row.Plaintext.Extras,
	}
	nextCred, err := creds.RefreshCredential(ctx, cur)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("refresh: %w", err))
	}
	if err := store.PutCredential(ctx, s.Pool, s.Cipher, id, row.AuthKind, store.CredentialPayload{
		AccessToken:  nextCred.AccessToken,
		RefreshToken: nextCred.RefreshToken,
		ExpiresAt:    nextCred.ExpiresAt,
		Extras:       nextCred.Extras,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.Logger.Info("admin: credential rotated",
		"account_id", id, "tenant_id", acct.TenantID)
	return connect.NewResponse(&adminv1.RotateCredentialResponse{}), nil
}

// ── Tail ───────────────────────────────────────────────────────────────────

// TailMessages is a server-stream of inbound messages for an account.
// Uses an ephemeral JetStream consumer via sdk.ConsumeInbound; cancelled
// cleanly when the stream context ends. Server-side filter:
//   - account_id MUST match (consumer-level subject filter ideally; here
//     enforced post-deserialize for simplicity).
//   - conversation_id filters in-handler if non-empty.
func (s *AdminServer) TailMessages(
	ctx context.Context,
	req *connect.Request[adminv1.TailMessagesRequest],
	stream *connect.ServerStream[adminv1.TailMessagesResponse],
) error {
	if s.Metrics != nil {
		s.Metrics.TailActive.Inc()
		defer s.Metrics.TailActive.Dec()
	}
	accountID := req.Msg.GetAccountId()
	if accountID == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("account_id required"))
	}
	conv := req.Msg.GetConversationId()

	if s.SDK == nil {
		return connect.NewError(connect.CodeUnimplemented, errors.New("SDK client not configured"))
	}

	// Ephemeral ordered consumer — self-destructs on ctx cancel; each
	// TailMessages caller gets its own live-tail slice of MESSAGES_INBOUND.
	deliveries, err := s.SDK.ConsumeInboundEphemeral(ctx, store.StreamInbound)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("consume inbound: %w", err))
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case d, ok := <-deliveries:
			if !ok {
				return nil
			}
			m := d.Msg()
			if m == nil {
				_ = d.Term()
				continue
			}
			if m.GetAccountId() != accountID {
				_ = d.Ack()
				continue
			}
			if conv != "" && m.GetConversationId() != conv {
				_ = d.Ack()
				continue
			}
			senderDisplay := ""
			if m.GetSender() != nil {
				senderDisplay = m.GetSender().GetDisplayName()
			}
			if err := stream.Send(&adminv1.TailMessagesResponse{
				Id:             m.GetId(),
				TenantId:       m.GetTenantId(),
				AccountId:      m.GetAccountId(),
				ConversationId: m.GetConversationId(),
				ChannelType:    m.GetChannelType(),
				SenderDisplay:  senderDisplay,
				Text:           m.GetText(),
				ReceivedAt:     m.GetReceivedAt(),
			}); err != nil {
				_ = d.Term()
				return err
			}
			_ = d.Ack()
		}
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

func tenantToProto(t store.Tenant) *adminv1.Tenant {
	out := &adminv1.Tenant{
		Id:          t.ID.String(),
		Slug:        t.Slug,
		DisplayName: t.DisplayName,
		Status:      t.Status,
		CreatedAt:   timestamppb.New(t.CreatedAt),
	}
	if t.DisabledAt != nil {
		out.DisabledAt = timestamppb.New(*t.DisabledAt)
	}
	return out
}

func accountToProto(a store.Account) *adminv1.Account {
	out := &adminv1.Account{
		Id:          a.ID.String(),
		TenantId:    a.TenantID.String(),
		ChannelType: a.ChannelType,
		Provider:    a.Provider,
		ExternalId:  a.ExternalID,
		DisplayName: a.DisplayName,
		CreatedAt:   timestamppb.New(a.CreatedAt),
	}
	if a.DisabledAt != nil {
		out.DisabledAt = timestamppb.New(*a.DisabledAt)
	}
	return out
}

// SchemaPresenceCheck verifies the admin schema (credentials table) is
// applied. Called from cmd/admin/main.go before binding the listener;
// returns a non-nil error if the table is missing — cmd/admin never runs
// migrations and refuses to start without the schema.
//
// Uses to_regclass which returns NULL (no row read error) when the table
// doesn't exist; clean way to distinguish "table missing" from generic
// pgx ErrNoRows.
func SchemaPresenceCheck(ctx context.Context, pool *pgxpool.Pool) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var present *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.credentials')::text`).Scan(&present); err != nil {
		return fmt.Errorf("admin: schema presence check (DB unreachable?): %w", err)
	}
	if present == nil || *present == "" {
		return fmt.Errorf("admin: schema presence check failed: 'credentials' table missing — run cmd/gateway to apply migrations")
	}
	return nil
}
