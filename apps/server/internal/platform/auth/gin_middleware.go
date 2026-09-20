package auth

import (
	"net/http"
	"strings"
	"time"

	"servify/apps/server/internal/config"

	"github.com/gin-gonic/gin"
)

type MiddlewareConfig struct {
	Secret string
	RBAC   config.RBACConfig
	Now    func() time.Time
	Policy TokenPolicy
	// APIKeyResolver 可选：提供后接受 X-API-Key 认证（service principal）。
	APIKeyResolver APIKeyResolver
}

func MiddlewareConfigFromApp(cfg *config.Config) MiddlewareConfig {
	var out MiddlewareConfig
	if cfg == nil {
		return out
	}
	out.Secret = cfg.JWT.Secret
	out.RBAC = cfg.Security.RBAC
	return out
}

// AuthMiddleware enforces Bearer JWT auth and projects normalized claims into gin.Context.
func AuthMiddleware(cfg MiddlewareConfig) gin.HandlerFunc {
	validator := Validator{
		Secret: cfg.Secret,
		Now:    cfg.Now,
	}
	resolver := Resolver{RBAC: cfg.RBAC}

	return func(c *gin.Context) {
		if key := strings.TrimSpace(c.GetHeader("X-API-Key")); key != "" {
			handleAPIKeyAuth(c, cfg, key)
			return
		}
		ah := c.GetHeader("Authorization")
		if !strings.HasPrefix(strings.ToLower(ah), "bearer ") {
			abortJSON(c, http.StatusUnauthorized, "Unauthorized", "missing bearer token")
			return
		}
		token := strings.TrimSpace(ah[len("Bearer "):])
		if token == "" || validator.Secret == "" {
			abortJSON(c, http.StatusUnauthorized, "Unauthorized", "invalid token or server misconfig")
			return
		}

		payload, err := validator.ValidateToken(token)
		if err != nil {
			abortJSON(c, http.StatusUnauthorized, "Unauthorized", err.Error())
			return
		}
		// 挑战 token（2fa_challenge）与 refresh token 只对各自专用端点有效；
		// 这里黑名单式拒绝（历史 access token 无 token_use claim，不能要求严格相等）。
		switch tokenUse, _ := payload["token_use"].(string); tokenUse {
		case "", "access":
		default:
			abortJSON(c, http.StatusUnauthorized, "Unauthorized", "token is not an access token")
			return
		}

		claims := extractClaims(payload, resolver)
		now := time.Now()
		if cfg.Now != nil {
			now = cfg.Now()
		}
		if cfg.Policy != nil {
			if err := cfg.Policy(payload, claims, now); err != nil {
				abortJSON(c, http.StatusUnauthorized, "Unauthorized", err.Error())
				return
			}
		}
		reqCtx := ContextWithScope(c.Request.Context(), claims.TenantID, claims.WorkspaceID)
		c.Request = c.Request.WithContext(reqCtx)
		if claims.HasUserID {
			c.Set("user_id", claims.UserID)
		}
		if claims.UserIDRaw != nil {
			c.Set("user_id_raw", claims.UserIDRaw)
		}
		if len(claims.Roles) > 0 {
			c.Set("roles", claims.Roles)
		}
		if len(claims.Permissions) > 0 {
			c.Set("permissions", claims.Permissions)
		}
		c.Set("principal_kind", claims.PrincipalKind)
		if claims.TenantID != "" {
			c.Set("tenant_id", claims.TenantID)
		}
		if claims.WorkspaceID != "" {
			c.Set("workspace_id", claims.WorkspaceID)
		}
		if claims.SessionID != "" {
			c.Set("session_id", claims.SessionID)
		}
		c.Set("auth_claims", claims.Values)

		c.Next()
	}
}

// RequirePermissionsAny requires the caller to have at least one listed permission.
func RequirePermissionsAny(required ...string) gin.HandlerFunc {
	req := make([]string, 0, len(required))
	for _, r := range required {
		if s := strings.TrimSpace(r); s != "" {
			req = append(req, s)
		}
	}
	return func(c *gin.Context) {
		granted := getGrantedPermissions(c)
		for _, r := range req {
			if HasPermission(granted, r) {
				c.Next()
				return
			}
		}
		abortJSON(c, http.StatusForbidden, "Forbidden", "insufficient permission")
	}
}

// RequirePermissionsAll requires the caller to have all listed permissions.
func RequirePermissionsAll(required ...string) gin.HandlerFunc {
	req := make([]string, 0, len(required))
	for _, r := range required {
		if s := strings.TrimSpace(r); s != "" {
			req = append(req, s)
		}
	}
	return func(c *gin.Context) {
		granted := getGrantedPermissions(c)
		for _, r := range req {
			if !HasPermission(granted, r) {
				abortJSON(c, http.StatusForbidden, "Forbidden", "insufficient permission")
				return
			}
		}
		c.Next()
	}
}

// RequireResourcePermission maps HTTP methods to resource permissions.
func RequireResourcePermission(resource string) gin.HandlerFunc {
	resource = strings.TrimSpace(resource)
	return func(c *gin.Context) {
		perm := ResourcePermission(resource, c.Request.Method)
		RequirePermissionsAny(perm, resource+".*", "*")(c)
	}
}

// RequirePrincipalKinds requires the caller to match one of the normalized principal kinds.
func RequirePrincipalKinds(required ...string) gin.HandlerFunc {
	reqSet := make(map[string]struct{}, len(required))
	for _, r := range required {
		if kind := normalizePrincipalKind(r); kind != "" {
			reqSet[kind] = struct{}{}
		}
	}
	return func(c *gin.Context) {
		kind := getPrincipalKind(c)
		if _, ok := reqSet[kind]; ok {
			c.Next()
			return
		}
		abortJSON(c, http.StatusForbidden, "Forbidden", "insufficient principal type")
	}
}

// handleAPIKeyAuth X-API-Key 分支：解析凭据、注入只读 service principal 上下文。
// 失败统一 401（不区分密钥不存在/吊销/过期），原 JWT 路径不受影响。
func handleAPIKeyAuth(c *gin.Context, cfg MiddlewareConfig, key string) {
	if cfg.APIKeyResolver == nil {
		abortJSON(c, http.StatusUnauthorized, "Unauthorized", "api key auth not configured")
		return
	}
	record, err := cfg.APIKeyResolver.ResolveAPIKey(c.Request.Context(), key)
	if err != nil {
		abortJSON(c, http.StatusUnauthorized, "Unauthorized", "invalid api key")
		return
	}
	claims := Claims{
		Values: map[string]interface{}{
			"api_key_id":     record.ID,
			"principal_kind": PrincipalService,
		},
		Roles:         []string{},
		Permissions:   APIKeyScopesOrDefault(record.Scopes),
		PrincipalKind: PrincipalService,
		TenantID:      record.TenantID,
		WorkspaceID:   record.WorkspaceID,
	}
	reqCtx := ContextWithScope(c.Request.Context(), claims.TenantID, claims.WorkspaceID)
	c.Request = c.Request.WithContext(reqCtx)
	c.Set("permissions", claims.Permissions)
	c.Set("principal_kind", claims.PrincipalKind)
	if claims.TenantID != "" {
		c.Set("tenant_id", claims.TenantID)
	}
	if claims.WorkspaceID != "" {
		c.Set("workspace_id", claims.WorkspaceID)
	}
	c.Set("api_key_id", record.ID)
	c.Set("auth_claims", claims.Values)

	c.Next()
}

func getGrantedPermissions(c *gin.Context) []string {
	if v, ok := c.Get("permissions"); ok {
		if perms, ok := v.([]string); ok {
			return perms
		}
	}
	return nil
}

func getGrantedRoles(c *gin.Context) []string {
	if v, ok := c.Get("roles"); ok {
		switch t := v.(type) {
		case []string:
			return t
		case []interface{}:
			var out []string
			for _, it := range t {
				if s, ok := it.(string); ok {
					out = append(out, s)
				}
			}
			return out
		case string:
			if t != "" {
				return []string{t}
			}
		}
	}
	return nil
}

func getPrincipalKind(c *gin.Context) string {
	if v, ok := c.Get("principal_kind"); ok {
		if kind := normalizePrincipalKind(v); kind != "" {
			return kind
		}
	}
	return PrincipalUnknown
}

func abortJSON(c *gin.Context, status int, errCode, msg string) {
	c.AbortWithStatusJSON(status, gin.H{
		"error":   errCode,
		"message": msg,
	})
}
