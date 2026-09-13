import React, { useEffect, useState } from 'react';
import { LoginForm, ProFormText } from '@ant-design/pro-components';
import {
  LockOutlined,
  SafetyCertificateOutlined,
  SafetyOutlined,
  UserOutlined,
} from '@ant-design/icons';
import { Alert, Button, message, Space } from 'antd';
import { navigateTo } from '@/lib/navigation';
import { setToken, setRefreshToken, parseJwtPayload, setUserInfo } from '@/utils/auth';
import { verifyTwoFactorLogin, type TwoFactorChallengeResponse } from '@/services/auth2fa';

/** SSO 失败码 → 用户可读文案（与后端 redirectOIDCError 的 code 对齐） */
const OIDC_ERROR_MESSAGES: Record<string, string> = {
  oidc_disabled: '单点登录未启用，请使用账号密码登录',
  oidc_state: '登录会话已失效，请重新发起单点登录',
  oidc_failed: '单点登录失败，请稍后重试',
  oidc_login_denied: '当前账号不允许通过单点登录进入',
};

const LoginPage: React.FC = () => {
  const [oidcEnabled, setOidcEnabled] = useState(false);
  const [oidcIssuerHost, setOidcIssuerHost] = useState('');
  // 两步验证挑战态：非空表示第一步密码已通过，等待输入认证器验证码
  const [challenge, setChallenge] = useState<TwoFactorChallengeResponse | null>(null);
  const [verifying, setVerifying] = useState(false);

  useEffect(() => {
    // SSO 失败重定向回来时带上 ?error=<code>
    const params = new URLSearchParams(window.location.search);
    const errCode = params.get('error');
    if (errCode) {
      message.error(OIDC_ERROR_MESSAGES[errCode] || '单点登录失败，请重试');
      window.history.replaceState(null, '', '/login');
    }

    fetch('/api/v1/auth/oidc/status')
      .then((resp) => (resp.ok ? resp.json() : null))
      .then((data) => {
        if (data?.enabled) {
          setOidcEnabled(true);
          if (data.issuer_host) setOidcIssuerHost(data.issuer_host);
        }
      })
      .catch(() => {
        // 状态接口不可达时静默降级为纯密码登录
      });
  }, []);

  const handleSubmit = async (values: { username: string; password: string }) => {
    try {
      const resp = await fetch('/api/v1/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(values),
      });

      if (!resp.ok) {
        const err = await resp.json().catch(() => ({ message: '登录失败' }));
        message.error(err.message || err.error || '登录失败');
        return;
      }

      const data = await resp.json();

      // 两步验证：密码已通过，进入验证码第二步
      if (data.two_factor_required) {
        setChallenge({
          two_factor_required: true,
          challenge_token: data.challenge_token,
          expires_in: data.expires_in,
        });
        message.info('请输入认证器验证码完成登录');
        return;
      }

      const token = data.token || data.data?.token;
      if (!token) {
        message.error('服务端未返回有效 Token');
        return;
      }

      applyTokens(token, data.refresh_token || data.data?.refresh_token);
      message.success('登录成功');
      navigateTo('/dashboard');
    } catch {
      message.error('网络错误，请检查后端服务是否启动');
    }
  };

  const handleVerify = async (code: string) => {
    if (!challenge) return false;
    setVerifying(true);
    try {
      const result = await verifyTwoFactorLogin(challenge.challenge_token, code);
      if (!result.token) {
        message.error('服务端未返回有效 Token');
        return false;
      }
      applyTokens(result.token, result.refresh_token);
      message.success('登录成功');
      navigateTo('/dashboard');
      return true;
    } catch (err) {
      message.error(err instanceof Error ? err.message : '验证失败，请重试');
      return false;
    } finally {
      setVerifying(false);
    }
  };

  const startSSO = () => {
    window.location.href = '/api/v1/auth/oidc/start';
  };

  if (challenge) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100vh' }}>
        <LoginForm
          title="两步验证"
          subTitle="请输入认证器 App 中的 6 位验证码（或 8 位恢复码）"
          onFinish={async (values: { code?: string }) => {
            const code = (values.code || '').trim();
            if (!code) {
              message.error('请输入验证码');
              return;
            }
            await handleVerify(code);
          }}
        >
          <Alert
            type="info"
            showIcon
            message="验证码每 30 秒刷新一次；恢复码验证后即失效"
            style={{ marginBottom: 16 }}
          />
          <ProFormText
            name="code"
            fieldProps={{
              size: 'large',
              prefix: <SafetyOutlined />,
              maxLength: 9,
              autoComplete: 'one-time-code',
              autoFocus: true,
              disabled: verifying,
            }}
            placeholder="验证码 / 恢复码"
            rules={[{ required: true, message: '请输入验证码' }]}
          />
          <Button
            block
            type="link"
            onClick={() => setChallenge(null)}
            disabled={verifying}
          >
            返回重新登录
          </Button>
        </LoginForm>
      </div>
    );
  }

  return (
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100vh' }}>
      <LoginForm
        title="Servify"
        subTitle="智能客服管理后台"
        onFinish={handleSubmit}
      >
        <ProFormText
          name="username"
          fieldProps={{ size: 'large', prefix: <UserOutlined /> }}
          placeholder="用户名"
          rules={[{ required: true, message: '请输入用户名' }]}
        />
        <ProFormText.Password
          name="password"
          fieldProps={{ size: 'large', prefix: <LockOutlined /> }}
          placeholder="密码"
          rules={[{ required: true, message: '请输入密码' }]}
        />
        {oidcEnabled && (
          <Space direction="vertical" style={{ width: '100%', paddingTop: 8 }}>
            <div style={{ textAlign: 'center', color: 'rgba(0,0,0,0.45)', fontSize: 12 }}>或</div>
            <Button
              block
              size="large"
              icon={<SafetyCertificateOutlined />}
              onClick={startSSO}
            >
              使用 {oidcIssuerHost || '企业'} SSO 登录
            </Button>
          </Space>
        )}
      </LoginForm>
    </div>
  );
};

function applyTokens(token: string, refreshToken?: string) {
  setToken(token);
  if (refreshToken) setRefreshToken(refreshToken);
  const user = parseJwtPayload(token);
  if (user) setUserInfo(user);
}

export default LoginPage;
