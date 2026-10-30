import { APIError } from './api';

const messages: Record<string, string> = {
  OFFLINE: 'offlineError',
  NETWORK_ERROR: 'connectionError',
  REQUEST_TIMEOUT: 'timeoutError',
  INVALID_RESPONSE: 'invalidResponseError',
  SERVER_UNAVAILABLE: 'serverBusyError',
  INTERNAL_ERROR: 'requestFailedError',
  UNAUTHORIZED: 'unauthorized',
  FORBIDDEN: 'forbidden',
  ACCOUNT_BANNED: 'banned',
  VALIDATION_FAILED: 'checkInputError',
  RATE_LIMITED: 'rateLimitedError',
  NOT_FOUND: 'recordMissingError',
  CSRF_FAILED: 'sessionChangedError',
  IDENTITY_CONFLICT: 'identityConflictError',
  IDEMPOTENCY_CONFLICT: 'changedRequestError',
  ORDER_NOT_PAYABLE: 'orderNotPayableError',
  ORDER_ALREADY_PAID: 'alreadyPaidError',
  PAYMENT_MISMATCH: 'paymentVerificationError',
  PAYMENT_VERIFY_FAILED: 'paymentVerificationError',
  PAYMENT_UNAVAILABLE: 'paymentUnavailableError',
  PAYMENT_TIMEOUT: 'paymentTimeoutError',
  PAYMENT_CHANNEL_UNAVAILABLE: 'paymentChannelError',
  PAYMENT_AMOUNT_LIMITED: 'paymentAmountError',
  PAYMENT_CONFIGURATION_ERROR: 'paymentConfigurationError',
  PAYMENT_RETURN_MISSING: 'paymentReturnMissing',
  LOGIN_PROVIDER_UNAVAILABLE: 'loginProviderError',
  UPSTREAM_UNAVAILABLE: 'upstreamError',
  SELF_BAN_FORBIDDEN: 'forbidden',
  LAST_IDENTITY_FORBIDDEN: 'lastIdentityHint',
};
export function errorMessage(error: unknown, t: (key: string) => string) {
  if (error instanceof APIError) return t(messages[error.code] ?? 'requestFailedError');
  return t(navigator.onLine ? 'requestFailedError' : 'offlineError');
}
