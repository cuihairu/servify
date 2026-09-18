# WebRTC / 远程协助部署指南

本文回答一个高频问题：**部署 Servify 的远程协助能力，需要额外部署什么？** 并给出 STUN/TURN、防火墙端口与验证步骤的完整清单。

先给结论：

1. **WebRTC 协议栈（pion/webrtc）不需要单独部署**。Servify 使用 [`pion/webrtc/v4`](https://github.com/pion/webrtc)（纯 Go 库，见 `apps/server/go.mod` 的 `github.com/pion/webrtc/v4 v4.2.20`），编译期直接打进 `bin/servify` 单二进制，没有任何独立的 WebRTC 进程或容器。
2. **需要规划的是三件事**：STUN 服务（可自建 coturn 或用公共 STUN）、服务器侧 UDP 防火墙、信令入口（`/api/v1/ws`）的反代配置。
3. **当前版本的配置面只有一个键**：`webrtc.stun_server`。TURN 尚无配置面（详见[第 4 节](#4-stun-与-turn现状与边界)），跨严格 NAT 的公网互通需要先扩展这一处代码。

## 目录

1. [架构与连接拓扑](#1-架构与连接拓扑)
2. [当前配置面](#2-当前配置面)
3. [端口与防火墙清单](#3-端口与防火墙清单)
4. [STUN 与 TURN：现状与边界](#4-stun-与-turn现状与边界)
5. [自建 coturn 部署示例](#5-自建-coturn-部署示例)
6. [公共 STUN 与国内替代](#6-公共-stun-与国内替代)
7. [端到端验证步骤](#7-端到端验证步骤)
8. [故障排查](#8-故障排查)
9. [与远程协助 REST 面的关系](#9-与远程协助-rest-面的关系)

## 1. 架构与连接拓扑

Servify 在 WebRTC 链路中承担**两个角色**：

- **信令服务器**：浏览器通过 WebSocket（`/api/v1/ws?session_id=...`）与 Servify 交换 SDP offer/answer 与 ICE candidate（消息类型 `webrtc-offer` / `webrtc-answer` / `webrtc-candidate`，见 `apps/server/internal/services/websocket.go`）。
- **PeerConnection 一端**：服务端进程自己也是一个 PeerConnection 端点（`WebRTCService.CreatePeerConnection`，见 `apps/server/internal/services/webrtc.go`）。也就是说 ICE 直连发生在**浏览器 ↔ Servify 服务器**之间，媒体/数据流不经过任何第三方，但**服务器必须参与 ICE**，UDP 连通性是服务器侧的网络问题。

```mermaid
flowchart LR
    B["浏览器<br/>访客/坐席"] -- "信令: WS /api/v1/ws" --> S["Servify 服务端<br/>pion/webrtc 内嵌"]
    B -. "ICE: STUN 地址发现" .-> ST["STUN 服务<br/>coturn 或公共"]
    B -. "ICE 直连: UDP" ==> S
```

信令走 WebSocket：浏览器通过 `/api/v1/ws?session_id=...` 与 Servify 交换 SDP offer/answer 与 ICE candidate（消息类型 `webrtc-offer` / `webrtc-answer` / `webrtc-candidate`）。ICE 直连发生在**浏览器 ↔ Servify 服务器**之间：媒体流量（SRTP/DataChannel）不经过任何第三方，但服务器必须参与 ICE，UDP 连通性是服务器侧的网络问题。STUN 只参与地址发现（双方报出各自的 server-reflexive 地址），不承载媒体流量。

## 2. 当前配置面

`config.yml` 中与 WebRTC 相关的配置只有一项：

```yaml
webrtc:
  # ICE 使用的 STUN 服务地址（单值）。
  # 默认 stun:stun.l.google.com:19302（见 apps/server/internal/config/config.go）。
  stun_server: "stun:stun.l.google.com:19302"
```

该值被 `NewWebRTCService(cfg.WebRTC.STUNServer, wsHub)` 消费（`apps/server/internal/app/server/runtime_assembly.go`），最终进入每个 PeerConnection 的 `ICEServers[].URLs`。

部署要点：

- **中国大陆或无法访问 Google 服务的环境必须改掉默认值**，否则 ICE 地址发现会超时拖慢建连（自建 coturn 见[第 5 节](#5-自建-coturn-部署示例)，公共替代见[第 6 节](#6-公共-stun-与国内替代)）。
- 当前实现只把**这一个 URL** 塞进 ICEServers（`apps/server/internal/services/webrtc.go` 的 `CreatePeerConnection`），不支持多个 STUN 冗余、也不支持带凭据的 TURN 条目。

环境变量：**当前没有** `webrtc.stun_server` 的环境变量覆盖（env 覆盖是白名单式的，见 `apps/server/internal/app/bootstrap/config.go` 的 `applyConfigEnvOverrides`，不含 webrtc 键）。修改 STUN 只能通过配置文件的 `webrtc.stun_server` 键。

## 3. 端口与防火墙清单

| 方向 | 端口 | 协议 | 用途 | 必需性 |
| --- | --- | --- | --- | --- |
| 客户端 → Servify | 8080（或你的 `server.port`） | TCP | HTTP API + WebSocket 信令（`/api/v1/ws`） | 必需 |
| 客户端 → Servify | 部署环境 UDP 临时端口范围（通常 `32768-60999`） | UDP | ICE 直连（SRTP 媒体 / DataChannel） | WebRTC 建连必需 |
| Servify → STUN | 3478 | UDP | STUN 绑定请求（地址发现） | 使用 STUN 时必需 |
| 客户端 → coturn | 3478 | UDP/TCP | STUN/TURN 入口（自建时） | 自建 coturn 时必需 |
| 客户端 → coturn | `49160-49200`（可配） | UDP | TURN 中继端口（仅 TURN 生效时用） | 配置 TURN 时必需 |

关于 UDP 临时端口范围的说明（如实边界）：

- 当前代码**没有**通过 pion 的 `SettingEngine.SetEphemeralUDPPortRange` 收敛服务端 UDP 端口，pion 使用系统临时端口（Linux 默认 `net.ipv4.ip_local_port_range`，通常 `32768-60999`）。公网部署时防火墙需放行该范围（或收窄系统范围）。
- 若 Servify 部署在公网（无 NAT）且客户端也在公网，host candidates 直连通常即可成功，STUN 只是加速路径；若 Servify 在 NAT 后，需要为服务器做 UDP 端口转发或 TURN（见下一节）。

## 4. STUN 与 TURN：现状与边界

| 能力 | 现状 |
| --- | --- |
| STUN | ✅ 支持，`webrtc.stun_server` 单键配置 |
| 多 STUN 冗余 | ❌ 当前只填一个 URL |
| TURN（中继） | ❌ **无配置面**：`ICEServer.Username/Credential` 未暴露，`webrtc.Configuration` 亦未暴露附加 ICE server 列表 |
| 服务端 UDP 端口收敛 | ❌ 未暴露 `SettingEngine` 配置 |

**这意味着什么**：STUN 能解决"双方都在 NAT 后、但 NAT 类型友好（full-cone/restricted cone）"的打洞；一旦任一侧是对称 NAT（常见于部分企业网、部分移动网络），纯 STUN 会打洞失败，必须走 TURN 中继。在补齐 TURN 配置面之前：

- 部署建议让 **Servify 服务器拥有公网 IP 或一对一 NAT（全端口转发）**——服务端网络条件良好时，浏览器侧即使在对称 NAT 后也大概率能直连（出站方向的打洞由浏览器侧 NAT 完成）。
- 若确有跨对称 NAT 的场景，需要先做一处代码扩展：`webrtc` 配置增加 `ice_servers` 列表（含 `username`/`credential`），`CreatePeerConnection` 填入完整 `ICEServers`。改动面很小（config 结构体 + `webrtc.go` 一处），是明确的演进路径。

## 5. 自建 coturn 部署示例

自建 coturn 是最可控的方案（一台有公网 IP 的机器即可，STUN 场景对带宽要求极低）。Docker 方式：

```bash
docker run -d --name coturn \
  --network host \
  coturn/coturn:latest \
  -n --no-cli \
  --listening-port=3478 \
  --fingerprint \
  --lt-cred-mech \
  --realm=servify.example.com \
  --user=servify:servify-turn-secret \
  --min-port=49160 --max-port=49200 \
  --no-tls --no-dtls
```

说明：

- `--network host` 让 coturn 直接使用宿主网络，避免 Docker NAT 干扰地址发现；此时防火墙直接对宿主放行 `3478/udp` 与 `49160-49200/udp`。
- 只做 STUN 用可以去掉 `--lt-cred-mech/--user/--min-port/--max-port`（不需要凭据与中继端口）；带上它们是为将来 Servify 补齐 TURN 配置面后无需再动基础设施。
- `--realm` 换成你的域名；生产建议再加 TLS/DTLS（`--no-tls --no-dtls` 去掉并配证书），本例从简。

systemd 裸机方式的最小 `/etc/turnserver.conf`：

```conf
listening-port=3478
fingerprint
lt-cred-mech
realm=servify.example.com
user=servify:servify-turn-secret
min-port=49160
max-port=49200
```

**防火墙放行**（以 ufw 为例）：

```bash
ufw allow 3478/udp
ufw allow 3478/tcp
ufw allow 49160:49200/udp
```

**验证 coturn 本身可用**（在另一台机器上执行，排除本机回路）：

```bash
# 方式一：coturn 自带工具
turnutils_stun -p 3478 <coturn公网IP>

# 方式二：浏览器打开 https://icetest.info 或
# https://webrtc.github.io/samples/src/content/peerconnection/trickle-ice/
# 填入 stun:<coturn公网IP>:3478，能看到 srflx 候选即为可用
```

然后在 Servify 侧启用：

```yaml
webrtc:
  stun_server: "stun:<coturn公网IP>:3478"
```

## 6. 公共 STUN 与国内替代

不想自建时可用公共 STUN（**注意：第三方服务无 SLA，生产环境推荐自建**）：

| 服务 | 地址 | 适用 |
| --- | --- | --- |
| Google（默认值） | `stun:stun.l.google.com:19302` | 国际网络 |
| Nextcloud | `stun:stun.nextcloud.com:443` | 国际网络（443 端口利于穿墙） |
| 小米（国内） | `stun:stun.miwifi.com:3478` | 中国大陆 |
| Tencent（国内） | `stun:stun.qq.com:3478` | 中国大陆 |

选择原则：

- 中国大陆部署**不要保留 Google 默认值**——`stun.l.google.com` 不可达时，每次建连都要等 ICE 地址发现超时，表现为 WebRTC 建连"能通但很慢"或超时失败。
- 公共 STUN 只做地址发现、不承载媒体，安全性风险低；可用性风险由[第 7 节](#7-端到端验证步骤)的验证步骤兜底。

## 7. 端到端验证步骤

按顺序执行，每步通过再走下一步（假设服务跑在 `:8080`）：

**① 服务与信令入口**

```bash
curl -fsS http://127.0.0.1:8080/health
curl -fsS http://127.0.0.1:8080/ready
# WS 入口可达性（无升级头时应得到 400/426 而非超时）
curl -sS -i --max-time 3 http://127.0.0.1:8080/api/v1/ws?session_id=probe | head -1
```

**② 信令回环（WebRTC 消息面）**

用任意 WS 客户端连 `ws://<host>:8080/api/v1/ws?session_id=webrtc-probe`，发送：

```json
{"type": "webrtc-offer", "data": {"sdp": "<真实浏览器生成的 SDP offer>", "type": "offer"}}
```

应收到 `webrtc-answer`（服务端 pion 生成），随后服务端会主动推送 `webrtc-candidate`。

**③ ICE 连接状态**

```bash
curl -fsS "http://127.0.0.1:8080/api/v1/webrtc/stats?session_id=webrtc-probe"
# 关注 connection_state 与 ice_connection_state：
#   connecting/checking → 正常推进中
#   connected/completed → ICE 直连成功
curl -fsS "http://127.0.0.1:8080/api/v1/webrtc/connections"
```

仓库验收基线：`make remote-assist-acceptance`（含 WS 真实建会话）与历史验收记录（见 `docs/acceptance-checklist.md` "实时能力"节）确认过 `connection_state=connecting`、`ice_connection_state=checking` 的服务端行为——**服务端 SDP/信令正常时的常态就是停在 checking，等待浏览器侧 UDP 打洞完成**；只有 UDP 全程不通才会从 checking 滑向 failed。

**④ 判定网络面**

- `ice_connection_state` 长期 `checking`：信令通、UDP 打洞不通 → 查[第 3 节](#3-端口与防火墙清单)的 UDP 放行与 STUN 可达性。
- 收不到 `webrtc-answer`：信令面问题，与 STUN/UDP 无关 → 查反代是否正确升级 WebSocket（见 `docs/deployment.md` 生产部署一节的 WS 反代配置）。

## 8. 故障排查

| 现象 | 可能原因 | 处置 |
| --- | --- | --- |
| WS 连上但收不到 `webrtc-answer` | SDP 非法 / 服务端 PeerConnection 创建失败 | 查服务端日志 `failed to create peer connection`；确认 offer 是浏览器生成的真实 SDP |
| `ice_connection_state` 卡在 `checking` | UDP 未放行 / STUN 不可达 / 对称 NAT | 放行 UDP 端口范围；`turnutils_stun` 验证 STUN；更换 `webrtc.stun_server`（国内换国内源）；必要时让服务器直挂公网 |
| 建连很慢（数秒级） | 默认 Google STUN 不可达，等超时才回退 host 候选 | 换可达的 STUN（国内环境必改默认值） |
| `connection_state` 变 `failed` | ICE 全部候选失败 | 服务端是否在严格 NAT 后？做 UDP 端口转发；或等待 TURN 配置面补齐 |
| 浏览器侧报 `getUserMedia` 错误 | 媒体采集失败，与部署无关 | 浏览器需 HTTPS/localhost 才能采集摄像头麦克风 |

## 9. 与远程协助 REST 面的关系

远程协助的业务面（发起协助会话、标注、结束与录制元数据）是**独立的 REST API**（`/api/remote-assist` 系列，`RequireResourcePermission("assist")` 权限），不依赖 UDP/STUN，普通 HTTPS 即可用；2026-09-18 已完成全链路真实验收（见 `docs/acceptance-checklist.md` "远程协助会话与标注"行）。

两者的分工：

| 链路 | 承载 | 部署要求 |
| --- | --- | --- |
| `/api/remote-assist/*`（REST） | 会话管理、屏幕标注坐标、录制元数据、结束态 | 仅 HTTPS，无额外网络要求 |
| `/api/v1/ws` + WebRTC（本文） | 实时音视频 / DataChannel 实时通道 | WS 反代 + UDP + STUN（本文全部内容） |

即：**只部署 REST 面时远程协助的"管理链路"完整可用；要亮出"实时协作"能力，才需要本文的网络面。**
