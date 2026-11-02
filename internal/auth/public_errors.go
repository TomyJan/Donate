package auth

import "errors"

// UserError maps controlled auth diagnostics to fixed public notices. It never
// returns provider text, URLs, identifiers or details of number verification.
// An empty code leaves ordinary session/infrastructure handling to the caller.
func UserError(err error) (code, message, callback string) {
	if errors.Is(err, ErrBanned) {
		return "ACCOUNT_BANNED", "您的账号已被封禁，如有疑问请联系站长。", "account_banned"
	}
	if errors.Is(err, ErrConflict) {
		return "IDENTITY_CONFLICT", "该登录方式已绑定账号，请检查后重试。", "identity_conflict"
	}
	var d *diagnosticError
	if !errors.As(err, &d) {
		return "", "", ""
	}
	if d.stage == "qq_avatar_fetch" || d.stage == "qq_avatar_decode" {
		if errors.Is(err, ErrUpstream) {
			return "QQ_VERIFICATION_UNAVAILABLE", "QQ 号码核验暂不可用，请稍后重试。", "provider_unavailable"
		}
		return "QQ_VERIFICATION_FAILED", "QQ 号码核验未通过，请确认号码后重试。", "verification"
	}
	switch d.reason {
	case "invalid_number_format":
		return "QQ_NUMBER_INVALID", "请输入 5–12 位且不以 0 开头的 QQ 号。", "verification"
	case "image_mismatch":
		return "QQ_VERIFICATION_FAILED", "QQ 号码核验未通过，请确认号码后重试。", "verification"
	case "attempt_limit":
		return "AUTH_ATTEMPTS_EXCEEDED", "核验次数已用完，请重新授权后再试。", "attempt_limit"
	case "flow_expired":
		return "AUTH_FLOW_EXPIRED", "授权已超时，请重新开始。", "flow_expired"
	case "invalid_cookie", "flow_not_found", "flow_consumed", "flow_already_claimed", "state_mismatch", "invalid_flow_stage", "provider_mismatch", "authorization_code_missing":
		return "AUTH_FLOW_INVALID", "授权流程已失效，请重新开始。", "flow_invalid"
	case "binding_session_mismatch":
		return "AUTH_SESSION_CHANGED", "登录账号已变化，请回到账号页面重新绑定。", "session_changed"
	case "authorization_cancelled":
		return "LOGIN_CANCELLED", "已取消授权，可重新选择登录方式。", "cancelled"
	case "provider_disabled", "client_rejected", "client_mismatch":
		return "LOGIN_CONFIGURATION_ERROR", "此登录方式暂不可用，请选择其他方式或联系站长。", "configuration"
	}
	if d.stage == "flow_decrypt" {
		return "AUTH_FLOW_INVALID", "授权流程已失效，请重新开始。", "flow_invalid"
	}
	if d.stage == "identity_bind" && errors.Is(err, ErrUnauthorized) {
		return "AUTH_SESSION_CHANGED", "登录账号已变化，请回到账号页面重新绑定。", "session_changed"
	}
	if d.stage == "flow_finish" && errors.Is(err, ErrUnauthorized) {
		return "AUTH_FLOW_INVALID", "授权流程已失效，请重新开始。", "flow_invalid"
	}
	if errors.Is(err, ErrUpstream) {
		return "LOGIN_PROVIDER_UNAVAILABLE", "登录服务暂未响应，请稍后重试或选择其他方式。", "provider_unavailable"
	}
	if errors.Is(err, ErrUnauthorized) && (d.stage == "oidc_token" || d.stage == "oidc_verify" || d.stage == "oidc_claims" || d.stage == "qq_token" || d.stage == "qq_identity" || d.stage == "qq_profile") || d.reason == "authorization_rejected" || d.reason == "token_rejected" {
		return "LOGIN_VERIFICATION_FAILED", "授权未通过验证，请重新授权或选择其他方式。", "verification"
	}
	return "", "", ""
}
