// Package iceturn 装配 WebRTC ICE 配置并为 coturn 时间限凭据模式生成短时凭据。
//
// 选型与口径见 docs/TURN_DEPLOYMENT.md：coturn 以 use-auth-secret +
// static-auth-secret 运行，本包按“REST API for TURN”约定生成
// username=过期 Unix 秒、credential=base64(HMAC-SHA1(secret, username)) 的
// 短时凭据；secret 只存服务端，客户端经信令拿到的是到期可自愈的短时凭据。
//
// 契约：调用方先用 Config.Validate 做装配层兜底 gate（与 config.Validate 的
// 零容忍门禁成对），Assemble 不重复校验、不做回退。
package iceturn
