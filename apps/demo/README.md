# Servify Demo 页面

这是用于展示 Servify 客服组件集成的模拟电商网站演示页面。

## 用途

- 销售演示：向潜在客户展示客服系统如何嵌入到实际网站中
- 功能验证：测试 SDK 与服务端的集成
- UI/UX 演示：展示聊天挂件、消息流、远程协助等用户界面

## 文件说明

- `index.html` - 单页面模拟电商网站，内嵌客服组件
- 无需构建，直接在浏览器中打开即可查看

## 本地运行

```bash
# 启动服务端
make run

# 在浏览器中打开
open apps/demo/index.html
# 或访问 http://localhost:8080/demo/（如果服务端配置了静态文件服务）
```

## 与其他目录的关系

- `apps/admin` - 正式的管理后台（UmiJS + Ant Design Pro）
- `apps/demo-sdk` - SDK 预构建产物与集成示例
- `sdk/` - SDK 源码（TypeScript）
