package iceturn

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DefaultTTL 是未显式配置 webrtc.turn.ttl 时的凭据有效期。
const DefaultTTL = 5 * time.Minute

// Config 是 TURN 时间限凭据模式的装配参数（coturn use-auth-secret）。
type Config struct {
	URL              string
	Realm            string
	StaticAuthSecret string
	TTL              time.Duration
}

// Enabled 上报 TURN 是否启用（URL 非空）。
func (c Config) Enabled() bool {
	return strings.TrimSpace(c.URL) != ""
}

// Validate 是装配层兜底 gate（与 config.InsecureDefaults 的零容忍告警成对）：
// TURN 启用即要求 realm / secret / ttl 完整，缺一拒绝启动。
func (c Config) Validate() error {
	if !c.Enabled() {
		return nil
	}
	if strings.TrimSpace(c.Realm) == "" {
		return errors.New("webrtc.turn.url is set but webrtc.turn.realm is empty")
	}
	if strings.TrimSpace(c.StaticAuthSecret) == "" {
		return errors.New("webrtc.turn.url is set but webrtc.turn.static_auth_secret is empty")
	}
	if c.TTL <= 0 {
		return errors.New("webrtc.turn.url is set but webrtc.turn.ttl is not positive")
	}
	return nil
}

// WithDefaults 把非正 TTL 归一为 DefaultTTL（TTL 只在启用分支被消费）。
func (c Config) WithDefaults() Config {
	if c.TTL <= 0 {
		c.TTL = DefaultTTL
	}
	return c
}

// Credential 是一次 TURN allocation 的短时凭据。
type Credential struct {
	Username   string
	Credential string
}

// RESTCredential 按“REST API for TURN”约定生成时间限凭据：
// username = 过期时刻的 Unix 秒，credential = base64(HMAC-SHA1(secret, username))。
// TTL 非正时回退 DefaultTTL，保证调用方漏配时凭据仍然可用。
func RESTCredential(secret string, ttl time.Duration, now time.Time) Credential {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	username := strconv.FormatInt(now.Add(ttl).Unix(), 10)
	mac := hmac.New(sha1.New, []byte(secret))
	// Write 对 hash 对象永不返回错误，契约式忽略。
	_, _ = mac.Write([]byte(username))
	return Credential{
		Username:   username,
		Credential: base64.StdEncoding.EncodeToString(mac.Sum(nil)),
	}
}

// ICEConfig 是装配完成的 ICE 配置，服务端 PeerConnection 与客户端下发共用同一份形状。
type ICEConfig struct {
	STUNServers    []string
	TURNURL        string
	TURNUsername   string
	TURNCredential string
	// TURNTTL 是签发凭据时用的有效期，随下发给客户端用于规划刷新；
	// TURN 未启用时为零值。
	TURNTTL time.Duration
}

// Assemble 把 STUN 列表（为空时回退单值 legacy 配置）与 TURN 配置装配为 ICEConfig。
// TURN 未启用时只含 STUN；启用时由调用方保证已通过 Config.Validate，
// 这里按契约直接生成凭据，不做二次校验。
func Assemble(stunServers []string, legacySTUNServer string, turn Config, now time.Time) ICEConfig {
	ice := ICEConfig{}
	seen := make(map[string]bool, len(stunServers))
	for _, server := range stunServers {
		trimmed := strings.TrimSpace(server)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		ice.STUNServers = append(ice.STUNServers, trimmed)
	}
	if len(ice.STUNServers) == 0 {
		if legacy := strings.TrimSpace(legacySTUNServer); legacy != "" {
			ice.STUNServers = []string{legacy}
		}
	}
	if !turn.Enabled() {
		return ice
	}
	credential := RESTCredential(turn.StaticAuthSecret, turn.TTL, now)
	ice.TURNURL = strings.TrimSpace(turn.URL)
	ice.TURNUsername = credential.Username
	ice.TURNCredential = credential.Credential
	ice.TURNTTL = turn.WithDefaults().TTL
	return ice
}

// Describe 返回装配结果的人类可读摘要（日志用），凭据本体不落日志。
func (c ICEConfig) Describe() string {
	stunCount := len(c.STUNServers)
	if c.TURNURL == "" {
		return fmt.Sprintf("stun=%d,turn=disabled", stunCount)
	}
	return fmt.Sprintf("stun=%d,turn=%s,realm-credential=issued", stunCount, c.TURNURL)
}
