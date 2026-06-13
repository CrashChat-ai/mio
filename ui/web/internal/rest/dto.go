package rest

type tenantJSON struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Status      string `json:"status"`
	CreatedAt   string `json:"createdAt"`
	DisabledAt  string `json:"disabledAt,omitempty"`
}

type accountJSON struct {
	ID                 string `json:"id"`
	TenantID           string `json:"tenantId"`
	ChannelType        string `json:"channelType"`
	Provider           string `json:"provider"`
	ExternalID         string `json:"externalId"`
	DisplayName        string `json:"displayName"`
	RateLimitPerSecond int32  `json:"rateLimitPerSecond"`
	RateLimitScope     string `json:"rateLimitScope"`
	CreatedAt          string `json:"createdAt"`
	DisabledAt         string `json:"disabledAt,omitempty"`
}

type channelTypeJSON struct {
	Slug            string   `json:"slug"`
	Status          string   `json:"status"`
	AuthKind        string   `json:"authKind"`
	SupportsThreads bool     `json:"supportsThreads"`
	SupportsEdit    bool     `json:"supportsEdit"`
	SupportsDelete  bool     `json:"supportsDelete"`
	AllowedKinds    []string `json:"allowedAttachmentKinds"`
	RateLimitScope  string   `json:"rateLimitScope"`
	RateLimitPerSec int32    `json:"rateLimitPerSecond"`
	MaxTextBytes    int32    `json:"maxTextBytes"`
}

type tailMessageJSON struct {
	ID             string `json:"id"`
	TenantID       string `json:"tenantId"`
	AccountID      string `json:"accountId"`
	ConversationID string `json:"conversationId"`
	ChannelType    string `json:"channelType"`
	SenderDisplay  string `json:"senderDisplay"`
	Text           string `json:"text"`
	ReceivedAt     string `json:"receivedAt"`
}

type credentialMetadataJSON struct {
	AccountID     string `json:"accountId"`
	HasCredential bool   `json:"hasCredential"`
	AuthKind      string `json:"authKind,omitempty"`
	KeyVersion    int32  `json:"keyVersion,omitempty"`
	ExpiresAt     string `json:"expiresAt,omitempty"`
	RotatedAt     string `json:"rotatedAt,omitempty"`
}

type webhookInfoJSON struct {
	AccountID    string   `json:"accountId"`
	ChannelType  string   `json:"channelType"`
	WebhookURL   string   `json:"webhookUrl"`
	RouteAliases []string `json:"routeAliases"`
	AuthKind     string   `json:"authKind"`
	SetupHint    string   `json:"setupHint"`
}

type consumerHealthJSON struct {
	ConsumerName  string `json:"consumerName"`
	Stream        string `json:"stream"`
	NumPending    uint64 `json:"numPending"`
	NumAckPending uint64 `json:"numAckPending"`
	LastDelivered string `json:"lastDelivered,omitempty"`
}

type auditEventJSON struct {
	OperatorEmail string `json:"operatorEmail"`
	OperatorRole  string `json:"operatorRole"`
	Action        string `json:"action"`
	TargetType    string `json:"targetType"`
	TargetID      string `json:"targetId"`
	Result        string `json:"result"`
	Error         string `json:"error,omitempty"`
	CreatedAt     string `json:"createdAt"`
}
