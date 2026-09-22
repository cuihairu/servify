package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

// healthyRagFlowConfig 返回指向指定 mock 的 ragflow 配置。
func healthyRagFlowConfig(url string) config.RagFlowConfig {
	return config.RagFlowConfig{
		Enabled:   true,
		BaseURL:   url,
		APIKey:    "ragflow-key",
		DatasetID: "ds-rf",
	}
}

// newHealthyRagFlowServer 起一个 dataset 探测通过的 RAGFlow mock（统一包裹体）。
func newHealthyRagFlowServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/datasets" && r.URL.Query().Get("id") == "ds-rf" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":[{"id":"ds-rf","name":"RF KB"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
}

func unreachableDifyConfig() config.DifyConfig {
	return config.DifyConfig{Enabled: true, BaseURL: "http://127.0.0.1:1", APIKey: "dify-key", DatasetID: "ds-1"}
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

	source, err := selectKnowledgeSource(config.RagFlowConfig{}, config.DifyConfig{
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
	source, err := selectKnowledgeSource(config.RagFlowConfig{}, config.DifyConfig{
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
	source, err := selectKnowledgeSource(config.RagFlowConfig{}, config.DifyConfig{
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

// ragflow + dify 均启用且健康：选择链最高优先级归 ragflow。
func TestSelectKnowledgeSourceRagFlowWinsOverHealthyDify(t *testing.T) {
	rfSrv := newHealthyRagFlowServer()
	defer rfSrv.Close()
	difySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/datasets/ds-1" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"ds-1","name":"Dify KB"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer difySrv.Close()

	source, err := selectKnowledgeSource(healthyRagFlowConfig(rfSrv.URL), config.DifyConfig{
		Enabled:   true,
		BaseURL:   difySrv.URL,
		APIKey:    "dify-key",
		DatasetID: "ds-1",
	}, healthyWeKnoraConfig("http://127.0.0.1:1"), knowledgeSourceOptions{
		checkHealth:   true,
		healthTimeout: 500 * time.Millisecond,
		logger:        logrus.New(),
	})
	if err != nil {
		t.Fatalf("selectKnowledgeSource() error = %v", err)
	}
	if source.id != "ragflow" || !source.present() {
		t.Fatalf("expected ragflow to win, got id=%q", source.id)
	}
}

// ragflow 健康检查失败且 dify 健康：降级到 dify 分支。
func TestSelectKnowledgeSourceRagFlowUnhealthyFallsBackToDify(t *testing.T) {
	difySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/datasets/ds-1" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"ds-1","name":"Dify KB"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer difySrv.Close()

	source, err := selectKnowledgeSource(config.RagFlowConfig{
		Enabled:   true,
		BaseURL:   "http://127.0.0.1:1",
		APIKey:    "ragflow-key",
		DatasetID: "ds-rf",
	}, config.DifyConfig{
		Enabled:   true,
		BaseURL:   difySrv.URL,
		APIKey:    "dify-key",
		DatasetID: "ds-1",
	}, config.WeKnoraConfig{}, knowledgeSourceOptions{
		checkHealth:   true,
		healthTimeout: 500 * time.Millisecond,
		logger:        logrus.New(),
	})
	if err != nil {
		t.Fatalf("selectKnowledgeSource() error = %v", err)
	}
	if source.id != "dify" || !source.present() {
		t.Fatalf("expected dify fallback, got id=%q", source.id)
	}
}

// ragflow 健康检查失败且仅 weknora 启用：直接落 weknora 分支（不被 require 拦截）。
func TestSelectKnowledgeSourceRagFlowUnhealthyFallsToWeKnora(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/health" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"healthy"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	source, err := selectKnowledgeSource(config.RagFlowConfig{
		Enabled:   true,
		BaseURL:   "http://127.0.0.1:1",
		APIKey:    "ragflow-key",
		DatasetID: "ds-rf",
	}, config.DifyConfig{}, healthyWeKnoraConfig(srv.URL), knowledgeSourceOptions{
		checkHealth:     true,
		fallbackEnabled: true,
		healthTimeout:   500 * time.Millisecond,
		logger:          logrus.New(),
	})
	if err != nil {
		t.Fatalf("selectKnowledgeSource() error = %v", err)
	}
	if source.id != "weknora" || !source.present() {
		t.Fatalf("expected weknora source, got id=%q", source.id)
	}
}

// ragflow 健康检查失败且 dify/weknora 均关 + requireHealthy：启动失败，错误前缀逐字。
func TestSelectKnowledgeSourceRagFlowUnhealthyRequireFails(t *testing.T) {
	_, err := selectKnowledgeSource(config.RagFlowConfig{
		Enabled:   true,
		BaseURL:   "http://127.0.0.1:1",
		APIKey:    "ragflow-key",
		DatasetID: "ds-rf",
	}, config.DifyConfig{}, config.WeKnoraConfig{}, knowledgeSourceOptions{
		checkHealth:    true,
		requireHealthy: true,
		healthTimeout:  500 * time.Millisecond,
		logger:         logrus.New(),
	})
	if err == nil || !strings.Contains(err.Error(), "ragflow health check failed:") {
		t.Fatalf("expected ragflow health failure, got %v", err)
	}
}

// ragflow 健康检查失败 + 无 require + fallbackEnabled：降级为无知识源（不报错）。
func TestSelectKnowledgeSourceRagFlowUnhealthyNoRequireDegrades(t *testing.T) {
	source, err := selectKnowledgeSource(config.RagFlowConfig{
		Enabled:   true,
		BaseURL:   "http://127.0.0.1:1",
		APIKey:    "ragflow-key",
		DatasetID: "ds-rf",
	}, config.DifyConfig{}, config.WeKnoraConfig{}, knowledgeSourceOptions{
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

// 请求级（checkHealth=false）：ragflow 不可达也纯构造入选并压过启用的 dify。
func TestSelectKnowledgeSourceRequestLevelConstructsRagFlow(t *testing.T) {
	source, err := selectKnowledgeSource(healthyRagFlowConfig("http://127.0.0.1:1"), unreachableDifyConfig(), config.WeKnoraConfig{}, knowledgeSourceOptions{
		checkHealth: false,
		logger:      logrus.New(),
	})
	if err != nil {
		t.Fatalf("selectKnowledgeSource() error = %v", err)
	}
	if source.id != "ragflow" || !source.present() {
		t.Fatalf("expected ragflow source without health probe, got id=%q", source.id)
	}
}

// nil logger 防御：opts.logger 缺省时回退 StandardLogger，选择链不 panic。
func TestSelectKnowledgeSourceNilLoggerDefaults(t *testing.T) {
	source, err := selectKnowledgeSource(config.RagFlowConfig{}, config.DifyConfig{}, config.WeKnoraConfig{}, knowledgeSourceOptions{
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

	empty := knowledgeSource{}.buildOrchestrated(base, &mock.Provider{}, aidelivery.AIRuntimeParams{}, logger)
	status := empty.GetStatus(ctx)
	if enabled, _ := status["knowledge_provider_enabled"].(bool); enabled {
		t.Fatalf("expected provider disabled for empty source, got %+v", status)
	}

	// weknora source：driver 与 id 同时就位，状态标签透传 provider 身份。
	source := knowledgeSource{driver: &mockkp.Provider{}, id: "weknora"}
	svc := source.buildOrchestrated(base, &mock.Provider{}, aidelivery.AIRuntimeParams{}, logger)
	if enabled, _ := svc.GetStatus(ctx)["knowledge_provider_enabled"].(bool); !enabled {
		t.Fatalf("expected provider enabled for weknora source")
	}
	if provider, _ := svc.GetStatus(ctx)["knowledge_provider"].(string); provider != "weknora" {
		t.Fatalf("provider label = %q want weknora", provider)
	}
}
