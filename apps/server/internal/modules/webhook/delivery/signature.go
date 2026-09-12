package delivery

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// SignatureHeader 计算出站投递的 X-Servify-Signature 头：
//
//	t=<unix秒>,v1=<hex(hmac-sha256(secret, "<t>.<body>"))>
//
// 接收方校验方式：用同样的 secret 对 "t=值 + '.' + 原始请求体" 计算 HMAC 并
// 恒等比较；|now - t| 超过 5 分钟应拒绝（防重放）。
func SignatureHeader(secret string, body []byte, at time.Time) string {
	t := at.Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", t)
	mac.Write(body)
	return fmt.Sprintf("t=%d,v1=%s", t, hex.EncodeToString(mac.Sum(nil)))
}

// VerifySignature 供接收方/测试侧对称校验（窗口 ±5 分钟）。
func VerifySignature(secret string, body []byte, header string, now time.Time) bool {
	var t int64
	var v1 string
	if _, err := fmt.Sscanf(header, "t=%d,v1=%s", &t, &v1); err != nil {
		return false
	}
	if diff := now.Unix() - t; diff < -300 || diff > 300 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", t)
	mac.Write(body)
	return hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(v1))
}
