package rest

import (
	"encoding/json"
	"net/http"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"
)

func tenantToJSON(tenant *adminv1.Tenant) tenantJSON {
	if tenant == nil {
		return tenantJSON{}
	}
	return tenantJSON{
		ID:          tenant.GetId(),
		Slug:        tenant.GetSlug(),
		DisplayName: tenant.GetDisplayName(),
		Status:      tenant.GetStatus(),
		CreatedAt:   timestamp(tenant.GetCreatedAt()),
		DisabledAt:  timestamp(tenant.GetDisabledAt()),
	}
}

func accountToJSON(account *adminv1.Account) accountJSON {
	if account == nil {
		return accountJSON{}
	}
	return accountJSON{
		ID:                 account.GetId(),
		TenantID:           account.GetTenantId(),
		ChannelType:        account.GetChannelType(),
		Provider:           account.GetProvider(),
		ExternalID:         account.GetExternalId(),
		DisplayName:        account.GetDisplayName(),
		RateLimitPerSecond: account.GetRateLimitPerSecond(),
		RateLimitScope:     account.GetRateLimitScope(),
		CreatedAt:          timestamp(account.GetCreatedAt()),
		DisabledAt:         timestamp(account.GetDisabledAt()),
	}
}

func channelTypeToJSON(channel *adminv1.ChannelTypeInfo) channelTypeJSON {
	if channel == nil {
		return channelTypeJSON{}
	}
	caps := channel.GetCapabilities()
	out := channelTypeJSON{
		Slug:            channel.GetSlug(),
		Status:          channel.GetStatus(),
		AuthKind:        caps.GetAuthKind(),
		SupportsThreads: caps.GetSupportsThreads(),
		SupportsEdit:    caps.GetSupportsEdit(),
		SupportsDelete:  caps.GetSupportsDelete(),
		RateLimitScope:  caps.GetRateLimitScope(),
		RateLimitPerSec: caps.GetRateLimitPerSecond(),
		MaxTextBytes:    caps.GetMaxTextBytes(),
	}
	for _, kind := range caps.GetAllowedAttachments() {
		out.AllowedKinds = append(out.AllowedKinds, kind.String())
	}
	return out
}

func tailMessageToJSON(msg *adminv1.TailMessagesResponse) tailMessageJSON {
	if msg == nil {
		return tailMessageJSON{}
	}
	return tailMessageJSON{
		ID:             msg.GetId(),
		TenantID:       msg.GetTenantId(),
		AccountID:      msg.GetAccountId(),
		ConversationID: msg.GetConversationId(),
		ChannelType:    msg.GetChannelType(),
		SenderDisplay:  msg.GetSenderDisplay(),
		Text:           msg.GetText(),
		ReceivedAt:     timestamp(msg.GetReceivedAt()),
	}
}

func webhookInfoToJSON(info *adminv1.GetWebhookInfoResponse) webhookInfoJSON {
	if info == nil {
		return webhookInfoJSON{}
	}
	aliases := info.GetRouteAliases()
	if aliases == nil {
		aliases = []string{}
	}
	return webhookInfoJSON{
		AccountID:    info.GetAccountId(),
		ChannelType:  info.GetChannelType(),
		WebhookURL:   info.GetWebhookUrl(),
		RouteAliases: aliases,
		AuthKind:     info.GetAuthKind(),
		SetupHint:    info.GetSetupHint(),
	}
}

func consumerHealthToJSON(c *adminv1.ConsumerHealth) consumerHealthJSON {
	if c == nil {
		return consumerHealthJSON{}
	}
	return consumerHealthJSON{
		ConsumerName:  c.GetConsumerName(),
		Stream:        c.GetStream(),
		NumPending:    c.GetNumPending(),
		NumAckPending: c.GetNumAckPending(),
		LastDelivered: timestamp(c.GetLastDelivered()),
	}
}

func credentialMetadataToJSON(meta *adminv1.GetCredentialMetadataResponse) credentialMetadataJSON {
	if meta == nil {
		return credentialMetadataJSON{}
	}
	return credentialMetadataJSON{
		AccountID:     meta.GetAccountId(),
		HasCredential: meta.GetHasCredential(),
		AuthKind:      meta.GetAuthKind(),
		KeyVersion:    meta.GetKeyVersion(),
		ExpiresAt:     timestamp(meta.GetExpiresAt()),
		RotatedAt:     timestamp(meta.GetRotatedAt()),
	}
}

func timestamp(ts *timestamppb.Timestamp) string {
	if ts == nil || !ts.IsValid() {
		return ""
	}
	return ts.AsTime().UTC().Format(time.RFC3339Nano)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close() //nolint:errcheck
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return false
	}
	return true
}
