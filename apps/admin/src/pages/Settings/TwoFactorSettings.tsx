import React, { useCallback, useEffect, useState } from 'react';
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Input,
  Modal,
  Popconfirm,
  Space,
  Tag,
  Typography,
  message,
} from 'antd';
import {
  SafetyCertificateFilled,
  SyncOutlined,
} from '@ant-design/icons';
import { request } from '@/lib/request';
import { getErrorMessage } from '@/utils/error';
import {
  disableTwoFactor,
  enableTwoFactor,
  getRecoveryCodesRemaining,
  regenerateRecoveryCodes,
  setupTwoFactor,
  type TwoFactorSetup,
} from '@/services/auth2fa';

const { Paragraph, Text } = Typography;

const RecoveryCodesModal: React.FC<{
  codes: string[];
  onClose: () => void;
}> = ({ codes, onClose }) => (
  <Modal
    open
    title="恢复码已生成（只显示这一次）"
    okText="我已保存"
    cancelButtonProps={{ style: { display: 'none' } }}
    onOk={onClose}
    closable={false}
    maskClosable={false}
    keyboard={false}
  >
    <Alert
      type="warning"
      showIcon
      message="请将恢复码保存在安全的地方"
      description="无法登录认证器时，可用恢复码代替验证码登录。每个恢复码只能使用一次，关闭本窗口后不再显示。"
      style={{ marginBottom: 16 }}
    />
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, 1fr)', gap: 8 }}>
      {codes.map((code) => (
        <Text key={code} code copyable style={{ fontSize: 16 }}>
          {code}
        </Text>
      ))}
    </div>
  </Modal>
);

const TwoFactorSettings: React.FC = () => {
  const [enabled, setEnabled] = useState<boolean | null>(null);
  const [remaining, setRemaining] = useState<number | null>(null);

  const [setup, setSetup] = useState<TwoFactorSetup | null>(null);
  const [verifyCode, setVerifyCode] = useState('');
  const [enabling, setEnabling] = useState(false);

  const [disableOpen, setDisableOpen] = useState(false);
  const [disablePassword, setDisablePassword] = useState('');
  const [disableCode, setDisableCode] = useState('');
  const [disabling, setDisabling] = useState(false);

  const [regenOpen, setRegenOpen] = useState(false);
  const [regenCode, setRegenCode] = useState('');
  const [regenerating, setRegenerating] = useState(false);

  const [codes, setCodes] = useState<string[] | null>(null);

  const refreshStatus = useCallback(async () => {
    try {
      const me = await request<{ data: { totp_enabled?: boolean } }>('/api/v1/auth/me');
      const totpEnabled = Boolean(me.data?.totp_enabled);
      setEnabled(totpEnabled);
      if (totpEnabled) {
        setRemaining(await getRecoveryCodesRemaining());
      } else {
        setRemaining(null);
      }
    } catch (err) {
      message.error(getErrorMessage(err, '加载两步验证状态失败'));
      setEnabled(false);
    }
  }, []);

  useEffect(() => {
    void refreshStatus();
  }, [refreshStatus]);

  const startSetup = async () => {
    try {
      setSetup(await setupTwoFactor());
      setVerifyCode('');
    } catch (err) {
      message.error(getErrorMessage(err, '生成绑定密钥失败'));
    }
  };

  const confirmEnable = async () => {
    if (!setup) return;
    setEnabling(true);
    try {
      const recovery = await enableTwoFactor(setup.secret, verifyCode.trim());
      setSetup(null);
      setCodes(recovery);
      await refreshStatus();
    } catch (err) {
      message.error(getErrorMessage(err, '验证码错误，请重试'));
    } finally {
      setEnabling(false);
    }
  };

  const confirmDisable = async () => {
    setDisabling(true);
    try {
      await disableTwoFactor(disablePassword, disableCode.trim());
      setDisableOpen(false);
      setDisablePassword('');
      setDisableCode('');
      message.success('两步验证已解除');
      await refreshStatus();
    } catch (err) {
      message.error(getErrorMessage(err, '解除失败，请检查密码与验证码'));
    } finally {
      setDisabling(false);
    }
  };

  const confirmRegenerate = async () => {
    setRegenerating(true);
    try {
      const recovery = await regenerateRecoveryCodes(regenCode.trim());
      setRegenOpen(false);
      setRegenCode('');
      setCodes(recovery);
      await refreshStatus();
    } catch (err) {
      message.error(getErrorMessage(err, '验证码错误，请重试'));
    } finally {
      setRegenerating(false);
    }
  };

  if (enabled === null) {
    return <Card loading title="两步验证（TOTP）" />;
  }

  return (
    <Card
      title={
        <Space>
          <SafetyCertificateFilled style={{ color: enabled ? '#52c41a' : 'rgba(0,0,0,0.25)' }} />
          两步验证（TOTP）
          {enabled ? <Tag color="success">已启用</Tag> : <Tag>未启用</Tag>}
        </Space>
      }
    >
      {!setup && (
        <>
          <Paragraph type="secondary">
            启用后，登录需要输入认证器 App（如 Google Authenticator、1Password）中的 6 位动态验证码，
            为账号增加一层密码之外的保护。OIDC/SSO 登录不受影响。
          </Paragraph>
          {enabled ? (
            <Descriptions column={1} size="small" style={{ marginBottom: 16 }}>
              <Descriptions.Item label="剩余恢复码">
                {remaining === null ? '—' : `${remaining} 个未使用`}
              </Descriptions.Item>
            </Descriptions>
          ) : null}
          <Space wrap>
            {!enabled && (
              <Button type="primary" onClick={startSetup}>
                启用两步验证
              </Button>
            )}
            {enabled && (
              <>
                <Button icon={<SyncOutlined />} onClick={() => setRegenOpen(true)}>
                  重新生成恢复码
                </Button>
                <Popconfirm
                  title="解除两步验证会降低账号安全性，确定继续？"
                  okText="继续"
                  cancelText="取消"
                  onConfirm={() => {
                    setDisableCode('');
                    setDisablePassword('');
                    setDisableOpen(true);
                  }}
                >
                  <Button danger>解除绑定</Button>
                </Popconfirm>
              </>
            )}
          </Space>
        </>
      )}

      {setup && (
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Alert
            type="info"
            showIcon
            message="第 1 步：在认证器 App 中添加以下密钥"
            description="扫描 otpauth 链接（支持直接粘贴到多数认证器），或手动输入密钥文本。"
          />
          <Space direction="vertical" size="small" style={{ width: '100%' }}>
            <Text type="secondary">otpauth 链接</Text>
            <Paragraph code copyable style={{ marginBottom: 0, wordBreak: 'break-all' }}>
              {setup.otpauth_uri}
            </Paragraph>
            <Text type="secondary">密钥</Text>
            <Paragraph code copyable style={{ marginBottom: 0 }}>
              {setup.secret}
            </Paragraph>
          </Space>
          <Alert type="info" showIcon message="第 2 步：输入 App 显示的 6 位验证码确认绑定" />
          <Space wrap>
            <Input
              style={{ width: 160 }}
              placeholder="6 位验证码"
              maxLength={6}
              value={verifyCode}
              onChange={(e) => setVerifyCode(e.target.value.replace(/\D/g, ''))}
              onPressEnter={confirmEnable}
            />
            <Button type="primary" loading={enabling} disabled={verifyCode.length !== 6} onClick={confirmEnable}>
              确认启用
            </Button>
            <Button
              onClick={() => setSetup(null)}
              disabled={enabling}
            >
              取消
            </Button>
          </Space>
        </Space>
      )}

      <Modal
        open={disableOpen}
        title="解除两步验证"
        okText="解除"
        okButtonProps={{ danger: true, loading: disabling }}
        cancelText="取消"
        onCancel={() => setDisableOpen(false)}
        onOk={confirmDisable}
      >
        <Space direction="vertical" style={{ width: '100%' }} size="middle">
          <Alert type="warning" showIcon message="解绑后登录只需密码；剩余恢复码将全部作废。" />
          <Text type="secondary">账号密码（SSO 账号可留空）</Text>
          <Input.Password
            value={disablePassword}
            onChange={(e) => setDisablePassword(e.target.value)}
            placeholder="当前密码"
            autoComplete="current-password"
          />
          <Text type="secondary">认证器验证码或恢复码</Text>
          <Input
            value={disableCode}
            onChange={(e) => setDisableCode(e.target.value)}
            placeholder="6 位验证码 / 8 位恢复码"
            maxLength={9}
          />
        </Space>
      </Modal>

      <Modal
        open={regenOpen}
        title="重新生成恢复码"
        okText="生成"
        okButtonProps={{ loading: regenerating }}
        cancelText="取消"
        onCancel={() => setRegenOpen(false)}
        onOk={confirmRegenerate}
      >
        <Space direction="vertical" style={{ width: '100%' }} size="middle">
          <Alert
            type="warning"
            showIcon
            message="生成新恢复码后，当前所有未使用的恢复码立即作废。"
          />
          <Input
            value={regenCode}
            onChange={(e) => setRegenCode(e.target.value)}
            placeholder="输入当前 6 位验证码"
            maxLength={6}
          />
        </Space>
      </Modal>

      {codes && <RecoveryCodesModal codes={codes} onClose={() => setCodes(null)} />}
    </Card>
  );
};

export default TwoFactorSettings;
