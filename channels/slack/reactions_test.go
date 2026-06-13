package slack

import (
	"context"
	"testing"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

func reactionCmd(emoji string, removed bool) *miov1.SendCommand {
	cmd := &miov1.SendCommand{
		ConversationExternalId: "C1",
		Relation: &miov1.MessageRelation{
			Kind:             miov1.MessageRelation_KIND_REACTION,
			TargetExternalId: "C1:100.200",
			ReactionEmoji:    emoji,
		},
	}
	if removed {
		cmd.Attributes = map[string]string{attrSlackReactionRemoved: "true"}
	}
	return cmd
}

func TestSend_ReactionAdd(t *testing.T) {
	f := newFakeSlack(t)
	a := newTestAdapter()

	got, err := a.Send(context.Background(), reactionCmd(":tada:", false))
	if err != nil {
		t.Fatalf("Send reaction: %v", err)
	}
	if got != "" {
		t.Errorf("reaction Send must return empty external id, got %q", got)
	}
	form := f.lastForm["reactions.add"]
	if form == nil {
		t.Fatal("reactions.add not called")
	}
	if form["name"] != "tada" {
		t.Errorf("emoji name = %q, want tada (colons stripped)", form["name"])
	}
	if form["channel"] != "C1" || form["timestamp"] != "100.200" {
		t.Errorf("ItemRef = %q/%q, want C1/100.200", form["channel"], form["timestamp"])
	}
	if _, posted := f.lastForm["chat.postMessage"]; posted {
		t.Error("reaction must not post a message")
	}
}

func TestSend_ReactionRemove(t *testing.T) {
	f := newFakeSlack(t)
	a := newTestAdapter()

	if _, err := a.Send(context.Background(), reactionCmd("eyes", true)); err != nil {
		t.Fatalf("Send reaction remove: %v", err)
	}
	if _, ok := f.lastForm["reactions.remove"]; !ok {
		t.Fatal("reactions.remove not called for slack_reaction_removed=true")
	}
}

func TestSend_ReactionRequiresEmoji(t *testing.T) {
	newFakeSlack(t)
	a := newTestAdapter()
	if _, err := a.Send(context.Background(), reactionCmd("", false)); err == nil {
		t.Fatal("want error when reaction_emoji empty")
	}
}

func TestSend_ReactionRequiresCompositeTarget(t *testing.T) {
	newFakeSlack(t)
	a := newTestAdapter()
	cmd := reactionCmd("tada", false)
	cmd.Relation.TargetExternalId = "bare-ts"
	if _, err := a.Send(context.Background(), cmd); err == nil {
		t.Fatal("want error when target_external_id is not composite")
	}
}
