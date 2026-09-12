import React, { useEffect } from 'react';
import { Spin } from 'antd';
import { navigateTo } from '@/lib/navigation';
import { setToken, setRefreshToken } from '@/utils/auth';

/**
 * SSO 回调落点：后端以 URL fragment（#token=...&refresh_token=...）交接
 * 会话令牌——fragment 不进服务器日志与 Referer。读取后立即清除，
 * 再放行进入仪表板。
 */
const OIDCCallbackPage: React.FC = () => {
  useEffect(() => {
    const fragment = window.location.hash.startsWith('#')
      ? window.location.hash.slice(1)
      : window.location.hash;
    const params = new URLSearchParams(fragment);
    const token = params.get('token');
    const refreshToken = params.get('refresh_token');

    if (!token) {
      navigateTo('/login?error=oidc_failed');
      return;
    }

    setToken(token);
    if (refreshToken) setRefreshToken(refreshToken);
    // 立即抹掉地址栏中的令牌，避免浏览器历史/屏幕共享残留
    window.history.replaceState(null, '', '/dashboard');
    navigateTo('/dashboard');
  }, []);

  return (
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100vh' }}>
      <Spin size="large" tip="正在完成单点登录…">
        <div style={{ width: 200, height: 80 }} />
      </Spin>
    </div>
  );
};

export default OIDCCallbackPage;
