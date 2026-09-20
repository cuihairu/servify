package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	mockkp "servify/apps/server/internal/platform/knowledgeprovider/mock"
	"servify/apps/server/internal/platform/llm/mock"

	"github.com/sirupsen/logrus"
)

// newKnowledgeSourceBaseAI 构造已初始化内部知识库的 base AI（不可达 LLM 地址即可）。
func newKnowledgeSourceBaseAI() *aidelivery.AIService {
	base := aidelivery.NewAIService("key", "http://127.0.0.1:1")
	base.InitializeKnowledgeBase()
	return base
}

func healthyWeKnoraConfig(url string) config.WeKnoraConfig {
	return config.WeKnoraConfig{
		Enabled:         true,
		BaseURL:         url,
		APIKey:          "wk-key",
		KnowledgeBaseID: "kb-1",
	}
}

// dify 健康检查失败（不可达）且 weknora 健康：选择链降级到 weknora。
func TestSelectKnowledgeSourceDifyUnhealthyFallsBackToWeKnora(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/health" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"healthy"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	source, err := selectKnowledgeSource(config.DifyConfig{
		Enabled:   true,
		BaseURL:   "http://127.0.0.1:1",
		DatasetID: "ds-missing",
	}, healthyWeKnoraConfig(srv.URL), knowledgeSourceOptions{
		checkHealth:     true,
		fallbackEnabled: true,
		healthTimeout:   500 * time.Millisecond,
		logger:          logrus.New(),
	})
	if err != nil {
		t.Fatalf("selectKnowledgeSource() error = %v", err)
	}
	if source.id != "weknora" || !source.present() {
		t.Fatalf("expected weknora fallback, got id=%q", source.id)
	}
}

// dify 健康检查失败且 weknora 未启用、未 require：降级为无知识源（不报错）。
func TestSelectKnowledgeSourceDifyUnhealthyNoWeKnoraDegrades(t *testing.T) {
	source, err := selectKnowledgeSource(config.DifyConfig{
		Enabled:   true,
		BaseURL:   "http://127.0.0.1:1",
		DatasetID: "ds-missing",
	}, config.WeKnoraConfig{}, knowledgeSourceOptions{
		checkHealth:     true,
		fallbackEnabled: true,
		healthTimeout:   500 * time.Millisecond,
		logger:          logrus.New(),
	})
	if err != nil {
		t.Fatalf("selectKnowledgeSource() error = %v", err)
	}
	if source.present() {
		t.Fatalf("expected empty source, got %+v", source)
	}
}

// 请求级（checkHealth=false）跳过健康探测：dify 不可达也纯构造入选。
func TestSelectKnowledgeSourceRequestLevelSkipsHealth(t *testing.T) {
	source, err := selectKnowledgeSource(config.DifyConfig{
		Enabled:   true,
		BaseURL:   "http://127.0.0.1:1",
		DatasetID: "ds-x",
	}, config.WeKnoraConfig{Enabled: true, BaseURL: "http://127.0.0.1:1"}, knowledgeSourceOptions{
		checkHealth: false,
		logger:      logrus.New(),
	})
	if err != nil {
		t.Fatalf("selectKnowledgeSource() error = %v", err)
	}
	if source.id != "dify" || !source.present() {
		t.Fatalf("expected dify source without health probe, got id=%q", source.id)
	}
}

// nil logger 防御：opts.logger 缺省时回退 StandardLogger，选择链不 panic。
func TestSelectKnowledgeSourceNilLoggerDefaults(t *testing.T) {
	source, err := selectKnowledgeSource(config.DifyConfig{}, config.WeKnoraConfig{}, knowledgeSourceOptions{
		checkHealth: false,
	})
	if err != nil || source.present() {
		t.Fatalf("expected empty source without error, got (%+v, %v)", source, err)
	}
}

// buildOrchestrated 把 source 身份透传到编排服务状态标签；空 source 即无知识源。
func TestKnowledgeSourceBuildOrchestrated(t *testing.T) {
	if (knowledgeSource{}).present() {
		t.Fatal("empty source should not be present")
	}
	ctx := context.Background()
	base := newKnowledgeSourceBaseAI()
	logger := logrus.New()

	empty := knowledgeSource{}.buildOrchestrated(base, &mock.Provider{}, logger)
	status := empty.GetStatus(ctx)
	if enabled, _ := status["knowledge_provider_enabled"].(bool); enabled {
		t.Fatalf("expected provider disabled for empty source, got %+v", status)
	}

	// weknora source：driver 与 id 同时就位，状态标签透传 provider 身份。
	source := knowledgeSource{driver: &mockkp.Provider{}, id: "weknora"}
	svc := source.buildOrchestrated(base, &mock.Provider{}, logger)
	if enabled, _ := svc.GetStatus(ctx)["knowledge_provider_enabled"].(bool); !enabled {
		t.Fatalf("expected provider enabled for weknora source")
	}
	if provider, _ := svc.GetStatus(ctx)["knowledge_provider"].(string); provider != "weknora" {
		t.Fatalf("provider label = %q want weknora", provider)
	}
}
