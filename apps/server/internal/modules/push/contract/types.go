package contract

import "time"

// RegisterPushTokenRequest 是访客推送注册的最小面（M3 移动 SDK 配套 §10 #5）：
// 免认证通道按 session 归属租户 scope，platform 服务端白名单校验
// （ios=APNs / android=FCM），token 为宿主推送平台的设备凭证。
type RegisterPushTokenRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Platform  string `json:"platform" binding:"required"`
	Token     string `json:"token" binding:"required"`
}

// PushTokenRegistration 是注册结果摘要：不含 token 全文（敏感面不回显，
// 上报方自持 token），幂等注册时 UpdatedAt 即最近保活时间。
type PushTokenRegistration struct {
	ID        uint      `json:"id"`
	SessionID string    `json:"session_id"`
	Platform  string    `json:"platform"`
	UpdatedAt time.Time `json:"updated_at"`
}
