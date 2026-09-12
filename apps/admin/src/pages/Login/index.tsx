import React, { useEffect, useState } from 'react';
import { LoginForm, ProFormText } from '@ant-design/pro-components';
import { LockOutlined, SafetyCertificateOutlined, UserOutlined } from '@ant-design/icons';
import { Button, message, Space } from 'antd';
import { navigateTo } from '@/lib/navigation';
import { setToken, setRefreshToken, parseJwtPayload, setUserInfo } from '@/utils/auth';

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
        message.error(err.message || '登录失败');
        return;
      }

      const data = await resp.json();
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

  const startSSO = () => {
    window.location.href = '/api/v1/auth/oidc/start';
  };

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
