package payment

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// gatewayFailure contains only controlled diagnostics, never upstream response
// text, request URLs (V1 includes the key), signatures or payment redirects.
type gatewayFailure struct {
	operation, stage, code, reason string
	status                         int
	base                           error
	wait                           time.Duration
}

func (e *gatewayFailure) Error() string {
	return fmt.Sprintf("易支付 %s: stage=%s http_status=%d code=%s reason=%s", e.operation, e.stage, e.status, e.code, e.reason)
}
func (e *gatewayFailure) Unwrap() error             { return e.base }
func (e *gatewayFailure) RetryAfter() time.Duration { return e.wait }

// Diagnostic is safe for logs even if an arbitrary HTTP error embeds secrets.
func Diagnostic(err error) string {
	var failure *gatewayFailure
	if errors.As(err, &failure) {
		return failure.Error()
	}
	if errors.Is(err, ErrProtocol) {
		return "payment_protocol_invalid"
	}
	if errors.Is(err, ErrUnavailable) {
		return "payment_unavailable"
	}
	return transportReason(err)
}

func transportReason(err error) string {
	var timeout net.Error
	var dns *net.DNSError
	var certificate *tls.CertificateVerificationError
	var unknownCA x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalidCertificate x509.CertificateInvalidError
	var operation *net.OpError
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &timeout) && timeout.Timeout():
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.As(err, &dns):
		return "dns"
	case errors.As(err, &certificate), errors.As(err, &unknownCA), errors.As(err, &hostname), errors.As(err, &invalidCertificate):
		return "tls_certificate"
	case errors.As(err, &operation):
		return "connection"
	default:
		return "transport_unknown"
	}
}

// Error responses may be unsigned. These hints explain rejection but cannot
// prove an order unpaid or authorize any change to its payment state.
func rejectionReason(message string) string {
	for _, item := range []struct{ text, reason string }{
		{"签名", "request_signature"}, {"时间戳", "request_timestamp"},
		{"接口方法不存在", "api_not_found"}, {"URL Error", "api_not_found"},
		{"商户不存在", "merchant_not_found"}, {"封禁", "merchant_disabled"},
		{"未通过审核", "merchant_unapproved"}, {"未完成实名认证", "merchant_unverified"},
		{"域名", "merchant_domain"}, {"保证金", "merchant_deposit"},
		{"余额不足", "merchant_balance"}, {"限额", "amount_limit"},
		{"最大支付金额", "amount_limit"}, {"最小支付金额", "amount_limit"},
		{"安全验证", "security_verification"}, {"订单号不存在", "order_not_found"},
		{"当前订单不存在", "order_not_found"}, {"已完成支付", "order_already_paid"},
		{"支付参数有变化", "order_parameters_changed"},
		{"支付通道", "channel_unavailable"}, {"支付方式", "channel_unavailable"},
		{"不能为空", "request_parameter"}, {"不合法", "request_parameter"},
	} {
		if strings.Contains(message, item.text) {
			return item.reason
		}
	}
	return "upstream_rejected"
}

func diagnosticCode(raw string) string {
	if raw == "" {
		return "missing"
	}
	if len(raw) <= 12 {
		if _, err := strconv.ParseInt(raw, 10, 32); err == nil {
			return raw
		}
	}
	return "invalid"
}
