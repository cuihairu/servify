package metrics

// consistency_test.go 是 P2-4 的可运维门禁：告警规则（rules.yaml）、Grafana
// dashboard（*.json）、runbook（operational-runbook.md）三者必须与代码中
// 实际注册的指标一致，未接线指标（known-gaps.md）禁止出现在任何 PromQL
// 表达式里。任何一侧漂移，本测试失败。
//
// 文件相对本包的路径：repo 根在向上五级。

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"servify/apps/server/internal/observability/telemetry"

	"gopkg.in/yaml.v3"
)

// repoRoot 指向仓库根（apps/server/internal/observability/metrics 起向上五级）。
const repoRoot = "../../../../../"

// allMetricNames 是 telemetry 定义的指标名全集（事实源：names.go）。
var allMetricNames = []string{
	telemetry.MetricHTTPRequestTotal,
	telemetry.MetricHTTPRequestDuration,
	telemetry.MetricHTTPResponseSize,
	telemetry.MetricConversationsCreated,
	telemetry.MetricTicketsCreated,
	telemetry.MetricTicketsResolved,
	telemetry.MetricRoutingDecisions,
	telemetry.MetricAIRequestsTotal,
	telemetry.MetricAIRequestDuration,
	telemetry.MetricAILLMTokenUsage,
	telemetry.MetricRateLimitDropped,
	telemetry.MetricEventBusPublished,
	telemetry.MetricEventBusHandled,
	telemetry.MetricEventBusFailed,
	telemetry.MetricEventBusDuration,
	telemetry.MetricEventBusDeadLetter,
	telemetry.MetricWorkerJobsTotal,
	telemetry.MetricWorkerJobDuration,
	telemetry.MetricWorkerActiveJobs,
	telemetry.MetricErrorsTotal,
}

// builtinMetricNames 是 runtime collectors 直接产出、不经 telemetry 常量的指标。
var builtinMetricNames = []string{
	"go_goroutines",
	"process_resident_memory_bytes",
}

// promQLIdents 里允许出现的非指标标识符（聚合函数与保留字）。
var promQLFunctions = map[string]bool{
	"sum": true, "rate": true, "histogram_quantile": true, "by": true,
	"without": true, "avg": true, "count": true, "max": true, "min": true,
	"irate": true, "increase": true, "avg_over_time": true, "le": true,
	"and": true, "or": true, "unless": true, "on": true, "ignoring": true,
	"group_left": true, "group_right": true, "offset": true, "bool": true,
}

var (
	metricConstRe  = regexp.MustCompile(`Metric[A-Za-z0-9_]+\s*=\s*"([a-z_]+)"`)
	gapLineRe      = regexp.MustCompile(`(?m)^-\s*metric:\s*([a-z_][a-z0-9_]*)`)
	runbookAlertRe = regexp.MustCompile(`(?m)^###\s+([A-Za-z0-9]+)\s*$`)
)

// knownGaps 读取 known-gaps.md 登记的未接线指标名集合。
func knownGaps(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(repoRoot + "deploy/observability/known-gaps.md")
	if err != nil {
		t.Fatalf("read known-gaps.md: %v", err)
	}
	gaps := make(map[string]bool)
	for _, m := range gapLineRe.FindAllStringSubmatch(string(raw), -1) {
		gaps[m[1]] = true
	}
	if len(gaps) == 0 {
		t.Fatal("known-gaps.md 解析出 0 个条目，格式约定（- metric: <name>）可能被破坏")
	}
	return gaps
}

// exprMetricNames 从 PromQL 表达式提取被引用的指标名：剔除 label selector
// `{...}`、分组修饰 `by (...)`、范围与数字字面量后，剩余的非函数标识符。
func exprMetricNames(expr string) []string {
	cleaned := regexp.MustCompile(`\{[^}]*\}`).ReplaceAllString(expr, " ")
	cleaned = regexp.MustCompile(`(?i)\b(?:by|without)\s*\([^)]*\)`).ReplaceAllString(cleaned, " ")
	cleaned = regexp.MustCompile(`\d+[smhd]`).ReplaceAllString(cleaned, " ")
	var names []string
	for _, tok := range regexp.MustCompile(`[a-zA-Z_][a-zA-Z0-9_]*`).FindAllString(cleaned, -1) {
		if promQLFunctions[tok] {
			continue
		}
		// histogram 自动后缀（_bucket/_sum/_count）归一到基础指标名。
		tok = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(tok, "_bucket"), "_sum"), "_count")
		names = append(names, tok)
	}
	return names
}

type alertRule struct {
	Alert string `yaml:"alert"`
	Expr  string `yaml:"expr"`
}

type alertGroup struct {
	Name  string      `yaml:"name"`
	Rules []alertRule `yaml:"rules"`
}

type alertFile struct {
	Groups []alertGroup `yaml:"groups"`
}

func loadAlerts(t *testing.T) (alertNames []string, exprs []string) {
	t.Helper()
	raw, err := os.ReadFile(repoRoot + "deploy/observability/alerts/rules.yaml")
	if err != nil {
		t.Fatalf("read rules.yaml: %v", err)
	}
	var af alertFile
	if err := yaml.Unmarshal(raw, &af); err != nil {
		t.Fatalf("parse rules.yaml: %v", err)
	}
	for _, g := range af.Groups {
		if len(g.Rules) == 0 {
			t.Fatalf("告警组 %s 没有 rules（空组应整组删除）", g.Name)
		}
		for _, r := range g.Rules {
			alertNames = append(alertNames, r.Alert)
			exprs = append(exprs, r.Expr)
		}
	}
	if len(alertNames) == 0 {
		t.Fatal("rules.yaml 未解析出任何告警")
	}
	return alertNames, exprs
}

func loadDashboardExprs(t *testing.T) []string {
	t.Helper()
	var exprs []string
	for _, name := range []string{
		"deploy/observability/dashboards/servify-service.json",
		"deploy/observability/dashboards/servify-business.json",
	} {
		raw, err := os.ReadFile(repoRoot + name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var dash struct {
			Panels []struct {
				Title   string `json:"title"`
				Targets []struct {
					Expr string `json:"expr"`
				} `json:"targets"`
			} `json:"panels"`
		}
		if err := json.Unmarshal(raw, &dash); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if len(dash.Panels) == 0 {
			t.Fatalf("%s 没有 panels", name)
		}
		for _, p := range dash.Panels {
			for _, tgt := range p.Targets {
				if strings.TrimSpace(tgt.Expr) == "" {
					t.Fatalf("面板 %q（%s）存在空 expr", p.Title, name)
				}
				exprs = append(exprs, tgt.Expr)
			}
		}
	}
	return exprs
}

// TestTelemetryNamesCovered 防止 names.go 新增常量后全集列表漏更。
func TestTelemetryNamesCovered(t *testing.T) {
	raw, err := os.ReadFile("../telemetry/names.go")
	if err != nil {
		t.Fatalf("read names.go: %v", err)
	}
	listed := make(map[string]bool)
	for _, name := range allMetricNames {
		listed[name] = true
	}
	for _, m := range metricConstRe.FindAllStringSubmatch(string(raw), -1) {
		if !listed[m[1]] {
			t.Errorf("names.go 定义了 %q 但 consistency 全集未收录，请在 allMetricNames 补上", m[1])
		}
	}
}

// TestAlertAndDashboardExprsReferenceRegisteredMetrics 断言所有 PromQL
// 表达式只引用已注册（telemetry 全集 ∪ runtime 白名单）且未登记为 gap 的指标。
func TestAlertAndDashboardExprsReferenceRegisteredMetrics(t *testing.T) {
	allowed := make(map[string]bool)
	for _, name := range append(append([]string{}, allMetricNames...), builtinMetricNames...) {
		allowed[name] = true
	}
	gaps := knownGaps(t)

	_, alertExprs := loadAlerts(t)
	for i, expr := range append(alertExprs, loadDashboardExprs(t)...) {
		for _, name := range exprMetricNames(expr) {
			if !allowed[name] {
				t.Errorf("expr 引用了未注册指标 %q（expr #%d: %s）", name, i, strings.TrimSpace(expr))
			}
			if gaps[name] {
				t.Errorf("expr 引用了 known-gaps 指标 %q——未接线指标查不出数据，会形成永真沉默的告警/死面板（expr #%d: %s）", name, i, strings.TrimSpace(expr))
			}
		}
	}
}

// TestKnownGapsAreRegisteredMetrics 断言 gaps 登记的名字都真实存在，
// 防止拼写错误把指标永久"藏"在 gaps 里。
func TestKnownGapsAreRegisteredMetrics(t *testing.T) {
	listed := make(map[string]bool)
	for _, name := range allMetricNames {
		listed[name] = true
	}
	for name := range knownGaps(t) {
		if !listed[name] {
			t.Errorf("known-gaps.md 登记的 %q 不在 telemetry 指标全集里（拼写错误？）", name)
		}
	}
}

// TestRunbookAlertSectionsMatchRules 断言 runbook 的告警处置段与
// rules.yaml 的告警集合一一对应（不多、不少）。
func TestRunbookAlertSectionsMatchRules(t *testing.T) {
	raw, err := os.ReadFile(repoRoot + "deploy/observability/runbook/operational-runbook.md")
	if err != nil {
		t.Fatalf("read runbook: %v", err)
	}
	content := string(raw)
	start := strings.Index(content, "## Alert Runbooks")
	end := len(content)
	if next := strings.Index(content[start+1:], "\n## "); next >= 0 {
		end = start + 1 + next
	}
	if start < 0 {
		t.Fatal("runbook 缺少 ## Alert Runbooks 节")
	}
	sections := make(map[string]bool)
	for _, m := range runbookAlertRe.FindAllStringSubmatch(content[start:end], -1) {
		sections[m[1]] = true
	}

	alertNames, _ := loadAlerts(t)
	for _, name := range alertNames {
		if !sections[name] {
			t.Errorf("rules.yaml 告警 %q 在 runbook 的 Alert Runbooks 节缺少 ### 处置段", name)
		}
	}
	for name := range sections {
		found := false
		for _, a := range alertNames {
			if a == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("runbook 有 %q 的处置段，但 rules.yaml 没有这条告警（过期段落）", name)
		}
	}
}
