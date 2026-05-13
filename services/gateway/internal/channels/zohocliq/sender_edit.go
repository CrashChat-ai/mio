package zohocliq

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// cliqEditRequest is the request body for PATCH /api/v2/chats/{chatid}/messages/{msgid}.
// Cliq uses PATCH for in-place edits (verified against Cliq REST API docs;
// if a live call reveals PUT is required, swap the method string here — one-line change).
type cliqEditRequest struct {
	Text string `json:"text"`
}

// Edit updates an existing Cliq message in-place.
// cmd.EditOfExternalId must be the Cliq message id returned by a prior Send call.
// cmd.ConversationExternalId must be the Cliq chat id.
//
// Uses doWithSelfHeal — a stale-token 401 transparently refreshes and retries
// once before surfacing as a terminal auth error. Symmetric with Send.
func (a *Adapter) Edit(ctx context.Context, cmd *miov1.SendCommand) error {
	convExtID := cmd.GetConversationExternalId()
	msgExtID := cmd.GetEditOfExternalId()

	if convExtID == "" {
		return fmt.Errorf("cliq edit: conversation_external_id is required")
	}
	if msgExtID == "" {
		return fmt.Errorf("cliq edit: edit_of_external_id is required (pool must resolve before calling Edit)")
	}

	body := cliqEditRequest{Text: cmd.GetText()}
	reqBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("cliq edit: marshal request: %w", err)
	}

	// Escape path segments — Cliq IDs are opaque and could theoretically
	// include URL-special chars in future API versions.
	endpoint := fmt.Sprintf("%s/api/v2/chats/%s/messages/%s",
		a.baseURL, url.PathEscape(convExtID), url.PathEscape(msgExtID))

	if _, err := a.doWithSelfHeal(ctx, http.MethodPatch, endpoint, reqBody); err != nil {
		return err
	}

	a.logger.Info("cliq: edited outbound message",
		"cmd_id", cmd.GetId(),
		"conv_external_id", convExtID,
		"cliq_msg_id", msgExtID,
	)
	return nil
}
