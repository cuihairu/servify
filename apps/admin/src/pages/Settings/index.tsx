import React from 'react';
import { Result, Tabs } from 'antd';
import TwoFactorSettings from './TwoFactorSettings';

const SettingsPage: React.FC = () => {
  return (
    <div style={{ maxWidth: 760, margin: '24px auto', padding: '0 16px' }}>
      <Tabs
        items={[
          {
            key: 'personal-security',
            label: '个人安全',
            children: <TwoFactorSettings />,
          },
          {
            key: 'system',
            label: '系统设置',
            children: (
              <Result
                status="info"
                title="系统设置"
                subTitle="当前配置通过 config.yml 文件管理。如需修改配置，请编辑 config.yml 文件并重启服务。"
              />
            ),
          },
        ]}
      />
    </div>
  );
};

export default SettingsPage;
