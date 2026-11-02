import { expect, it } from 'vitest';
import { APIError } from './api';
import { errorMessage } from './errors';
import { translate } from './i18n';

it.each([
  ['QQ_NUMBER_INVALID', 'qqNumberInvalidError'],
  ['QQ_VERIFICATION_FAILED', 'qqVerificationError'],
  ['QQ_VERIFICATION_UNAVAILABLE', 'qqVerificationUnavailableError'],
  ['AUTH_FLOW_EXPIRED', 'flowExpired'],
  ['AUTH_FLOW_INVALID', 'flowInvalidError'],
  ['AUTH_ATTEMPTS_EXCEEDED', 'authAttemptsError'],
  ['AUTH_SESSION_CHANGED', 'authSessionChangedError'],
  ['LOGIN_CANCELLED', 'loginCancelledError'],
  ['LOGIN_CONFIGURATION_ERROR', 'loginConfigurationError'],
  ['LOGIN_VERIFICATION_FAILED', 'loginVerificationError'],
])('classifies %s without exposing upstream details', (code, key) => {
  for (const lang of ['zh-CN', 'en'] as const) {
    const message = errorMessage(new APIError(code, 'secret upstream payload', 400), (k) =>
      translate(k, lang),
    );
    expect(message).toBe(translate(key, lang));
    expect(message).not.toBe(key);
    expect(message).not.toMatch(/secret|avatar|picture|头像/i);
  }
});
