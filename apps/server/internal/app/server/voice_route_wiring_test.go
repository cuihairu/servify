package server

import (
	"context"
	"strings"
	"testing"

	translationdelivery "servify/apps/server/internal/modules/translation/delivery"
	"servify/apps/server/internal/platform/eventbus"
	realtimeplatform "servify/apps/server/internal/platform/realtime"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// stubVoiceStarter 语音通道门面替身：装配面只关心"注入即注册路由"。
type stubVoiceStarter struct{}

func (stubVoiceStarter) StartAudioStream(context.Context, string, string, translationdelivery.VoiceStreamSink) (translationdelivery.VoiceAudioStream, error) {
	return nil, nil
}

func voiceRouteRegistered(router *gin.Engine) bool {
	for _, route := range router.Routes() {
		if route.Path == "/api/v1/ws/voice" {
			return true
		}
	}
	return false
}

// TestBuildRouterVoiceRouteGatedByRuntime 语音通道路由的装配门控：AI 面已
// 接线（VoiceTranslationRuntime 非 nil）才注册；ai.asr 未配置形态（runtime
// nil）或 hub 缺席都不注册。
func TestBuildRouterVoiceRouteGatedByRuntime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := testRouterConfig()

	t.Run("runtime wired registers route", func(t *testing.T) {
		router := BuildRouter(Dependencies{
			Config:                  cfg,
			Logger:                  logrus.New(),
			VoiceHub:                realtimeplatform.NewVoiceHub(),
			VoiceTranslationRuntime: stubVoiceStarter{},
		})
		if !voiceRouteRegistered(router) {
			t.Fatal("voice route must be registered when the voice runtime is wired")
		}
	})

	t.Run("runtime nil keeps route unregistered", func(t *testing.T) {
		router := BuildRouter(Dependencies{
			Config:   cfg,
			Logger:   logrus.New(),
			VoiceHub: realtimeplatform.NewVoiceHub(),
		})
		if voiceRouteRegistered(router) {
			t.Fatal("voice route must stay unregistered when ai.asr is not configured")
		}
	})

	t.Run("hub nil keeps route unregistered", func(t *testing.T) {
		router := BuildRouter(Dependencies{
			Config:                  cfg,
			Logger:                  logrus.New(),
			VoiceTranslationRuntime: stubVoiceStarter{},
		})
		if voiceRouteRegistered(router) {
			t.Fatal("voice route must stay unregistered without a voice hub")
		}
	})
}

// TestVoiceRouteCatalogedAsPublicSurface 语音通道是免认证建连面（与
// /api/v1/ws 同信任域）：必须进安全面目录，否则 RouteSecurityWarnings 会对
// 装配了 ai.asr 的部署持续告警"catalog is missing an entry"。
func TestVoiceRouteCatalogedAsPublicSurface(t *testing.T) {
	cfg := testRouterConfig()
	router := BuildRouter(Dependencies{
		Config:                  cfg,
		Logger:                  logrus.New(),
		VoiceHub:                realtimeplatform.NewVoiceHub(),
		VoiceTranslationRuntime: stubVoiceStarter{},
	})

	warnings := RouteSecurityWarnings(router.Routes(), cfg)
	for _, warning := range warnings {
		if strings.Contains(warning, "/api/v1/ws/voice") {
			t.Fatalf("voice channel must be cataloged, got warning %q", warning)
		}
	}

	catalogued := false
	for _, surface := range SecuritySurfaceCatalog(cfg) {
		if surface.Path == "/api/v1/ws/voice" {
			catalogued = surface.Exposure == "public" && surface.RequiresDedicatedRateLimit
		}
	}
	if !catalogued {
		t.Fatal("voice channel must be cataloged as a public rate-limited surface")
	}
}

// TestBuildRuntimeWiresVoiceChannelByASRConfig 装配面端到端：ai.asr.provider
// 非空 → VoiceTranslationRuntime 非 nil + 路由注册；空 provider（默认）→
// 两者都不装配（语音通道禁用形态）；ai.tts 未配置不影响字幕形态装配。
func TestBuildRuntimeWiresVoiceChannelByASRConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name      string
		asr       string
		tts       string
		wantVoice bool
	}{
		{name: "asr unconfigured", asr: "", tts: "", wantVoice: false},
		{name: "asr only caption form", asr: "openai", tts: "", wantVoice: true},
		{name: "asr and tts", asr: "openai", tts: "openai", wantVoice: true},
		{name: "unknown asr provider", asr: "not-a-provider", tts: "openai", wantVoice: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newRuntimeTestConfig(t)
			cfg.AI.ASR.Provider = tc.asr
			cfg.AI.ASR.APIKey = "test-key"
			cfg.AI.ASR.BaseURL = "http://127.0.0.1:1"
			cfg.AI.TTS.Provider = tc.tts
			cfg.AI.TTS.APIKey = "test-key"
			cfg.AI.TTS.BaseURL = "http://127.0.0.1:1"

			rt, err := BuildRuntime(cfg, logrus.New(), newRuntimeTestDB(t), nil, eventbus.NewInMemoryBus())
			if err != nil {
				t.Fatalf("BuildRuntime() error = %v", err)
			}
			defer func() { _ = rt.Stop(context.Background()) }()

			if got := rt.VoiceTranslationRuntime != nil; got != tc.wantVoice {
				t.Fatalf("voice runtime wired = %v, want %v", got, tc.wantVoice)
			}
			if got := voiceRouteRegistered(BuildRouter(rt.RouterDependencies())); got != tc.wantVoice {
				t.Fatalf("voice route registered = %v, want %v", got, tc.wantVoice)
			}
		})
	}
}
