package slack

import (
	"context"
	"fmt"
	"testing"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

func reactionEnvelopeJSON(reaction, evType string) []byte {
	return []byte(fmt.Sprintf(`{
  "type": "event_callback",
  "team_id": "T01ABCD2EFG",
  "event_id": "Ev01REACT",
  "event": {
    "type": %q,
    "user": "U01SENDER002",
    "reaction": %q,
    "ts": "1700000400.001000",
    "item": {"type": "message", "channel": "C01PUBLIC001", "ts": "1700000000.000100"}
  }
}`, evType, reaction))
}

// Common reaction names across formats; normalize treats reaction_emoji as an
// opaque string, so the same matrix proves capture for every emoji kind.
var commonReactions = []string{
	// standard single-word
	"thumbsup", "heart", "fire", "eyes", "rocket", "joy", "tada", "100",
	"raised_hands", "pray", "clap", "white_check_mark", "x", "warning",
	"heavy_check_mark", "smile", "sob", "thinking_face",
	// aliases
	"+1", "-1",
	// skin-tone variants
	"wave::skin-tone-2", "thumbsup::skin-tone-5", "raised_hands::skin-tone-3",
	// custom / workspace emoji (arbitrary names)
	"parrot", "meow_party", "this_is_fine", "custom_logo", "shipit",
}

func TestNormalizeReaction_CommonMatrix(t *testing.T) {
	wantTarget := composite("C01PUBLIC001", "1700000000.000100")
	wantSrc := composite("C01PUBLIC001", "1700000400.001000")
	for _, emoji := range commonReactions {
		t.Run(emoji, func(t *testing.T) {
			env, err := ParseEnvelope(reactionEnvelopeJSON(emoji, "reaction_added"))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			msg, err := Normalize(env)
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			rel := msg.GetRelation()
			if rel.GetKind() != miov1.MessageRelation_KIND_REACTION {
				t.Fatalf("kind = %v, want KIND_REACTION", rel.GetKind())
			}
			if rel.GetReactionEmoji() != emoji {
				t.Errorf("reaction_emoji = %q, want %q (must round-trip opaque)", rel.GetReactionEmoji(), emoji)
			}
			if rel.GetTargetExternalId() != wantTarget {
				t.Errorf("target = %q, want %q", rel.GetTargetExternalId(), wantTarget)
			}
			if msg.GetSourceMessageId() != wantSrc {
				t.Errorf("source_message_id = %q, want %q", msg.GetSourceMessageId(), wantSrc)
			}
			if msg.GetConversationExternalId() != "C01PUBLIC001" {
				t.Errorf("conversation = %q, want item.channel", msg.GetConversationExternalId())
			}
			if msg.GetAttributes()[attrSlackReactionRemoved] != "" {
				t.Errorf("reaction_added must not carry removed attr")
			}
		})
	}
}

func TestNormalizeReaction_RemovedMatrix(t *testing.T) {
	for _, emoji := range []string{"thumbsup", "wave::skin-tone-2", "parrot", "+1"} {
		t.Run(emoji, func(t *testing.T) {
			env, err := ParseEnvelope(reactionEnvelopeJSON(emoji, "reaction_removed"))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			msg, err := Normalize(env)
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if msg.GetRelation().GetReactionEmoji() != emoji {
				t.Errorf("emoji = %q, want %q", msg.GetRelation().GetReactionEmoji(), emoji)
			}
			if msg.GetAttributes()[attrSlackReactionRemoved] != "true" {
				t.Errorf("reaction_removed must carry %s=true", attrSlackReactionRemoved)
			}
		})
	}
}

func TestSend_ReactionEmojiFormatting(t *testing.T) {
	cases := []struct{ in, want string }{
		{"tada", "tada"},
		{":tada:", "tada"},
		{"thumbsup", "thumbsup"},
		{"+1", "+1"},
		{"white_check_mark", "white_check_mark"},
		{"wave::skin-tone-2", "wave::skin-tone-2"},
		{":wave::skin-tone-2:", "wave::skin-tone-2"},
		{"parrot", "parrot"},
		{":this_is_fine:", "this_is_fine"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			f := newFakeSlack(t)
			a := newTestAdapter()
			if _, err := a.Send(context.Background(), reactionCmd(c.in, false)); err != nil {
				t.Fatalf("Send reaction %q: %v", c.in, err)
			}
			form := f.lastForm["reactions.add"]
			if form == nil {
				t.Fatal("reactions.add not called")
			}
			if form["name"] != c.want {
				t.Errorf("emoji name = %q, want %q", form["name"], c.want)
			}
		})
	}
}
