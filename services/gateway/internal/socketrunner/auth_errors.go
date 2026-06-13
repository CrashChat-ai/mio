package socketrunner

import "regexp"

// permanentAuthError matches Slack errors that signal irrecoverable credential
// loss. Retrying these storms the gateway with no chance of success, so the
// runner stops instead. Ported from goclaw internal/channels/slack/utils.go.
var permanentAuthError = regexp.MustCompile(
	`(?i)(invalid_auth|token_revoked|account_inactive|not_authed|team_not_found|missing_scope|token_expired|org_login_required)`,
)

func isPermanentAuthError(msg string) bool {
	return permanentAuthError.MatchString(msg)
}
