# 同类在线客服产品调研:功能、界面与交互

调研时间:2026-10-08。样本 12 家,国际 8 家(Intercom、Zendesk、Crisp、tawk.to、Tidio、LiveChat、HubSpot、Drift),国内 4 家(美洽、智齿科技、网易七鱼、环信)。功能与价格以各家官网、官方帮助中心当天页面为准,截图均为当天用浏览器实地拍摄,存放在 `docs/research/assets/`。

---

## 一、功能清单

图例:✔ 有;◐ 有但分档/需付费;— 未在采集页面见到。

| 产品 | 聊天窗(访客端) | 坐席工作台 | 工单 | 机器人/AI | 多渠道 | 主题定制 | 通知/未读 | 数据看板 |
|---|---|---|---|---|---|---|---|---|
| Intercom | ✔ Messenger | ✔ 收件箱 | ✔ | ✔ Fin($0.99/次解决) | ◐ WhatsApp/SMS/电话按量计费 | ✔ Launcher+Home 可配置 | ✔ | ✔ 预置报表,实时看板在 Expert |
| Zendesk | ✔ Web Widget | ✔ | ✔ 核心能力 | ✔ AI Agents | ✔ | ✔ 位置/圆角/三色/Logo | ✔ 声音提醒 | ✔ |
| Crisp | ✔ | ✔ 共享收件箱 | ◐ Plus 档 | ◐ 档内含 AI Chatbot | ◐ Essentials 起全渠道 | ✔ | ✔ Push | ◐ Plus 档高级分析 |
| tawk.to | ✔ | ✔ | ✔ | ✔ Apollo+AI Assist | ✔ | ✔ 颜色/位置/问候语 | ✔ Push+桌面/移动 App | ✔ |
| Tidio | ✔ | ✔ | ✔ | ✔ Lyro | ✔ WhatsApp/Instagram/Messenger | ✔ | ✔ | ✔ 统一报表 |
| LiveChat | ✔ | ✔ Inbox | — | ✔ AI Agent+AI 工具组 | ◐ | ✔ Live Editor | ✔ | ✔ Reports |
| HubSpot | ✔ 免费 | ✔ 会话收件箱 | ✔ | ✔ 免费 Chatbot Builder | ◐ | ✔ | ✔ Slack+移动端 | ✔ |
| Drift | ✔(已退役) | ✔ | — | ✔ 对话式营销起家 | ◐ | ✔ | ✔ | ✔ |
| 美洽 | ✔ | ✔ | ◐ | ✔ 大模型获客机器人 | ✔ 网站/公众号/抖音/小程序/APP | ✔ widget 高度自定义、iframe 嵌入 | ✔ 多方式提醒 | ✔ 数据看板 |
| 智齿科技 | ✔ | ✔ | ✔ 工单渠道 | ✔ 对接 OpenAI/DeepSeek/通义千问/文心一言/Claude | ✔ 含 WhatsApp | ◐ | ✔ | ✔ |
| 网易七鱼 | ✔ | ✔ | ✔ 含邮件工单、SLA | ✔ 物流/退款智能体 | ✔ | ◐ | ✔ 消息提醒 | ✔ 自定义报表+数据大屏 |
| 环信 | ✔ | ✔ 效率工作台 | ✔ 智慧工单 | ✔ 7×24 机器人(欢迎语/默认/重复/超时/转人工可配) | ✔ 在线/小程序/APP/视频/呼叫中心 | ◐ | ✔ | ✔ 数据大屏+智能质检 |

几条值得单独记下的事实:

- **工单在国内外的定位不同。** Zendesk 把工单当主干,在线聊天只是入口之一;Crisp 把工单放在最贵的 Plus 档($295/月)才给;七鱼和智齿把工单做成"一二线协同"的内部流转工具,和 SLA、满意度评价绑在一起([七鱼工单页](https://b.163.com/home/qiyu/worksheet))。
- **AI 报价方式已经分化。** Intercom 座席价 $29/$85/$132(年付)另收 Fin $0.99/次解决,官方自己的口径是"AI 用量大时这部分会超过座席费"([定价指南](https://www.intercom.com/learning-center/intercom-pricing));Tidio 按"可计费会话数"分档,Free 50 次、Starter $24.17/月起([定价页](https://www.tidio.com/pricing/));Zendesk 是座席价打包 AI,$19/$55/$115 每坐席每月(年付)([定价页](https://www.zendesk.com/pricing/))。
- **tawk.to 的免费不是补贴,是商业模式。** 坐席数、会话数、网站数都不限,收入来自按"站点"卖的附加包(去广告标识 $29/月起的说法来自第三方评测,官方口径见 [why-free](https://www.tawk.to/why-free/))。
- **Drift 已经退场。** drift.com 首页现在直接写"We've transitioned from Drift to 1mind",Salesloft 在 2026-03-06 宣布逐步关停并把客户导向 1mind([drift.com](https://www.drift.com/))。做获客型聊天机器人的样本少了一家,留着它是因为它证明了"只做售前对话"这条线撑不住独立产品。
- **国内四家的共同结构**是"在线客服 + 工单 + 机器人 + 呼叫中心"四件套,再各自加一块特色:七鱼加质检和报表大屏,环信加视频客服和小程序,智齿加出海和 WhatsApp,美洽把叙事压在获客上(官网首页通篇讲留资卡、名片卡、线索,产品介绍写着"3 分钟完成网站代码部署")([meiqia.com](https://www.meiqia.com/))。

---

## 二、界面截图

全部为 2026-10-08 浏览器实拍,文件在 `docs/research/assets/`。

| 截图 | 出处 URL |
|---|---|
| `intercom-live-chat.png` | https://www.intercom.com/live-chat |
| `intercom-messenger-docs.png` | https://www.intercom.com/help/en/articles/6612589-set-up-and-customize-the-messenger |
| `intercom-messenger-faqs.png` | https://www.intercom.com/help/en/articles/6612597-messenger-faqs |
| `zendesk-messaging.png` | https://www.zendesk.co.jp/service/messaging/(zendesk.com 按访问者 IP 跳日文站,原样保留) |
| `zendesk-widget-docs.png` | https://developer.zendesk.com/documentation/zendesk-web-widget-sdks/ |
| `crisp-pricing.png` | https://crisp.chat/en/pricing/ |
| `drift-home.png` | https://www.drift.com/ |
| `tawk-live-chat.png` | https://www.tawk.to/products/live-chat/ |
| `tidio-live-chat.png` | https://www.tidio.com/live-chat/ |
| `livechat-features.png` | https://www.livechat.com/features/ |
| `hubspot-live-chat.png` | https://www.hubspot.com/products/service/live-chat |
| `meiqia-home.png` | https://www.meiqia.com/ |
| `zhichi-livechat.png` | https://www.zhichi.com/livechat/ |
| `qiyu-online-kefu.png` | https://b.163.com/home/qiyu/onlinekefu |
| `qiyu-worksheet.png` | https://b.163.com/home/qiyu/worksheet |
| `easemob-cs.png` | https://www.easemob.com/product/cs |

看得到的界面规律:

- 官网产品页普遍放一张"聊天窗 + 坐席台"的并排图,访客端在右下、坐席端占大面积(HubSpot、七鱼、智齿都是这个构图)。真正把配置界面拍出来的是 Intercom 的 Messenger 设置文档和 Zendesk 的开发者文档,配置项分 Content/Appearance/Install/Security 四类。
- 价格页的结构高度趋同:左侧档位卡、右侧功能对勾表。Crisp 用一张四列表格把"聊天窗、收件箱、移动 App、SDK、Push"放在免费档,把"工单、白标、高级分析"压到 Plus。

---

## 三、人机交互要点

### 3.1 嵌入方式

三种深度,对应三种集成成本:

1. **一段 `<script>`。** tawk.to 的安装文档就是一个 async 脚本贴到页面里,另配 Shopify/WordPress/Wix 等 50+ 平台的一键安装器([live-chat 页](https://www.tawk.to/products/live-chat/));Tidio 官方文档同样要求脚本放在 `</body>` 闭合标签之前,并给出可直接复制的完整片段([帮助文档](https://help.tidio.com/hc/en-us/articles/5464289229340-Opening-the-widget-automatically));美洽官网写"3 分钟完成网站代码部署"([meiqia.com](https://www.meiqia.com/))。
2. **客户端 JS API。** Zendesk 的 Web Widget API 允许在具体页面上改 widget 的设置和显示方式,不改后台配置([开发者文档](https://developer.zendesk.com/documentation/zendesk-web-widget-sdks/));tawk.to 用 `customStyle` 方法做像素级偏移,并且要在嵌入脚本之前定义才生效([位置文档](https://help.tawk.to/article/changing-the-widget-position));Tidio 暴露 `window.tidioChatApi`,`open()` 就能远程把窗口打开。
3. **SDK 与 iframe。** Intercom 的 Messenger 在移动端要装 SDK(iOS v15.2.0、Android、React Native、Cordova 各自版本号不同)[Messenger FAQs];Zendesk 除 Web Widget 外还有 Android/iOS/Unity SDK;美洽官网把"widget 高度自定义"和"iframe 嵌入"列为开放能力。

### 3.2 打开动线

- 默认动线都是"收起成气泡 → 点击展开 → 首屏是 Home 或问候语"。Intercom 把这一层拆成两个概念:Launcher(气泡)和 Messenger Home(首次点开的空间),Home、Messages、Tickets、Help、News、Tasks 六个空间可拖拽排序,Messages 不可关闭([Messenger 设置文档](https://www.intercom.com/help/en/articles/6612589-set-up-and-customize-the-messenger))。
- 主动打开是获客型产品的标配:tawk.to 的触发器按 URL、行为、来源页发出欢迎语([live-chat 页](https://www.tawk.to/products/live-chat/));Tidio 的做法最直白,官方给一段"进站即开窗"的脚本,还分桌面版和移动版两个变体;LiveChat 的 Campaigns 和美洽的"按停留时长/关键页面主动邀请"是同一类能力。
- SPA 场景有个共同坑:Intercom 要求每次路由变化调 `Intercom("update")`,否则消息不会自己冒出来([Messenger FAQs](https://www.intercom.com/help/en/articles/6612597-messenger-faqs))。

### 3.3 气泡样式

可配置项已经收敛成一套事实标准:

- **位置**:tawk.to 提供桌面端 6 个预设位(上/中/下 × 左/右),后台点选不用写代码,另有代码级微调([位置文档](https://help.tawk.to/article/changing-the-widget-position));Zendesk 只给左下/右下两选,配默认 16px 边距偏移。
- **颜色与形状**:Zendesk 的 Style 页能设主色(气泡与顶栏)、消息色、操作色,圆角 0–20px 滑杆,外加 Logo、标题、描述、附件开关、声音提醒([外观配置文档](https://support.zendesk.com/hc/en-us/articles/4500747797914-Configuring-the-name-and-appearance-of-the-Web-Widget))。
- **改法**:LiveChat 的 Live Editor 直接在真实网站上改,右侧预览分桌面/移动两个视图,分"收起态"和"展开态"两套主题,不用写 CSS([定制文档](https://www.livechat.com/help/customize-your-chat/))。

### 3.4 移动端适配

- 桌面的 6 个位置在移动端不通用:tawk.to 的位置说明写明预设位只针对台式和笔记本,移动端由 widget 自动重新排位以避开站点导航([位置文档](https://help.tawk.to/article/changing-the-widget-position))。
- 有产品干脆允许对移动端关掉网页 widget,改走 App:LiveChat 的配置器可以按访客是否来自移动设备禁用 widget([定制文档](https://www.livechat.com/help/customize-your-chat/));tawk.to 用原生 iOS/Android App + Push 承接离线会话([live-chat 页](https://www.tawk.to/products/live-chat/))。
- 移动端聊天窗基本都是占满视口的整页形态,桌面才是右下角的小卡片;国内四家则普遍是"网页 widget + 移动工作台 App"两条线并行,坐席侧和访客侧分开适配。

---

## 四、对 servify 的取舍建议

1. **先做一套脚本级嵌入,再留 JS API。** 12 家里 9 家的入门动作就是一段脚本,差异全在 API 深度。tawk.to 的 `customStyle` 和 Zendesk 的按页配置说明:后台配置覆盖 80% 场景,剩下 20% 要能在页面里改位置、颜色、开合状态,且要写清"代码级覆盖后台级"的优先级。
2. **气泡配置项抄事实标准即可,别自造。** 位置(6 预设或左下/右下)、主色/消息色/操作色、圆角、问候语、收起态与展开态两套主题、桌面与移动双预览。LiveChat 和 Zendesk 的文档基本就是这张清单。
3. **移动端明确三选一**:占满视口展开、关掉 widget 走 App、自动重新定位。tawk.to 的做法(自动避让)成本最低,适合 servify 的第一版。
4. **AI 计价别急着按次结算。** Intercom 的 $0.99/次解决模式对用量大的客户是不可预测成本,连它自己的定价指南都承认会超过座席费;座席价打包 AI(Zendesk)或按会话数分档(Tidio)对小客户更好算账。
5. **工单先做"会话转工单 + 跨部门流转 + SLA"三件。** 七鱼的工单页给的最小闭环是:多方式创建(访客/表格/邮件/接口)、关联与预设回复、满意度评价([七鱼工单页](https://b.163.com/home/qiyu/worksheet))。
6. **Drift 的结局提示一条产品边界:** 只覆盖售前获客、不覆盖售后的聊天机器人,单独卖很难活;能长期收费的样本(Intercom、Zendesk、智齿、七鱼)都是"获客 + 服务 + 工单"的完整闭环。

---

## 五、来源

**国际**

- Intercom 定价与功能:[intercom-pricing](https://www.intercom.com/learning-center/intercom-pricing)、[Messenger 设置](https://www.intercom.com/help/en/articles/6612589-set-up-and-customize-the-messenger)、[Messenger FAQs](https://www.intercom.com/help/en/articles/6612597-messenger-faqs)、[live-chat 产品页](https://www.intercom.com/live-chat)
- Zendesk:[定价](https://www.zendesk.com/pricing/)、[Web Widget 与 SDK](https://developer.zendesk.com/documentation/zendesk-web-widget-sdks/)、[名称与外观配置](https://support.zendesk.com/hc/en-us/articles/4500747797914-Configuring-the-name-and-appearance-of-the-Web-Widget)、[messaging 产品页](https://www.zendesk.com/service/messaging/)
- Crisp:[定价与功能对比表](https://crisp.chat/en/pricing/)
- tawk.to:[live-chat 产品页](https://www.tawk.to/products/live-chat/)、[首页](https://www.tawk.to/)、[widget 位置文档](https://help.tawk.to/article/changing-the-widget-position)、[why-free](https://www.tawk.to/why-free/)
- Tidio:[功能页](https://www.tidio.com/features/)、[定价页](https://www.tidio.com/pricing/)、[自动开窗文档](https://help.tidio.com/hc/en-us/articles/5464289229340-Opening-the-widget-automatically)
- LiveChat:[功能页](https://www.livechat.com/features/)、[widget 定制](https://www.livechat.com/help/customize-your-chat/)、[FAQ(价格档)](https://www.livechat.com/help/frequently-asked-questions/)
- HubSpot:[免费 live chat 产品页](https://www.hubspot.com/products/service/live-chat)
- Drift:[drift.com](https://www.drift.com/)(已并入 Salesloft/1mind)

**国内**

- 美洽:[meiqia.com](https://www.meiqia.com/)
- 智齿科技:[zhichi.com](https://www.zhichi.com/)、[在线客服页](https://www.zhichi.com/livechat/)
- 网易七鱼:[在线客服](https://b.163.com/home/qiyu/onlinekefu)、[工单系统](https://b.163.com/home/qiyu/worksheet)
- 环信:[客服云产品页](https://www.easemob.com/product/cs)

**说明**:各产品"15000+ 客户""移动端市占率 77.4%""独立解决 90% 常见问题"等数字均为官网自述,未做第三方核验,引用时按自述口径处理。
