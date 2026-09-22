# TURN 部署选型：内嵌 pion/turn vs 独立 coturn

> 状态：选型已拍板（2026-09-22）：**coturn 独立部署 + 时间限凭据**。第 5 节清单第 1–4 项与第 6 项已落地（2026-09-22）：配置面、`iceturn` 凭据包、服务端装配、WS 下发 `webrtc-ice-config` + REST 下发口 `/api/v1/rtc/ice-servers`（SDK `startCall` 桩已消，服务端下发为默认、宿主覆盖保留）、coturn compose 交付资产、双文档同步。剩余：`docs/deployment.md` 交付清单节、TURN 建联验收脚本。
> 背景：2026-09-22 架构评审确认 TURN 无配置面（[部署指南第 4 节](./webrtc-deployment.md)已如实记录现状），跨对称 NAT 场景下 WebRTC 实时链路不可用。本文档回答"选哪个方案、凭据怎么管、代码扩展落在哪里"，**不重复操作细节**（安装、端口、验证见[WebRTC / 远程协助部署指南](./webrtc-deployment.md)）。

## 结论先行

1. **方案：coturn 独立部署（独立容器/进程），不做 pion/turn 内嵌。** 决定性维度是网络位置独立（TURN 需要公网 UDP，不该绑在 app 容器上）与生命周期解耦（app 滚动发版不能掐断进行中的媒体中继）。
2. **凭据口径：时间限 HMAC 凭据（coturn `use-auth-secret` 模式）**，服务端按 `static-auth-secret` 动态生成、经信令下发；**静态长期凭据不进生产**（[部署指南第 5 节](./webrtc-deployment.md)的 `--lt-cred-mech --user` 示例是最小可用形态，仅适用纯 STUN/开发，TURN 启用时按本文第 4 节升级）。
3. **托管云 TURN（Twilio NTS / Cloudflare Calls TURN 等）一票否决**：中国大陆私有化交付下不可达/不稳，与知识库选型的 SaaS-only 一票否决同口径（见 [KNOWLEDGE_BASE_LANDSCAPE](./KNOWLEDGE_BASE_LANDSCAPE.md)）。
4. 落地量级：配置面 + 凭据包 + 下发口 + compose 模板约 2–3 天（见第 5 节），客户机房真实联调另计。

## 1. 事实链（2026-09-22 核查）

| # | 事实 | 位置 |
| --- | --- | --- |
| 1 | 服务端是 PeerConnection 一端（浏览器 ↔ 服务端 ICE 直连），每个连接的 `ICEServers` 只填单 STUN | `apps/server/internal/platform/realtime/webrtc_service.go` `CreatePeerConnection` |
| 2 | WebRTC 配置面单键，默认 Google STUN，大陆机房不可达（超时拖慢建连） | `config.yml` `webrtc.stun_server`；兜底值在 `apps/server/internal/config/config.go` |
| 3 | `webrtc.stun_server` 无环境变量覆盖（env 覆盖白名单不含 webrtc 键） | `apps/server/internal/app/bootstrap/config.go` `applyConfigEnvOverrides` |
| 4 | 客户端一端的 ICE 配置默认空数组、靠宿主集成方自备；REST 下发口 `startCall` 是显式 `unsupported()` 桩 | `sdk/packages/core/src/sdk.ts`（`remoteAssist.iceServers` 回退链）、`sdk/packages/core/src/api.ts` |
| 5 | pion/turn v5 已在依赖图（`pion/webrtc` 间接依赖），内嵌方案无新增模块成本 | `apps/server/go.mod` |
| 6 | 交付形态以单机 Docker Compose 私有化为主 | `docs/deployment.md` |
| 7 | TURN 中继无配置面：`ICEServer.Username/Credential` 未暴露 | `apps/server/internal/platform/realtime/webrtc_service.go` |

**拓扑口径**（与[部署指南第 1 节](./webrtc-deployment.md)一致）：ICE 直连发生在浏览器 ↔ Servify 服务器之间，两端**都**需要 ICE 配置——服务端从 config 装配（事实 1），客户端浏览器一端目前靠宿主自备（事实 4）。因此 TURN 落地必须同时补"服务端装配"与"客户端下发口"两处，缺一则对称 NAT 仍有一侧打不通。

## 2. 候选方案与硬约束

| 方案 | 形态 |
| --- | --- |
| A：pion/turn 内嵌 | 在 Go 进程内用 `pion/turn` 起一个 TURN server（约 200 行自建代码） |
| B：coturn 独立部署 | 独立容器/进程（业界事实标准，Jitsi/Matrix/Nextcloud 同款） |
| C：托管云 TURN | Twilio NTS / Cloudflare Calls TURN 等 SaaS |

硬约束：

- **交付形态**：私有化单机 compose（事实 6），客户机房网络条件不可控；
- **产品底线**：远程协助/语音是客服实时链路，**会话中断即事故**；
- **网络可达**：主要交付场景在中国大陆（事实 2 佐证），C 直接出局。

## 3. 对比与推荐

| 维度 | A：pion/turn 内嵌 | B：coturn 独立部署 ✅ |
| --- | --- | --- |
| 部署单元 | 零新增（随 app 起停） | compose 新增 1 服务，host 网络 + 端口段（3478、5349、中继段如 49160-49200/udp） |
| **网络位置** | ❌ app 容器必须直接持有公网 UDP，与"app 走反代/LB 暴露"的常规交付冲突 | ✅ 独立容器可放公网节点/DMZ，app 网络拓扑不变 |
| **生命周期耦合** | ❌ app 每次发版/重启 = 全部 allocation 掐断，进行中的实时会话中断 | ✅ app 滚动发版不影响媒体中继 |
| 成熟度 | 库 + 自建 server 约 200 行，生产规模验证少 | 20 年边界情况积累（TCP/TLS relay、IPv6、filtering、防滥用） |
| 攻击面隔离 | ❌ relay 是流量滥用目标，与业务进程同生死 | ✅ 独立进程，relay 被打不波及客服主服务 |
| 凭据 | 自写 auth 回调（可复用 session 校验，但每次 allocation 回打 app，耦合） | ✅ `use-auth-secret` 原生支持时间限凭据，Go 侧约 30 行生成 |
| 可观测 | ✅ 可直接挂 prometheus（顺 MetricsAggregator 桥模式） | ⚠️ 无原生 prometheus（第三方 exporter 质量一般），靠日志 + connection stats |
| 团队顺手度 | ✅ 纯 Go | C 组件，多一份 runbook/升级职责 |

**决定性维度（网络位置、生命周期解耦）均指向 B。** A 的真实优势是"单二进制极简交付、媒体量小时运维最省"，适用于边缘小部署场景——servify 交付形态（事实 6）与产品底线（实时链路不能随发版断）不满足其前提，**不推荐**；若未来出现"all-in-one 单机镜像"交付形态可重新评估。

## 4. 凭据与安全口径

**机制**（coturn REST API / 时间限凭据，社区通行做法）：

```
username  = <expiry 的 Unix 时间戳>
credential = base64(HMAC-SHA1(static_auth_secret, username))
```

- coturn 侧：`use-auth-secret` + `static-auth-secret=<随机串>`，替换部署指南第 5 节的 `--lt-cred-mech --user` 静态凭据模式；
- TTL 建议 **5 分钟**：到期后已有 allocation 继续存活到连接结束，新 allocation 需重新取凭据；
- 服务端持有 secret 生成凭据，客户端（浏览器一端）经信令/REST 拿到的是短时凭据，**泄露可自愈、无需吊销流程**；
- 中继端口段保持收敛（如 `min-port=49160 --max-port=49200`），防火墙按段放行；
- secret 属敏感配置，进入 secure-config 门禁管辖（见第 5 节），不得进 demo/mock 产物与文档示例。

## 5. 落地路径（代码扩展清单）

按依赖顺序：

1. ✅（2026-09-22）**配置面**：`webrtc` 块扩展为 `stun_servers`（列表，多 STUN 冗余）+ `turn.url / turn.realm / turn.static_auth_secret / turn.ttl`。
   > 口径说明：[部署指南第 4 节](./webrtc-deployment.md)曾建议"配置加 `ice_servers` 列表（含 username/credential）"的最小演进路径——该路径会诱导**静态凭据写进配置文件**，本文档将其升级为上述 `turn.*` 口径：secret 只存服务端，凭据运行时生成。落地时同步更新部署指南第 2/4 节。
   门禁惯例：零容忍 `Validate` + 装配层兜底双层 gate；新增配置键必改 secure-config 白名单/模板 lint 测试（参照 `internal/config/config_template_lint_test.go`、`TestValidate_SecureConfigIsValid` 等）并同步 `config.yml` + `config.production.secure.example.yml` + `config.staging.example.yml` 三模板；`applyConfigEnvOverrides` 白名单补 webrtc 键（事实 3）。
2. ✅（2026-09-22）**平台包**：`internal/platform/` 新增 ICE 凭据包（或并入 `realtime`）：凭据生成（HMAC）+ ICE 配置装配，provider `none | coturn`，接口带 mock seam（覆盖率门禁 100%，异步副作用测试按既有惯例轮询）。
3. ✅（2026-09-22）**服务端装配**：`CreatePeerConnection` 填入完整 `ICEServers`（STUN 列表 + TURN 条目带短时凭据），voice/realtime 一处收口。
4. ✅（2026-09-22）**客户端下发口**：最小路径走 WS 信令建联时下发 ICE 配置（沿用 `webrtc-state-change` 类消息模式）；REST 面（`/api/v1/rtc/ice-servers`，workspace 鉴权）作为后续项，同时消掉 SDK `startCall` 的 `unsupported` 桩。SDK `remoteAssist.iceServers` 保留为宿主覆盖口，服务端下发为默认。
5. ◐（compose 资产 2026-09-22）**交付资产**：`infra/compose/docker-compose.coturn.yml`（host 网络 + `use-auth-secret` 模式 + 端口段 + 与 app 共享 secret 的 env 注入）；`docs/deployment.md` 交付清单补"远程协助网络要求"一节；验收脚本参照 `test-ragflow-acceptance.sh` 的 mock/real 双模式思路补 TURN 建联验收。
6. ✅（2026-09-22）**文档同步**：部署指南第 4 节现状表 TURN 行改 ✅、第 5 节 coturn 示例升级为 auth-secret 模式；本文档状态头更新落地排期。

## 6. 与既有文档分工

| 文档 | 职责 |
| --- | --- |
| 本文（TURN_DEPLOYMENT.md） | 选型拍板、凭据口径、代码扩展落点 |
| [webrtc-deployment.md](./webrtc-deployment.md) | 操作层：拓扑、端口/防火墙、coturn 安装、端到端验证、故障排查 |
| [architecture-review-2026.md](./architecture-review-2026.md) | AI/Agent 方向评审（本文是其 WebRTC 部署缺口的跟进决策） |

## 参考来源

- coturn 官方仓库（`use-auth-secret` / `static-auth-secret` / REST API 机制说明）：https://github.com/coturn/coturn
- 时间限 TURN 凭据机制（REST API for TURN Access）：https://datatracker.ietf.org/doc/html/draft-uberti-behave-turn-rest
- pion/turn（内嵌方案的库，已入依赖图）：https://github.com/pion/turn
- Trickle ICE 在线验证：https://webrtc.github.io/samples/src/content/peerconnection/trickle-ice/
