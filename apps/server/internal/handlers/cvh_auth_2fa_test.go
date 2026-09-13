package handlers

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

var _ auth2FAService = (*cvh2FAService)(nil)

// cvh2FAService 两步验证服务 inline mock。
type cvh2FAService struct {
	setup      *services.TwoFactorSetup
	setupErr   error
	enable     []string
	enableErr  error
	disableErr error
	regen      []string
	regenErr   error
	remaining  int64
	remainErr  error
	verify     *services.AuthResult
	verifyErr  error

	setupUID     uint
	enableUID    uint
	enableSecret string
	enableCode   string
	regenUID     uint
	regenCode    string
	verifyToken  string
	verifyCode   string
}

func (s *cvh2FAService) SetupTwoFactor(_ context.Context, userID uint) (*services.TwoFactorSetup, error) {
	s.setupUID = userID
	return s.setup, s.setupErr
}

func (s *cvh2FAService) EnableTwoFactor(_ context.Context, userID uint, secret, code string) ([]string, error) {
	s.enableUID = userID
	s.enableSecret = secret
	s.enableCode = code
	return s.enable, s.enableErr
}

func (s *cvh2FAService) DisableTwoFactor(_ context.Context, userID uint, password, code string) error {
	s.disableErr = s.disableErr
	return s.disableErr
}

func (s *cvh2FAService) RegenerateRecoveryCodes(_ context.Context, userID uint, code string) ([]string, error) {
	s.regenUID = userID
	s.regenCode = code
	return s.regen, s.regenErr
}

func (s *cvh2FAService) RecoveryCodesRemaining(_ context.Context, userID uint) (int64, error) {
	return s.remaining, s.remainErr
}

func (s *cvh2FAService) VerifyTwoFactorLogin(_ context.Context, req services.TwoFactorVerifyInput, _ services.AuthSessionMetadata) (*services.AuthResult, error) {
	s.verifyToken = req.ChallengeToken
	s.verifyCode = req.Code
	return s.verify, s.verifyErr
}

func cvh2FARouter(svc *cvh2FAService, authed bool) *gin.Engine {
	r := dxcRouter()
	h := NewAuth2FAHandler(svc)
	r.POST("/2fa/verify", h.VerifyLogin)
	g := r.Group("/2fa")
	if authed {
		g.Use(func(c *gin.Context) { c.Set("user_id", uint(7)); c.Next() })
	}
	g.POST("/setup", h.Setup)
	g.POST("/enable", h.Enable)
	g.POST("/disable", h.Disable)
	g.GET("/recovery-codes", h.RecoveryCodes)
	g.POST("/recovery-codes/regenerate", h.RegenerateRecoveryCodes)
	return r
}

func cvh2FAVerifyResult() *services.AuthResult {
	return &services.AuthResult{
		Token:            "tok-2fa",
		ExpiresIn:        3600,
		RefreshToken:     "rt-2fa",
		RefreshExpiresIn: 86400,
		User:             &models.User{ID: 7, Username: "u7", TotpEnabled: true},
	}
}

func TestCvh2FAVerifyLogin(t *testing.T) {
	path := "/2fa/verify"

	t.Run("success", func(t *testing.T) {
		svc := &cvh2FAService{verify: cvh2FAVerifyResult()}
		body := `{"challenge_token":"ch-1","code":"123456"}`
		w := dxcDo(cvh2FARouter(svc, false), http.MethodPost, path, body)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "ch-1", svc.verifyToken)
		assert.Equal(t, "123456", svc.verifyCode)
		var resp struct {
			Token        string `json:"token"`
			ExpiresIn    int    `json:"expires_in"`
			RefreshToken string `json:"refresh_token"`
			User         struct {
				ID          uint   `json:"id"`
				Username    string `json:"username"`
				TotpEnabled bool   `json:"totp_enabled"`
			} `json:"user"`
		}
		dxcDecode(t, w, &resp)
		assert.Equal(t, "tok-2fa", resp.Token)
		assert.Equal(t, 3600, resp.ExpiresIn)
		assert.Equal(t, "rt-2fa", resp.RefreshToken)
		assert.Equal(t, uint(7), resp.User.ID)
		assert.True(t, resp.User.TotpEnabled)
	})

	t.Run("invalid json", func(t *testing.T) {
		w := dxcDo(cvh2FARouter(&cvh2FAService{}, false), http.MethodPost, path, `{`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "请求参数无效")
	})

	errorCases := []struct {
		name string
		err  error
		want int
		msg  string
	}{
		{"invalid challenge", services.ErrAuthInvalid2FAChallenge, http.StatusUnauthorized, "登录挑战已失效"},
		{"invalid code", services.ErrAuthInvalid2FACode, http.StatusUnauthorized, "验证码错误"},
		{"user disabled", services.ErrAuthUserDisabled, http.StatusForbidden, "账号已被禁用"},
		{"generic", errors.New("boom"), http.StatusInternalServerError, "验证失败"},
	}
	for _, tc := range errorCases {
		t.Run("error "+tc.name, func(t *testing.T) {
			svc := &cvh2FAService{verifyErr: tc.err}
			w := dxcDo(cvh2FARouter(svc, false), http.MethodPost, path, `{"challenge_token":"ch-1","code":"000000"}`)
			assert.Equal(t, tc.want, w.Code)
			assert.Contains(t, w.Body.String(), tc.msg)
		})
	}
}

func TestCvh2FASetup(t *testing.T) {
	t.Run("unauthorized without user", func(t *testing.T) {
		w := dxcDo(cvh2FARouter(&cvh2FAService{}, false), http.MethodPost, "/2fa/setup", "")
		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "无效的 Token")
	})

	t.Run("success", func(t *testing.T) {
		svc := &cvh2FAService{setup: &services.TwoFactorSetup{Secret: "S3CRET", OTPAuthURI: "otpauth://totp/x"}}
		w := dxcDo(cvh2FARouter(svc, true), http.MethodPost, "/2fa/setup", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, uint(7), svc.setupUID)
		assert.Contains(t, w.Body.String(), "S3CRET")
		assert.Contains(t, w.Body.String(), "otpauth://totp/x")
	})

	errorCases := []struct {
		name string
		err  error
		want int
		msg  string
	}{
		{"disabled by admin", services.ErrTwoFactorDisabled, http.StatusForbidden, "两步验证已被管理员关闭"},
		{"already enabled", services.ErrTwoFactorAlreadyEnabled, http.StatusBadRequest, "已启用两步验证"},
		{"generic", errors.New("boom"), http.StatusInternalServerError, "操作失败"},
	}
	for _, tc := range errorCases {
		t.Run("error "+tc.name, func(t *testing.T) {
			svc := &cvh2FAService{setupErr: tc.err}
			w := dxcDo(cvh2FARouter(svc, true), http.MethodPost, "/2fa/setup", "")
			assert.Equal(t, tc.want, w.Code)
			assert.Contains(t, w.Body.String(), tc.msg)
		})
	}
}

func TestCvh2FAEnable(t *testing.T) {
	path := "/2fa/enable"

	t.Run("unauthorized without user", func(t *testing.T) {
		w := dxcDo(cvh2FARouter(&cvh2FAService{}, false), http.MethodPost, path, `{"secret":"s","code":"1"}`)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("success", func(t *testing.T) {
		svc := &cvh2FAService{enable: []string{"AAAA-BBBB", "CCCC-DDDD"}}
		w := dxcDo(cvh2FARouter(svc, true), http.MethodPost, path, `{"secret":"S3CRET","code":"123456"}`)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, uint(7), svc.enableUID)
		assert.Equal(t, "S3CRET", svc.enableSecret)
		assert.Equal(t, "123456", svc.enableCode)
		assert.Contains(t, w.Body.String(), "AAAA-BBBB")
		assert.Contains(t, w.Body.String(), "CCCC-DDDD")
	})

	t.Run("invalid json", func(t *testing.T) {
		w := dxcDo(cvh2FARouter(&cvh2FAService{}, true), http.MethodPost, path, `{`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "请求参数无效")
	})

	errorCases := []struct {
		name string
		err  error
		want int
		msg  string
	}{
		{"invalid code", services.ErrAuthInvalid2FACode, http.StatusUnauthorized, "验证码错误"},
		{"invalid input", services.ErrInvalidAuthInput, http.StatusBadRequest, "请求参数无效"},
		{"generic", errors.New("boom"), http.StatusInternalServerError, "操作失败"},
	}
	for _, tc := range errorCases {
		t.Run("error "+tc.name, func(t *testing.T) {
			svc := &cvh2FAService{enableErr: tc.err}
			w := dxcDo(cvh2FARouter(svc, true), http.MethodPost, path, `{"secret":"s","code":"1"}`)
			assert.Equal(t, tc.want, w.Code)
			assert.Contains(t, w.Body.String(), tc.msg)
		})
	}
}

func TestCvh2FADisable(t *testing.T) {
	path := "/2fa/disable"

	t.Run("unauthorized without user", func(t *testing.T) {
		w := dxcDo(cvh2FARouter(&cvh2FAService{}, false), http.MethodPost, path, `{"password":"p"}`)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("success", func(t *testing.T) {
		svc := &cvh2FAService{}
		w := dxcDo(cvh2FARouter(svc, true), http.MethodPost, path, `{"password":"pw","code":"123456"}`)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "两步验证已解除")
	})

	t.Run("invalid json", func(t *testing.T) {
		w := dxcDo(cvh2FARouter(&cvh2FAService{}, true), http.MethodPost, path, `{`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	errorCases := []struct {
		name string
		err  error
		want int
		msg  string
	}{
		{"not enabled", services.ErrTwoFactorNotEnabled, http.StatusBadRequest, "尚未启用两步验证"},
		{"wrong password", services.ErrAuthInvalidCredentials, http.StatusUnauthorized, "密码错误"},
		{"wrong code", services.ErrAuthInvalid2FACode, http.StatusUnauthorized, "验证码错误"},
		{"generic", errors.New("boom"), http.StatusInternalServerError, "解绑失败"},
	}
	for _, tc := range errorCases {
		t.Run("error "+tc.name, func(t *testing.T) {
			svc := &cvh2FAService{disableErr: tc.err}
			w := dxcDo(cvh2FARouter(svc, true), http.MethodPost, path, `{"password":"pw","code":"1"}`)
			assert.Equal(t, tc.want, w.Code)
			assert.Contains(t, w.Body.String(), tc.msg)
		})
	}
}

func TestCvh2FARecoveryCodes(t *testing.T) {
	t.Run("unauthorized without user", func(t *testing.T) {
		w := dxcDo(cvh2FARouter(&cvh2FAService{}, false), http.MethodGet, "/2fa/recovery-codes", "")
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("success", func(t *testing.T) {
		svc := &cvh2FAService{remaining: 3}
		w := dxcDo(cvh2FARouter(svc, true), http.MethodGet, "/2fa/recovery-codes", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"remaining":3`)
	})

	t.Run("service error", func(t *testing.T) {
		svc := &cvh2FAService{remainErr: errors.New("boom")}
		w := dxcDo(cvh2FARouter(svc, true), http.MethodGet, "/2fa/recovery-codes", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "查询失败")
	})
}

func TestCvh2FARegenerateRecoveryCodes(t *testing.T) {
	path := "/2fa/recovery-codes/regenerate"

	t.Run("unauthorized without user", func(t *testing.T) {
		w := dxcDo(cvh2FARouter(&cvh2FAService{}, false), http.MethodPost, path, `{"code":"1"}`)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("success", func(t *testing.T) {
		svc := &cvh2FAService{regen: []string{"EEEE-FFFF"}}
		w := dxcDo(cvh2FARouter(svc, true), http.MethodPost, path, `{"code":"123456"}`)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, uint(7), svc.regenUID)
		assert.Equal(t, "123456", svc.regenCode)
		assert.Contains(t, w.Body.String(), "EEEE-FFFF")
	})

	t.Run("invalid json", func(t *testing.T) {
		w := dxcDo(cvh2FARouter(&cvh2FAService{}, true), http.MethodPost, path, `{`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	errorCases := []struct {
		name string
		err  error
		want int
		msg  string
	}{
		{"not enabled", services.ErrTwoFactorNotEnabled, http.StatusBadRequest, "尚未启用两步验证"},
		{"invalid code", services.ErrAuthInvalid2FACode, http.StatusUnauthorized, "验证码错误"},
		{"disabled by admin", services.ErrTwoFactorDisabled, http.StatusForbidden, "两步验证已被管理员关闭"},
		{"generic", errors.New("boom"), http.StatusInternalServerError, "重置失败"},
	}
	for _, tc := range errorCases {
		t.Run("error "+tc.name, func(t *testing.T) {
			svc := &cvh2FAService{regenErr: tc.err}
			w := dxcDo(cvh2FARouter(svc, true), http.MethodPost, path, `{"code":"1"}`)
			assert.Equal(t, tc.want, w.Code)
			assert.Contains(t, w.Body.String(), tc.msg)
		})
	}
}
