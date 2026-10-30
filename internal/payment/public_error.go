package payment

import "errors"

// UserError exposes a small allowlist of actionable causes, never a gateway body.
func UserError(err error) (string, string) {
	var failure *gatewayFailure
	if errors.As(err, &failure) {
		if failure.status == 429 {
			return "RATE_LIMITED", "支付请求过于频繁，请稍后重试"
		}
		switch failure.reason {
		case "channel_unavailable":
			return "PAYMENT_CHANNEL_UNAVAILABLE", "该支付方式暂时无法使用，请换一种方式或稍后重试"
		case "amount_limit":
			return "PAYMENT_AMOUNT_LIMITED", "金额超出支付通道限额，请调整金额后重试"
		case "timeout":
			return "PAYMENT_TIMEOUT", "支付请求超时，请先查看订单状态，避免重复付款"
		case "order_already_paid":
			return "ORDER_ALREADY_PAID", "该订单已支付，请查看捐赠记录"
		case "response_signature":
			return "PAYMENT_VERIFY_FAILED", "支付结果暂时无法核实，请查看订单进度"
		case "request_signature", "request_timestamp", "api_not_found", "merchant_not_found", "merchant_disabled", "merchant_unapproved", "merchant_unverified", "merchant_domain", "merchant_deposit", "merchant_balance", "request_parameter":
			return "PAYMENT_CONFIGURATION_ERROR", "支付配置需要站点管理员处理，请稍后重试"
		}
	}
	if errors.Is(err, ErrProtocol) {
		return "PAYMENT_VERIFY_FAILED", "支付结果暂时无法核实，请查看订单进度"
	}
	return "PAYMENT_UNAVAILABLE", "支付服务暂未响应，可稍后在订单详情中继续支付"
}
