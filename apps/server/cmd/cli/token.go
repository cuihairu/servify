package cli

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/config"

	"github.com/spf13/cobra"
)

var (
	flagUserID   int
	flagSubject  string
	flagRoles    string
	flagPerms    string
	flagTTLMin   int
	flagNoExpiry bool
)

// tokenCmd generates an HS256 JWT for testing/admin usage.
var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Generate a JWT (HS256) for API authentication",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		secret := cfg.JWT.Secret
		if secret == "" {
			return fmt.Errorf("jwt.secret is empty; set it in config")
		}
		now := time.Now()
		payload := map[string]interface{}{
			"iat": now.Unix(),
		}
		if flagUserID > 0 {
			payload["user_id"] = flagUserID
			if flagSubject == "" {
				payload["sub"] = fmt.Sprintf("%d", flagUserID)
			}
		}
		if flagSubject != "" {
			payload["sub"] = flagSubject
		}
		if strings.TrimSpace(flagRoles) != "" {
			parts := strings.Split(flagRoles, ",")
			var roles []string
			for _, p := range parts {
				if s := strings.TrimSpace(p); s != "" {
					roles = append(roles, s)
				}
			}
			if len(roles) > 0 {
				payload["roles"] = roles
			}
		}
		if strings.TrimSpace(flagPerms) != "" {
			parts := strings.Split(flagPerms, ",")
			var perms []string
			for _, p := range parts {
				if s := strings.TrimSpace(p); s != "" {
					perms = append(perms, s)
				}
			}
			if len(perms) > 0 {
				payload["perms"] = perms
			}
		}
		if !flagNoExpiry {
			payload["exp"] = now.Add(time.Duration(flagTTLMin) * time.Minute).Unix()
		}
		fmt.Println(createHS256JWT(payload, secret))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(tokenCmd)
	tokenCmd.Flags().IntVar(&flagUserID, "user-id", 1, "numeric user id to embed in token")
	tokenCmd.Flags().StringVar(&flagSubject, "sub", "", "subject (sub) claim; defaults to user-id if provided")
	tokenCmd.Flags().StringVar(&flagRoles, "roles", "admin", "comma-separated roles (e.g. admin,agent)")
	tokenCmd.Flags().StringVar(&flagPerms, "perms", "", "comma-separated permissions (optional; overrides/extends RBAC mapping)")
	tokenCmd.Flags().IntVar(&flagTTLMin, "ttl", 60, "token time-to-live in minutes")
	tokenCmd.Flags().BoolVar(&flagNoExpiry, "no-exp", false, "do not include exp claim")
}

// createHS256JWT builds a compact JWT using HS256 with the given payload.
// 无错误返回：函数内唯一的错误来源 json.Marshal 的错误在函数内即被丢弃，
// 返回值恒为拼接结果，调用方不存在可触达的错误分支。
func createHS256JWT(payload map[string]interface{}, secret string) string {
	header := map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	}
	headerJSON, _ := json.Marshal(header)
	payloadJSON, _ := json.Marshal(payload)
	enc := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

	h := enc(headerJSON)
	p := enc(payloadJSON)
	signing := h + "." + p

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signing))
	sig := mac.Sum(nil)
	s := enc(sig)
	return signing + "." + s
}
