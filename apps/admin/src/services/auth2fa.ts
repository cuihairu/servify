import { request } from '@/lib/request';

// TOTP 两步验证端点封装。
// verify 走裸 fetch：登录页调用，验证码错误返回 401 时不触发全局
// “登录失效”跳转（那会整页刷新丢掉挑战上下文）。

export type TwoFactorChallengeResponse = {
  two_factor_required: true;
  challenge_token: string;
  expires_in: number;
};

export type TwoFactorTokenResponse = {
  token: string;
  expires_in: number;
  refresh_token?: string;
  refresh_expires_in?: number;
};

export async function verifyTwoFactorLogin(
  challengeToken: string,
  code: string,
): Promise<TwoFactorTokenResponse> {
  const resp = await fetch('/api/v1/auth/2fa/verify', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ challenge_token: challengeToken, code }),
  });
  if (!resp.ok) {
    const err = await resp.json().catch(() => null);
    throw new Error(err?.error || '验证失败，请重试');
  }
  return resp.json();
}

export type TwoFactorSetup = {
  secret: string;
  otpauth_uri: string;
};

export async function setupTwoFactor() {
  const resp = await request<{ data: TwoFactorSetup }>('/api/v1/auth/2fa/setup', {
    method: 'POST',
  });
  return resp.data;
}

export async function enableTwoFactor(secret: string, code: string) {
  const resp = await request<{ data: { recovery_codes: string[] } }>(
    '/api/v1/auth/2fa/enable',
    {
      method: 'POST',
      data: { secret, code },
    },
  );
  return resp.data.recovery_codes;
}

export async function disableTwoFactor(password: string, code: string) {
  await request('/api/v1/auth/2fa/disable', {
    method: 'POST',
    data: { password, code },
  });
}

export async function getRecoveryCodesRemaining() {
  const resp = await request<{ data: { remaining: number } }>(
    '/api/v1/auth/2fa/recovery-codes',
  );
  return resp.data.remaining;
}

export async function regenerateRecoveryCodes(code: string) {
  const resp = await request<{ data: { recovery_codes: string[] } }>(
    '/api/v1/auth/2fa/recovery-codes/regenerate',
    {
      method: 'POST',
      data: { code },
    },
  );
  return resp.data.recovery_codes;
}
