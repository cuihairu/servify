package server

import (
	"strings"
	"testing"

	"servify/apps/server/internal/config"
	translationdelivery "servify/apps/server/internal/modules/translation/delivery"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

func quotaTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestBuildTranslationQuotaGateDisabledOrBare(t *testing.T) {
	next := translationdelivery.NewTranslationHandlerService(nil, translationdelivery.RuntimeParams{})
	logger := logrus.New()

	// quota 关闭：原样返回（零行为）。
	cfg := &config.Config{}
	got, err := buildTranslationQuotaGate(cfg, nil, next, logger)
	if err != nil || got != next {
		t.Fatalf("disabled quota = (%v, %v), want bare next", got, err)
	}
	// cfg 为 nil / next 为 nil：同样原样返回。
	if got, err := buildTranslationQuotaGate(nil, nil, next, logger); err != nil || got != next {
		t.Fatalf("nil cfg = (%v, %v), want bare next", got, err)
	}
	if got, err := buildTranslationQuotaGate(cfg, nil, nil, logger); err != nil || got != nil {
		t.Fatalf("nil next = (%v, %v), want nil", got, err)
	}
}

func TestBuildTranslationQuotaGateValidation(t *testing.T) {
	next := translationdelivery.NewTranslationHandlerService(nil, translationdelivery.RuntimeParams{})
	logger := logrus.New()
	client := quotaTestRedis(t)

	cases := []struct {
		name string
		unit string
		win  string
		limit int64
		redis bool
		wantSub string
	}{
		{"bad unit", "chars", "day", 100, true, "translation.quota.unit"},
		{"bad window", "tokens", "week", 100, true, "translation.quota.window"},
		{"zero limit", "tokens", "day", 0, true, "translation.quota.limit"},
		{"negative limit", "requests", "hour", -1, true, "translation.quota.limit"},
		{"missing redis", "tokens", "day", 100, false, "requires redis"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Translation.Quota = config.TranslationQuotaConfig{
				Enabled: true, Limit: tc.limit, Unit: tc.unit, Window: tc.win,
			}
			rc := client
			if !tc.redis {
				rc = nil
			}
			got, err := buildTranslationQuotaGate(cfg, rc, next, logger)
			if err == nil || got != nil {
				t.Fatalf("expected startup error, got (%v, %v)", got, err)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q must mention %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestBuildTranslationQuotaGateEnabledWraps(t *testing.T) {
	next := translationdelivery.NewTranslationHandlerService(nil, translationdelivery.RuntimeParams{})
	cfg := &config.Config{}
	cfg.Translation.Quota = config.TranslationQuotaConfig{
		Enabled: true, Limit: 42, Unit: "requests", Window: "day",
	}
	got, err := buildTranslationQuotaGate(cfg, quotaTestRedis(t), next, logrus.New())
	if err != nil {
		t.Fatalf("valid config must assemble: %v", err)
	}
	if got == nil || got == next {
		t.Fatal("enabled quota must wrap next with a gate")
	}
}
