package socketrunner

import "testing"

func TestIsPermanentAuthError(t *testing.T) {
	permanent := []string{
		"invalid_auth",
		"token_revoked",
		"account_inactive",
		"not_authed",
		"team_not_found",
		"missing_scope",
		"token_expired",
		"org_login_required",
		"error: invalid_auth while connecting",
		"INVALID_AUTH",
	}
	for _, msg := range permanent {
		if !isPermanentAuthError(msg) {
			t.Errorf("isPermanentAuthError(%q) = false, want true", msg)
		}
	}

	transient := []string{
		"",
		"connection reset by peer",
		"i/o timeout",
		"rate_limited",
		"server_error",
	}
	for _, msg := range transient {
		if isPermanentAuthError(msg) {
			t.Errorf("isPermanentAuthError(%q) = true, want false", msg)
		}
	}
}
