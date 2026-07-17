package discord

const (
	channelType = "discord"

	attrDiscordMessageID       = "discord_message_id"
	attrDiscordChannelID       = "discord_channel_id"
	attrDiscordGuildID         = "discord_guild_id"
	attrDiscordReactionRemoved = "discord_reaction_removed"
	// attrDiscordReplyTo lets a producer pin a reply to a specific message id
	// directly, bypassing the composite split (mirror of slack_thread_ts).
	attrDiscordReplyTo = "discord_reply_to"
)
