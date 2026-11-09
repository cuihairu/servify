package main

// 评测基线回归门：脱敏标注集六类问题的召回/引用/拒答/越权底线 + 可复现性
//（确定性嵌入器与进程内驱动下两次运行指标一致，延迟除外）。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	knowledgedomain "servify/apps/server/internal/modules/knowledge/domain"
	"servify/apps/server/internal/platform/knowledgeprovider"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// runBaseline loads the embedded labeled set and evaluates it.
func runBaseline(t *testing.T) *Report {
	t.Helper()
	ds, err := loadDataset("")
	if err != nil {
		t.Fatalf("load embedded dataset: %v", err)
	}
	report, err := evaluate(ds, "test", 0)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	return report
}

func TestEvalBaselineFloors(t *testing.T) {
	report := runBaseline(t)

	assertRate := func(category string, got float64, want float64, label string) {
		t.Helper()
		if got != want {
			t.Fatalf("%s %s = %v, want %v", category, label, got, want)
		}
	}
	answer := report.Categories["answer"]
	if answer == nil {
		t.Fatal("answer category missing")
	}
	assertRate("answer", answer.RecallRate, 1, "recall")
	if answer.Top1Correct != answer.Top1Total || answer.Top1Total == 0 {
		t.Fatalf("answer citation accuracy %d/%d, want all", answer.Top1Correct, answer.Top1Total)
	}
	if answer.FalseRefusals != 0 {
		t.Fatalf("answer false refusals = %d, want 0", answer.FalseRefusals)
	}

	for _, category := range []string{"no_answer", "privilege"} {
		m := report.Categories[category]
		if m == nil {
			t.Fatalf("%s category missing", category)
		}
		if m.ZeroHitTotal == 0 || m.ZeroHitOK != m.ZeroHitTotal {
			t.Fatalf("%s zero-hit %d/%d, want all (refusals must stay silent)", category, m.ZeroHitOK, m.ZeroHitTotal)
		}
		if m.Leaks != 0 {
			t.Fatalf("%s scope leaks = %d, want 0", category, m.Leaks)
		}
	}

	stale := report.Categories["stale"]
	if stale == nil || stale.RecallRate != 1 {
		t.Fatalf("stale recall = %v, want 1 (both versions must surface)", stale)
	}
	if stale.Top1Correct != stale.Top1Total || stale.Top1Total == 0 {
		t.Fatalf("stale top1 %d/%d, want current doc ranked first", stale.Top1Correct, stale.Top1Total)
	}

	conflict := report.Categories["conflict"]
	if conflict == nil || conflict.RecallRate != 1 {
		t.Fatalf("conflict recall = %v, want 1 (both contradictory docs must surface)", conflict)
	}

	if report.RefusalMisjudgeRate != 0 {
		t.Fatalf("refusal misjudge rate = %v, want 0", report.RefusalMisjudgeRate)
	}
	// 词法嵌入器不做跨语言检索：0% 是已声明边界（dataset.json cross_lingual 注记），
	// 换真实向量服务后该基线需重估。
	if report.CrossLingualHitRate != 0 {
		t.Fatalf("cross-lingual hit rate = %v, want 0 (documented lexical embedder boundary)", report.CrossLingualHitRate)
	}
	if report.ChunkCount == 0 || report.DocCount == 0 {
		t.Fatalf("corpus counts must be non-zero, docs=%d chunks=%d", report.DocCount, report.ChunkCount)
	}
	if report.LatencyP50Micros <= 0 || report.LatencyP95Micros < report.LatencyP50Micros {
		t.Fatalf("latency baseline p50=%.0f p95=%.0f", report.LatencyP50Micros, report.LatencyP95Micros)
	}
}

func TestEvalDeterministic(t *testing.T) {
	first := runBaseline(t)
	second := runBaseline(t)

	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first: %v", err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second: %v", err)
	}

	strip := func(in []byte) map[string]interface{} {
		var m map[string]interface{}
		if err := json.Unmarshal(in, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		// 延迟与时序基线不作确定性断言。
		for _, key := range []string{"latency_p50_micros", "latency_p95_micros", "index_millis"} {
			delete(m, key)
		}
		for _, c := range m["cases"].([]interface{}) {
			delete(c.(map[string]interface{}), "latency_micros")
		}
		return m
	}
	a, _ := json.Marshal(strip(firstJSON))
	b, _ := json.Marshal(strip(secondJSON))
	if !bytes.Equal(a, b) {
		t.Fatalf("evaluation must be deterministic:\nfirst:  %s\nsecond: %s", a, b)
	}
}

func TestRenderTextAndJSON(t *testing.T) {
	report := runBaseline(t)
	var buf bytes.Buffer
	renderText(&buf, report)
	out := buf.String()
	for _, want := range []string{"knowledge evaluation baseline", "recall", "refusal misjudge rate", "latency p50"} {
		if !strings.Contains(out, want) {
			t.Fatalf("text report missing %q:\n%s", want, out)
		}
	}

	var jsonBuf bytes.Buffer
	if err := renderJSON(&jsonBuf, report); err != nil {
		t.Fatalf("renderJSON: %v", err)
	}
	var parsed Report
	if err := json.Unmarshal(jsonBuf.Bytes(), &parsed); err != nil {
		t.Fatalf("json report must parse: %v", err)
	}
	if parsed.Dataset != report.Dataset || len(parsed.Cases) != len(report.Cases) {
		t.Fatal("json report must round-trip the same report")
	}
}

func TestPercentile(t *testing.T) {
	if got := percentile(nil, 50); got != 0 {
		t.Fatalf("percentile(nil) = %v, want 0", got)
	}
	got := percentile([]float64{10, 40, 20, 30}, 50)
	if got != 20 { // nearest-rank: ceil(0.5*4)=2 → sorted[1]=20
		t.Fatalf("percentile = %v, want 20", got)
	}
	if got := percentile([]float64{5}, 95); got != 5 {
		t.Fatalf("percentile single = %v, want 5", got)
	}
	if got := percentile([]float64{1, 2}, 200); got != 2 {
		t.Fatalf("percentile clamp = %v, want 2", got)
	}
	if got := percentile([]float64{1, 2}, 1); got != 1 {
		t.Fatalf("percentile floor = %v, want 1", got)
	}
	if got := percentile([]float64{1, 2}, 0); got != 1 {
		t.Fatalf("percentile p=0 rank clamp = %v, want 1", got)
	}
}

func TestLoadDatasetFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ds.json")
	ds, err := loadDataset("")
	if err != nil {
		t.Fatalf("load embedded: %v", err)
	}
	raw, err := json.Marshal(ds)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := loadDataset(path)
	if err != nil {
		t.Fatalf("load from file: %v", err)
	}
	if len(loaded.Cases) != len(ds.Cases) || len(loaded.Docs) != len(ds.Docs) {
		t.Fatal("file dataset must match embedded set")
	}

	if _, err := loadDataset(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("missing dataset file must fail")
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write bad: %v", err)
	}
	if _, err := loadDataset(bad); err == nil {
		t.Fatal("invalid dataset json must fail")
	}
	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, []byte(`{"docs":[],"cases":[]}`), 0o644); err != nil {
		t.Fatalf("write empty: %v", err)
	}
	if _, err := loadDataset(empty); err == nil {
		t.Fatal("dataset without docs or cases must fail")
	}
}

// stubProvider injects failures at the provider seam for error-branch tests.
type stubProvider struct {
	upsertErr error
	searchErr error
}

var _ knowledgeprovider.KnowledgeProvider = (*stubProvider)(nil)

func (s *stubProvider) Search(ctx context.Context, req knowledgeprovider.SearchRequest) ([]knowledgeprovider.KnowledgeHit, error) {
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	return nil, nil
}

func (s *stubProvider) UpsertDocument(ctx context.Context, doc knowledgeprovider.KnowledgeDocument) (string, error) {
	if s.upsertErr != nil {
		return "", s.upsertErr
	}
	return doc.ID, nil
}

func (s *stubProvider) DeleteDocument(ctx context.Context, doc knowledgeprovider.KnowledgeDocument) error {
	return nil
}

func (s *stubProvider) HealthCheck(ctx context.Context) error { return nil }

// seamDataset is the minimal dataset driving the seam-override tests.
func seamDataset() *EvalDataset {
	return &EvalDataset{
		Driver: "local", Embedder: "local", Threshold: 0.7,
		Docs:  []EvalDoc{{ExternalID: "d1", TenantID: "t", KnowledgeID: "kb", Title: "t", Content: "c"}},
		Cases: []EvalCase{{ID: "c1", Category: "answer", Query: "q", TenantID: "t", KnowledgeID: "kb", ExpectDocIDs: []string{"d1"}}},
	}
}

// stubBackend swaps the backend seam for a failing/silent provider; returns
// the restore func.
func stubBackend(t *testing.T, prov knowledgeprovider.KnowledgeProvider, db *gorm.DB) func() {
	t.Helper()
	if db == nil {
		var err error
		db, err = openEvalDB()
		if err != nil {
			t.Fatalf("open scratch db: %v", err)
		}
	}
	orig := newEvalBackend
	newEvalBackend = func(ds *EvalDataset, topK int) (*evalBackend, error) {
		return &evalBackend{prov: prov, db: db}, nil
	}
	return func() { newEvalBackend = orig }
}

func TestLoadDatasetEmbeddedReadFailure(t *testing.T) {
	orig := readEmbeddedDataset
	readEmbeddedDataset = func() ([]byte, error) { return nil, fmt.Errorf("embed boom") }
	defer func() { readEmbeddedDataset = orig }()
	_, err := loadDataset("")
	if err == nil || !strings.Contains(err.Error(), "read embedded dataset") {
		t.Fatalf("embedded read failure must surface: %v", err)
	}
}

func TestEvalBackendOpenFailure(t *testing.T) {
	origDSN := evalDBDSN
	evalDBDSN = "file:%bad%.db?mode=rw"
	defer func() { evalDBDSN = origDSN }()
	if _, err := newEvalBackend(seamDataset(), 5); err == nil || !strings.Contains(err.Error(), "open scratch sqlite") {
		t.Fatalf("bad DSN must fail at open: %v", err)
	}
	if _, err := evaluate(seamDataset(), "test", 0); err == nil {
		t.Fatal("evaluate must propagate backend open failure")
	}
}

func TestEvalMigrateFailure(t *testing.T) {
	orig := migrateEvalDB
	migrateEvalDB = func(db *gorm.DB) error { return fmt.Errorf("migrate boom") }
	defer func() { migrateEvalDB = orig }()
	_, err := openEvalDB()
	if err == nil || !strings.Contains(err.Error(), "migrate boom") {
		t.Fatalf("migrate failure must propagate: %v", err)
	}
	migrateEvalDB = orig

	// 真实实现打关闭池：AutoMigrate 必报错。
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := migrateEvalDB(db); err == nil {
		t.Fatal("migrate on closed pool must fail")
	}
}

func TestEvalSeedFailure(t *testing.T) {
	restore := stubBackend(t, &stubProvider{upsertErr: fmt.Errorf("seed boom")}, nil)
	defer restore()
	_, err := evaluate(seamDataset(), "test", 0)
	if err == nil || !strings.Contains(err.Error(), "seed doc d1") {
		t.Fatalf("seed failure must propagate: %v", err)
	}
}

func TestEvalSearchFailure(t *testing.T) {
	restore := stubBackend(t, &stubProvider{searchErr: fmt.Errorf("search boom")}, nil)
	defer restore()
	_, err := evaluate(seamDataset(), "test", 0)
	if err == nil || !strings.Contains(err.Error(), "search case c1") {
		t.Fatalf("search failure must propagate: %v", err)
	}
}

func TestEvalCountChunksFailure(t *testing.T) {
	db, err := openEvalDB()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrator().DropTable(&knowledgedomain.KnowledgeDoc{}); err != nil {
		t.Fatalf("drop: %v", err)
	}
	restore := stubBackend(t, &stubProvider{}, db)
	defer restore()
	_, err = evaluate(seamDataset(), "test", 0)
	if err == nil || !strings.Contains(err.Error(), "count chunks") {
		t.Fatalf("count failure must propagate: %v", err)
	}
}

func TestEvaluateFallbackTopK(t *testing.T) {
	ds := seamDataset()
	ds.TopK = 0
	report, err := evaluate(ds, "test", 0)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if report.TopK != 5 {
		t.Fatalf("topk fallback = %d, want 5", report.TopK)
	}
}

func TestBuildCaseResultLeak(t *testing.T) {
	tc := EvalCase{ID: "c", Category: "privilege", TenantID: "tenant-a"}
	hits := []knowledgeprovider.KnowledgeHit{{
		Metadata: map[string]interface{}{"external_id": "tenant-b-pricing", "tenant_id": "tenant-b"},
	}}
	res := buildCaseResult(tc, hits, 5)
	if !res.Leaked {
		t.Fatal("cross-tenant hit must flag leak")
	}
	if res.Top1 != "tenant-b-pricing" || res.ZeroHit {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestRecordCaseMisjudgments(t *testing.T) {
	report := &Report{Categories: map[string]*CategoryMetric{}}
	counters := &evalCounters{}

	// 无答案却命中：会误答（would answer）。
	noAnswer := EvalCase{ID: "na", Category: "no_answer", ExpectZeroHit: true}
	recordCase(report, noAnswer, &CaseResult{ZeroHit: false}, counters)
	got := report.Categories["no_answer"]
	if got.WouldAnswer != 1 || got.ZeroHitOK != 0 || got.ZeroHitTotal != 1 {
		t.Fatalf("would-answer not counted: %+v", got)
	}
	if counters.wouldAnswer != 1 || counters.judgeable != 1 {
		t.Fatalf("counters wouldAnswer=%d judgeable=%d", counters.wouldAnswer, counters.judgeable)
	}

	// 越权命中计 leaks。
	recordCase(report, EvalCase{ID: "lk", Category: "privilege"}, &CaseResult{Leaked: true}, counters)
	if report.Categories["privilege"].Leaks != 1 {
		t.Fatal("leak not counted")
	}

	// 跨语言只计信息面：不入误判分母，top1 不计引用正确率。
	before := counters.judgeable
	cross := EvalCase{ID: "cx", Category: "multilingual", CrossLingual: true, ExpectTop1: "d1"}
	recordCase(report, cross, &CaseResult{Top1Correct: true}, counters)
	if counters.crossTotal != 1 || counters.crossHits != 1 {
		t.Fatalf("cross counters total=%d hits=%d", counters.crossTotal, counters.crossHits)
	}
	if counters.judgeable != before {
		t.Fatal("cross-lingual case must not enter misjudge denominator")
	}
	if got := report.Categories["multilingual"]; got.Top1Total != 0 {
		t.Fatalf("cross-lingual top1 must stay informational, got %d", got.Top1Total)
	}

	// 可答空回：误拒。
	answer := EvalCase{ID: "an", Category: "answer", ExpectDocIDs: []string{"d1"}}
	recordCase(report, answer, &CaseResult{ZeroHit: true}, counters)
	got = report.Categories["answer"]
	if got.FalseRefusals != 1 || counters.falseRefusals != 1 || counters.judgeable != 3 {
		t.Fatalf("false refusal not counted: %+v counters=%+v", got, counters)
	}
}

func TestRenderTextMisjudgeLine(t *testing.T) {
	report := &Report{
		Dataset: "test", Driver: "local", Embedder: "local", TopK: 5, Threshold: 0.7,
		Categories: map[string]*CategoryMetric{
			"no_answer": {Cases: 1, WouldAnswer: 1, Leaks: 1},
		},
	}
	var buf bytes.Buffer
	renderText(&buf, report)
	if !strings.Contains(buf.String(), "would-answer") {
		t.Fatalf("misjudge line missing: %s", buf.String())
	}
}

func TestRunEvaluateError(t *testing.T) {
	orig := newEvalBackend
	newEvalBackend = func(ds *EvalDataset, topK int) (*evalBackend, error) {
		return nil, fmt.Errorf("backend boom")
	}
	defer func() { newEvalBackend = orig }()
	if err := run("", 0, false, io.Discard); err == nil || !strings.Contains(err.Error(), "backend boom") {
		t.Fatalf("run must surface evaluate failure: %v", err)
	}
}
