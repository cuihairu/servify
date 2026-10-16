# WebRTC / Remote Assistance Deployment Guide

This document answers one frequently asked question: **what extra does deploying Servify's remote assistance capability require?** and gives the complete checklist for STUN/TURN, firewall ports, and verification steps.

Conclusions first:

1. **The WebRTC protocol stack (pion/webrtc) needs no separate deployment.** Servify uses [`pion/webrtc/v4`](https://github.com/pion/webrtc) (a pure Go library, see `github.com/pion/webrtc/v4 v4.2.20` in `apps/server/go.mod`), compiled directly into the `bin/servify` single binary at build time; there is no independent WebRTC process or container.
2. **Three things need planning**: a STUN service (self-hosted coturn or public STUN), server-side UDP firewall rules, and reverse-proxy configuration for the signaling entry point (`/api/v1/ws`).
3. **Configuration surface**: single STUN key `webrtc.stun_server` + multi-STUN redundancy `stun_servers`; TURN is supported (the time-limited credential shape decided in [TURN_DEPLOYMENT.md](./TURN_DEPLOYMENT.md) via `webrtc.turn.*`), **disabled by default**; enable it for strict-NAT traversal scenarios per [section 5](#5-self-hosted-coturn-deployment-example) (see [section 4](#4-stun-and-turn-current-state-and-boundary) for details).

## Table of Contents

1. [Architecture and Connection Topology](#1-architecture-and-connection-topology)
2. [Current Configuration Surface](#2-current-configuration-surface)
3. [Ports and Firewall Checklist](#3-ports-and-firewall-checklist)
4. [STUN and TURN: Current State and Boundary](#4-stun-and-turn-current-state-and-boundary)
5. [Self-hosted coturn Deployment Example](#5-self-hosted-coturn-deployment-example)
6. [Public STUN and Domestic Alternatives](#6-public-stun-and-domestic-alternatives)
7. [End-to-end Verification Steps](#7-end-to-end-verification-steps)
8. [Troubleshooting](#8-troubleshooting)
9. [Relation to the Remote Assistance REST Surface](#9-relation-to-the-remote-assistance-rest-surface)

## 1. Architecture and Connection Topology

Servify plays **two roles** in the WebRTC chain:

- **Signaling server**: the browser exchanges SDP offer/answer and ICE candidates with Servify over WebSocket (`/api/v1/ws?session_id=...`); message types `webrtc-offer` / `webrtc-answer` / `webrtc-candidate` (see `apps/server/internal/platform/realtime/websocket_hub.go`).
- **One PeerConnection endpoint**: the server process is itself also a PeerConnection endpoint (`WebRTCService.CreatePeerConnection`, see `apps/server/internal/platform/realtime/webrtc_service.go`). In other words, ICE direct connectivity happens between **browser ↔ Servify server**; media/data flows go through no third party, but **the server must participate in ICE**, so UDP connectivity is a server-side network concern.

```mermaid
flowchart LR
    B["浏览器<br/>访客/坐席"] -- "信令: WS /api/v1/ws" --> S["Servify 服务端<br/>pion/webrtc 内嵌"]
    B -. "ICE: STUN 地址发现" .-> ST["STUN 服务<br/>coturn 或公共"]
    B -. "ICE 直连: UDP" ==> S
```

Signaling goes over WebSocket: the browser exchanges SDP offer/answer and ICE candidates with Servify over `/api/v1/ws?session_id=...` (message types `webrtc-offer` / `webrtc-answer` / `webrtc-candidate`; after connection setup the server also proactively pushes a `webrtc-ice-config` delivering the ICE configuration and short-lived TURN credentials, see [section 2](#2-current-configuration-surface)). ICE direct connectivity happens between **browser ↔ Servify server**: media traffic (SRTP/DataChannel) goes through no third party, but the server must participate in ICE, so UDP connectivity is a server-side network concern. STUN only participates in address discovery (each side reports its own server-reflexive address); it carries no media traffic.

## 2. Current Configuration Surface

WebRTC-related configuration in `config.yml` (the `WebRTCConfig` in `apps/server/internal/config/config.go`):

```yaml
webrtc:
  # STUN 服务地址（单值，默认 stun:stun.l.google.com:19302）。
  stun_server: "stun:stun.l.google.com:19302"
  # 多 STUN 冗余（可选；配置后 stun_server 被忽略）。
  # stun_servers: ["stun:stun.qq.com:3478", "stun:stun.miwifi.com:3478"]
  # TURN 时间限凭据（docs/TURN_DEPLOYMENT.md；url 为空即禁用）。
  # turn:
  #   url: "turn:turn.example.com:3478"
  #   realm: "servify.example.com"
  #   static_auth_secret: "${TURN_STATIC_AUTH_SECRET}" # 只经 env 注入，不落配置文件
  #   ttl: 5m
```

Wiring: `wireRealtimeGateways` (`apps/server/internal/app/server/runtime_assembly.go`) assembles the STUN list and TURN config into an `ICEConfig` via `iceturn.Assemble` (`apps/server/internal/platform/iceturn/`) — when TURN is enabled, **short-lived credentials** are generated as `username = Unix expiry seconds` and `credential = base64(HMAC-SHA1(secret, username))` (default 5 minutes). The server PeerConnection's `ICEServers` and the client delivery share the same assembly result.

Client delivery: after the browser connects WS (`/api/v1/ws`), the server immediately pushes a `webrtc-ice-config` message (`data.ice_servers[]`: STUN entries carry only `urls`; TURN entries additionally carry `username`/`credential`/`ttl` in seconds). A same-shaped REST surface `GET /api/v1/rtc/ice-servers` (same auth as the `/webrtc/stats` admin surface) lets the client refresh proactively before credentials expire. SDK precedence: the host `remoteAssist.iceServers` override wins → WS push cache → REST fall back.

Deployment notes:

- **Mainland China or environments that cannot reach Google services must change the default**, otherwise ICE address discovery times out and slows connection setup (self-hosted coturn in [section 5](#5-self-hosted-coturn-deployment-example), public alternatives in [section 6](#6-public-stun-and-domestic-alternatives)).
- TURN enablement is a zero-tolerance gate: once `turn.url` is configured, `realm`/`static_auth_secret`/`ttl` must all be complete; missing any one refuses to start (`config.InsecureDefaults` warning + assembly-layer `iceturn.Config.Validate` double gate); the secret is only injected via env in production.

Environment variables (whitelist in `applyConfigEnvOverrides`, `apps/server/internal/app/bootstrap/config.go`): `WEBRTC_STUN_SERVERS` (comma-separated), `WEBRTC_TURN_URL`, `WEBRTC_TURN_REALM`, `WEBRTC_TURN_STATIC_AUTH_SECRET`.

## 3. Ports and Firewall Checklist

| Direction | Port | Protocol | Purpose | Required |
| --- | --- | --- | --- | --- |
| Client → Servify | 8080 (or your `server.port`) | TCP | HTTP API + WebSocket signaling (`/api/v1/ws`) | Required |
| Client → Servify | Deployment-environment UDP ephemeral port range (typically `32768-60999`) | UDP | ICE direct connectivity (SRTP media / DataChannel) | Required for WebRTC connection |
| Servify → STUN | 3478 | UDP | STUN binding requests (address discovery) | Required when using STUN |
| Client → coturn | 3478 | UDP/TCP | STUN/TURN entry (when self-hosted) | Required when self-hosting coturn |
| Client → coturn | `49160-49200` (configurable) | UDP | TURN relay ports (only when TURN is active) | Required when TURN is configured |

Notes on the UDP ephemeral port range (honest boundary):

- The current code does **not** constrain server-side UDP ports via pion's `SettingEngine.SetEphemeralUDPPortRange`; pion uses system ephemeral ports (Linux default `net.ipv4.ip_local_port_range`, typically `32768-60999`). For public deployments, the firewall must allow that range (or the system range must be narrowed).
- If Servify is deployed on the public internet (no NAT) and the client is also public, host candidate direct connectivity usually succeeds and STUN is only an acceleration path; if Servify sits behind NAT, the server needs UDP port forwarding or TURN (see the next section).

## 4. STUN and TURN: Current State and Boundary

| Capability | Current state |
| --- | --- |
| STUN | ✅ Supported, `webrtc.stun_server` single key + `webrtc.stun_servers` list |
| Multi-STUN redundancy | ✅ `stun_servers` list (env `WEBRTC_STUN_SERVERS` comma-separated) |
| TURN (relay) | ✅ **Supported**: `webrtc.turn.*` time-limited credentials (coturn `use-auth-secret` mode); credentials are signed at server runtime and delivered at WS connection (see [section 2](#2-current-configuration-surface) and [TURN_DEPLOYMENT.md](./TURN_DEPLOYMENT.md)) |
| Server-side UDP port constraint | ❌ No `SettingEngine` configuration exposed |

**What this means**: STUN solves the case where both sides are behind NAT but the NAT types are friendly (full-cone/restricted cone); once either side is symmetric NAT (common in some corporate networks and some mobile networks), pure STUN fails and TURN relay is required. TURN is now supported, **but disabled by default** (`turn.url` empty):

- Without TURN, the deployment recommendation is to give the **Servify server a public IP or 1:1 NAT (full port forwarding)** — with good server-side network conditions, the browser side will most likely achieve direct connectivity even behind symmetric NAT (outbound hole punching is completed by the browser-side NAT).
- Enable TURN for cross-symmetric-NAT scenarios: self-hosted coturn (the `use-auth-secret` time-limited credential shape, see [section 5](#5-self-hosted-coturn-deployment-example)) + configure `webrtc.turn.*` (see [section 2](#2-current-configuration-surface)); credential leakage is self-healing with no revocation flow needed.

## 5. Self-hosted coturn Deployment Example

Self-hosting coturn is the most controllable option (any machine with a public IP works; the STUN scenario has minimal bandwidth requirements).

**Production shape: `use-auth-secret` time-limited credentials** (decided in [TURN_DEPLOYMENT.md](./TURN_DEPLOYMENT.md); static long-lived credentials do not go to production). The repo ships compose delivery assets with the secret injected from the same source as the app:

```bash
export TURN_STATIC_AUTH_SECRET="$(openssl rand -hex 32)"
export TURN_PUBLIC_URL="turn:<coturn公网IP>:3478"
docker compose -f infra/compose/docker-compose.yml -f infra/compose/docker-compose.coturn.yml up -d
```

What `infra/compose/docker-compose.coturn.yml` does: a standalone coturn container + host network + `--use-auth-secret --static-auth-secret=${TURN_STATIC_AUTH_SECRET:?}` (refuses to start when missing) + relay segment `--min-port=49160 --max-port=49200`, while injecting `WEBRTC_TURN_URL/REALM/STATIC_AUTH_SECRET` into the servify service.

Equivalent manual `docker run` shape:

```bash
docker run -d --name coturn \
  --network host \
  coturn/coturn:4.6.2 \
  --listening-port=3478 \
  --realm=servify.example.com \
  --use-auth-secret \
  --static-auth-secret="$(openssl rand -hex 32)" \
  --min-port=49160 --max-port=49200 \
  --no-tls --no-dtls \
  --no-multicast-peers --no-loopback-peers \
  --no-cli --simple-log --log-file=stdout
```

Notes:

- `--network host` lets coturn use the host network directly, avoiding Docker NAT interference with address discovery; in this case the firewall allows `3478/udp` and `49160-49200/udp` directly on the host.
- The `--static-auth-secret` value must exactly match the Servify-side `WEBRTC_TURN_STATIC_AUTH_SECRET` (the app signs short-lived credentials with it, coturn verifies with it); **after generation, inject via env — do not put it in command-line history or config files**.
- For STUN-only use, `--use-auth-secret/--static-auth-secret/--min-port/--max-port` can be dropped (no credentials or relay ports needed).
- Replace `--realm` with your domain and keep it consistent with the app-side `webrtc.turn.realm`; TURNS (5349/TLS) is an optional enhancement — the media itself is end-to-end encrypted via DTLS-SRTP, so when needed, mount a certificate and drop `--no-tls --no-dtls`.
- For cloud-host 1:1 NAT scenarios, add `--external-ip=<public IP>`; otherwise the relay address is unreachable.

Minimal `/etc/turnserver.conf` for the systemd bare-metal approach (same auth-secret shape; secret recommended injected via `EnvironmentFile`):

```conf
listening-port=3478
use-auth-secret
static-auth-secret=<同 WEBRTC_TURN_STATIC_AUTH_SECRET>
realm=servify.example.com
min-port=49160
max-port=49200
no-cli
```

**Firewall rules** (using ufw as an example):

```bash
ufw allow 3478/udp
ufw allow 3478/tcp
ufw allow 49160:49200/udp
```

**Verify coturn itself works** (run on another machine to rule out local loopback):

```bash
# 方式一：coturn 自带工具
turnutils_stun -p 3478 <coturn公网IP>

# 方式二：浏览器打开 https://icetest.info 或
# https://webrtc.github.io/samples/src/content/peerconnection/trickle-ice/
# 填入 stun:<coturn公网IP>:3478，能看到 srflx 候选即为可用
```

Then enable it on the Servify side (env or config.yml — both are equivalent):

```yaml
webrtc:
  stun_server: "stun:<coturn公网IP>:3478"
  turn:
    url: "turn:<coturn公网IP>:3478"
    realm: "servify.example.com"
    static_auth_secret: "${TURN_STATIC_AUTH_SECRET}"
    ttl: 5m
```

After enabling, connect with a WS probe to `/api/v1/ws`; connection setup should deliver `webrtc-ice-config` immediately, and its TURN entry carries the short-lived `username`/`credential` (see [section 2](#2-current-configuration-surface) and [section 7](#7-end-to-end-verification-steps) ②).

## 6. Public STUN and Domestic Alternatives

Public STUN is available when you don't want to self-host (**note: third-party services carry no SLA; production deployments recommend self-hosting**):

| Service | Address | Applicable |
| --- | --- | --- |
| Google (default) | `stun:stun.l.google.com:19302` | International networks |
| Nextcloud | `stun:stun.nextcloud.com:443` | International networks (port 443 eases firewall traversal) |
| Xiaomi (domestic) | `stun:stun.miwifi.com:3478` | Mainland China |
| Tencent (domestic) | `stun:stun.qq.com:3478` | Mainland China |

Selection principles:

- **Do not keep the Google default for mainland China deployments** — when `stun.l.google.com` is unreachable, every connection waits for ICE address discovery to time out, showing up as WebRTC connection setup "working but very slow" or failing with a timeout.
- Public STUN only does address discovery and carries no media, so the security risk is low; the availability risk is backstopped by the verification steps in [section 7](#7-end-to-end-verification-steps).

## 7. End-to-end Verification Steps

Execute in order; proceed to the next step only after the current one passes (assumes the service runs on `:8080`):

**① Service and signaling entry point**

```bash
curl -fsS http://127.0.0.1:8080/health
curl -fsS http://127.0.0.1:8080/ready
# WS 入口可达性（无升级头时应得到 400/426 而非超时）
curl -sS -i --max-time 3 http://127.0.0.1:8080/api/v1/ws?session_id=probe | head -1
```

**② Signaling loopback (WebRTC message surface)**

Connect with any WS client to `ws://<host>:8080/api/v1/ws?session_id=webrtc-probe`; after connecting, you should **first receive a server-pushed `webrtc-ice-config`** (ICE configuration and TURN short-lived credentials; when TURN is not enabled, only STUN entries). Then send:

```json
{"type": "webrtc-offer", "data": {"sdp": "<真实浏览器生成的 SDP offer>", "type": "offer"}}
```

You should receive `webrtc-answer` (generated by server-side pion), after which the server proactively pushes `webrtc-candidate`.

**③ ICE connection state**

```bash
curl -fsS "http://127.0.0.1:8080/api/v1/webrtc/stats?session_id=webrtc-probe"
# 关注 connection_state 与 ice_connection_state：
#   connecting/checking → 正常推进中
#   connected/completed → ICE 直连成功
curl -fsS "http://127.0.0.1:8080/api/v1/webrtc/connections"
```

Repo acceptance baseline: `make remote-assist-acceptance` (including a real WS session) and the historical acceptance record (see the "实时能力" section of `docs/acceptance-checklist.md`) confirmed the server behavior of `connection_state=connecting` and `ice_connection_state=checking` — **when server-side SDP/signaling is normal, the constant state is staying at checking, waiting for browser-side UDP hole punching to complete**; only when UDP is fully unreachable does it slide from checking toward failed.

**④ Determine the network surface**

- `ice_connection_state` stuck at `checking` for a long time: signaling works but UDP hole punching does not → check the UDP allow rules and STUN reachability in [section 3](#3-ports-and-firewall-checklist).
- No `webrtc-answer` received: a signaling-surface problem, unrelated to STUN/UDP → check whether the reverse proxy correctly upgrades WebSocket (see the WS reverse proxy configuration in the production deployment section of `docs/deployment.md`).

## 8. Troubleshooting

| Symptom | Possible cause | Handling |
| --- | --- | --- |
| WS connects but no `webrtc-answer` arrives | Invalid SDP / server PeerConnection creation failed | Check server logs for `failed to create peer connection`; confirm the offer is real SDP generated by a browser |
| `ice_connection_state` stuck at `checking` | UDP not allowed / STUN unreachable / symmetric NAT | Allow the UDP port range; verify STUN with `turnutils_stun`; switch `webrtc.stun_server` (use a domestic source in domestic environments); if necessary, put the server directly on the public internet |
| Very slow connection setup (seconds) | The default Google STUN is unreachable and it waits for the timeout before falling back to host candidates | Switch to a reachable STUN (the default must be changed in domestic environments) |
| `connection_state` becomes `failed` | All ICE candidates failed | Is the server behind strict NAT? Set up UDP port forwarding; or enable TURN ([section 5](#5-self-hosted-coturn-deployment-example)) |
| Browser reports a `getUserMedia` error | Media capture failed; unrelated to deployment | The browser requires HTTPS/localhost to capture camera and microphone |

## 9. Relation to the Remote Assistance REST Surface

The remote assistance business surface (starting assist sessions, annotations, ending, and recording metadata) is a **separate REST API** (`/api/remote-assist` family, `RequireResourcePermission("assist")` permission), which does not depend on UDP/STUN and works over plain HTTPS; the full-chain real-machine acceptance was completed on 2026-09-18 (see the "远程协助会话与标注" row of `docs/acceptance-checklist.md`).

The division of labor between the two:

| Chain | Carries | Deployment requirement |
| --- | --- | --- |
| `/api/remote-assist/*` (REST) | Session management, screen annotation coordinates, recording metadata, ending state | HTTPS only, no extra network requirements |
| `/api/v1/ws` + WebRTC (this document) | Real-time audio/video / DataChannel real-time channel | WS reverse proxy + UDP + STUN (everything in this document) |

In short: **when only the REST surface is deployed, remote assistance's "management chain" is fully usable; the network surface in this document is needed only to light up the "real-time collaboration" capability.**
