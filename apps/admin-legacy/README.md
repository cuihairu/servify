# Servify 官网

这是 Servify 智能客服系统的官方网站，展示产品功能、技术架构和发展路线图。

## 网站特点

### 设计特色
- **现代化设计** - 采用最新的 Web 设计趋势
- **响应式布局** - 适配各类屏幕尺寸
- **流畅动画** - 精心设计的交互动画

## 使用最新 SDK（重要）

本目录下的 `sdk/servify-sdk.umd.js`、`sdk/servify-sdk.esm.js`、`sdk/index.d.ts` 由 SDK 包产物同步生成，并保持提交到仓库，以满足 CI 的生成产物校验。

同步方式：
- 全量构建并同步（推荐）：
  - 在仓库根执行：`make demo-sync-sdk`
- 或手动运行脚本：
  - `./scripts/sync-sdk-to-demo.sh`

构建完成后，打开本地服务即可使用最新 SDK：
- `make run` 后访问 http://localhost:8080/
- **渐变配色** - 专业的品牌色彩方案

### 功能特性
- **导航菜单** - 固定导航栏，智能高亮
- **英雄区域** - 吸引眼球的产品介绍
- **功能展示** - 可视化展示核心功能
- **竞争对比** - 清晰的优势分析表格
- **路线图** - 时间线展示产品发展计划
- **技术栈** - 技术架构可视化展示

### 性能优化
- **CDN 加速** - 使用 Tailwind CSS 和 Font Awesome CDN
- **图片优化** - 使用 SVG 图标和优化的图片
- **代码压缩** - 生产环境代码压缩
- **加载动画** - 平滑的页面加载效果

## 文件结构

```
web/
├── index.html      # 主页面
├── style.css       # 自定义样式
├── script.js       # 交互脚本
└── README.md       # 说明文档
```

## 技术栈

- **HTML5** - 语义化标签
- **Tailwind CSS** - 实用优先的CSS框架
- **JavaScript** - 原生JS交互
- **Font Awesome** - 图标库
- **CSS Grid/Flexbox** - 现代布局

## 部署说明

### 本地开发
```bash
# 直接在浏览器中打开
open web/index.html

# 或使用简单的HTTP服务器
cd web
python -m http.server 8000
# 访问 http://localhost:8000
```

### 生产部署
```bash
# 上传到静态网站托管服务
# 如 GitHub Pages, Netlify, Vercel 等

# 或部署到自己的服务器
scp -r web/* user@server:/var/www/html/
```

## 自定义配置

### 修改品牌信息
编辑 `index.html` 中的以下内容：
- 网站标题和描述
- 联系方式
- 社交媒体链接
- GitHub 仓库地址

### 修改样式
编辑 `style.css` 中的 CSS 变量：
```css
:root {
    --primary-color: #667eea;
    --secondary-color: #764ba2;
    --accent-color: #4f46e5;
}
```

### 添加功能
编辑 `script.js` 添加新的交互功能：
- 表单提交
- 动画效果
- 数据统计
- 用户分析

## 浏览器支持

- Chrome 60+
- Firefox 55+
- Safari 12+
- Edge 79+

## 更新日志

### v1.0.0 (2024-01-15)
- 初始版本发布
- 完整的产品介绍页面
- 响应式设计
- 交互动画效果

## 贡献指南

欢迎提交 Pull Request 来改进网站：

1. Fork 本项目
2. 创建功能分支
3. 提交更改
4. 创建 Pull Request

## 许可证

MIT License - 详见 [LICENSE](../LICENSE) 文件
## 浏览器 SDK 使用

- 直接引入（UMD）：
```html
<script src="/sdk/servify-sdk.umd.js"></script>
<script>
  const client = Servify.createClient({ sessionId: 'demo_1' });
  client.on('ai', (m) => console.log('AI:', m.content));
  client.connect();
  client.sendMessage('你好');
  // 打开 /sdk-demo.html 可快速体验
</script>
```

- 打包器（ESM）：
```js
import { ServifyClient } from '/sdk/servify-sdk.esm.js';
const client = new ServifyClient({ baseUrl: 'http://localhost:8080' });
await client.connect();
client.sendMessage('help');
```
