package server

import (
	"bytes"
	"strings"
	"testing"

	"servify/apps/server/internal/config"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// TestBuildRouterLogsSecuritySurfaceWarnings 覆盖 BuildRouter 的告警日志
// 分支：装配出的路由全部位于安全目录内，真实警告不会出现，经 seam 注入。
func TestBuildRouterLogsSecuritySurfaceWarnings(t *testing.T) {
	previous := routeSecurityWarnings
	routeSecurityWarnings = func(routes gin.RoutesInfo, cfg *config.Config) []string {
		if len(routes) == 0 {
			t.Fatal("BuildRouter passed zero routes; expected assembled route table")
		}
		return []string{"injected-warning GET /public/raw-export"}
	}
	t.Cleanup(func() { routeSecurityWarnings = previous })

	buf := &bytes.Buffer{}
	logger := logrus.New()
	logger.Out = buf
	logger.Level = logrus.WarnLevel

	router := BuildRouter(Dependencies{
		Config:           config.GetDefaultConfig(),
		Logger:           logger,
		AIHandlerService: stubAIHandlerService{},
	})
	if router == nil {
		t.Fatal("BuildRouter() returned nil engine")
	}
	if !strings.Contains(buf.String(), "security surface warning: injected-warning GET /public/raw-export") {
		t.Fatalf("router log = %q, want injected security surface warning", buf.String())
	}
}
