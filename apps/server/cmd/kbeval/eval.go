// Command kbeval runs the customer-service knowledge evaluation set
// (dataset.json, sanitized synthetic content) against the local retrieval
// driver and prints recall / citation / refusal / latency baselines.
//
// The six covered question categories:
//
//	answer      a doc exists and must be recalled with the right citation
//	no_answer   no doc covers the topic; retrieval must stay empty so the
//	            no-evidence policy can refuse instead of hallucinating
//	stale       superseded and current docs coexist; both must surface and
//	            the current one must outrank the stale one
//	conflict    two live docs disagree; both must surface so operators see it
//	multilingual same-language zh/en queries plus one cross-lingual case
//	            that documents the lexical embedder boundary (informational)
//	privilege   another tenant's doc must never leak across the scope filter
//
// Usage:
//
//	go -C apps/server run ./cmd/kbeval [-dataset path] [-topk 5] [-json]
//
// The deterministic local embedder (CJK bigrams + latin tokens, FNV-1a) and
// the in-process sqlite driver make the run fully reproducible: identical
// inputs produce identical metrics. Latency numbers are an in-process
// baseline of the local driver path, not a production latency proxy.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"time"

	knowledgedomain "servify/apps/server/internal/modules/knowledge/domain"
	embedlocal "servify/apps/server/internal/platform/embedding/local"
	"servify/apps/server/internal/platform/knowledgeprovider"
	"servify/apps/server/internal/platform/knowledgeprovider/local"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

//go:embed dataset.json
var embeddedDataset embed.FS

// readEmbeddedDataset is a seam so the embedded-read failure branch stays
// testable (real builds always ship dataset.json in the embed).
var readEmbeddedDataset = func() ([]byte, error) {
	return embeddedDataset.ReadFile("dataset.json")
}

// EvalDoc is one sanitized document in the labeled set; Superseded marks the
// stale half of a versioned pair (current doc wins top-1).
type EvalDoc struct {
	ExternalID  string `json:"external_id"`
	TenantID    string `json:"tenant_id"`
	KnowledgeID string `json:"knowledge_id"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	Lang        string `json:"lang"`
	Superseded  bool   `json:"superseded,omitempty"`
}

// EvalCase is one labeled question with its expected retrieval outcome.
type EvalCase struct {
	ID            string   `json:"id"`
	Category      string   `json:"category"`
	Lang          string   `json:"lang"`
	Query         string   `json:"query"`
	TenantID      string   `json:"tenant_id"`
	KnowledgeID   string   `json:"knowledge_id"`
	ExpectDocIDs  []string `json:"expect_doc_ids"`
	ExpectTop1    string   `json:"expect_top1,omitempty"`
	ExpectZeroHit bool     `json:"expect_zero_hit"`
	// CrossLingual marks the documented embedder boundary: query language
	// differs from the target doc. Reported informationally, never gated.
	CrossLingual bool `json:"cross_lingual,omitempty"`
}

// EvalDataset is the labeled evaluation set.
type EvalDataset struct {
	Description string     `json:"description"`
	Driver      string     `json:"driver"`
	Embedder    string     `json:"embedder"`
	TopK        int        `json:"topk"`
	Threshold   float64    `json:"threshold"`
	Docs        []EvalDoc  `json:"docs"`
	Cases       []EvalCase `json:"cases"`
}

// loadDataset reads the evaluation set: a path wins, otherwise the embedded
// dataset ships as the published baseline.
func loadDataset(path string) (*EvalDataset, error) {
	var raw []byte
	if path == "" {
		b, err := readEmbeddedDataset()
		if err != nil {
			return nil, fmt.Errorf("read embedded dataset: %w", err)
		}
		raw = b
	} else {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read dataset %s: %w", path, err)
		}
		raw = b
	}
	var ds EvalDataset
	if err := json.Unmarshal(raw, &ds); err != nil {
		return nil, fmt.Errorf("parse dataset %s: %w", path, err)
	}
	if len(ds.Docs) == 0 || len(ds.Cases) == 0 {
		return nil, fmt.Errorf("dataset %s has no docs or no cases", path)
	}
	return &ds, nil
}

// CaseResult is the per-case retrieval outcome.
type CaseResult struct {
	ID             string   `json:"id"`
	Category       string   `json:"category"`
	CrossLingual   bool     `json:"cross_lingual,omitempty"`
	HitExternalIDs []string `json:"hit_external_ids"`
	Top1           string   `json:"top1"`
	RecallHits     int      `json:"recall_hits"`
	Top1Correct    bool     `json:"top1_correct"`
	ZeroHit        bool     `json:"zero_hit"`
	// Leaked: at least one hit fell outside the requesting case's tenant.
	Leaked        bool    `json:"leaked"`
	LatencyMicros float64 `json:"latency_micros"`
}

// CategoryMetric aggregates one category's outcomes.
type CategoryMetric struct {
	Cases          int     `json:"cases"`
	RecallHits     int     `json:"recall_hits"`
	RecallExpected int     `json:"recall_expected"`
	RecallRate     float64 `json:"recall_rate"`
	Top1Total      int     `json:"top1_total"`
	Top1Correct    int     `json:"top1_correct"`
	ZeroHitTotal   int     `json:"zero_hit_total"`
	ZeroHitOK      int     `json:"zero_hit_ok"`
	// FalseRefusals: an answerable case whose retrieval came back empty.
	FalseRefusals int `json:"false_refusals"`
	// WouldAnswer: a no-answer case whose retrieval leaked a hit — the caller
	// would answer instead of refusing.
	WouldAnswer int `json:"would_answer"`
	// Leaks: hits served outside the requesting case's tenant scope.
	Leaks int `json:"leaks"`
}

// Report is the full evaluation baseline.
type Report struct {
	Dataset             string                     `json:"dataset"`
	Driver              string                     `json:"driver"`
	Embedder            string                     `json:"embedder"`
	TopK                int                        `json:"topk"`
	Threshold           float64                    `json:"threshold"`
	DocCount            int                        `json:"doc_count"`
	ChunkCount          int64                      `json:"chunk_count"`
	IndexMillis         int64                      `json:"index_millis"`
	Categories          map[string]*CategoryMetric `json:"categories"`
	CrossLingualHitRate float64                    `json:"cross_lingual_hit_rate"`
	// RefusalMisjudgeRate: (false refusals + would-answer leaks) over all
	// judgeable non-cross-lingual cases.
	RefusalMisjudgeRate float64      `json:"refusal_misjudge_rate"`
	LatencyP50Micros    float64      `json:"latency_p50_micros"`
	LatencyP95Micros    float64      `json:"latency_p95_micros"`
	Cases               []CaseResult `json:"cases"`
}

// evalBackend is the retrieval backend under evaluation (provider + its
// scratch store for corpus accounting).
type evalBackend struct {
	prov knowledgeprovider.KnowledgeProvider
	db   *gorm.DB
}

// newEvalBackend assembles the local driver on the deterministic embedder;
// package seam so tests can inject failing backends for the error branches.
var newEvalBackend = func(ds *EvalDataset, topK int) (*evalBackend, error) {
	db, err := openEvalDB()
	if err != nil {
		return nil, err
	}
	emb := embedlocal.NewProvider(embedlocal.Config{})
	prov := local.NewProvider(db, emb, local.Config{
		Search: local.SearchConfig{TopK: topK, Threshold: ds.Threshold},
	})
	return &evalBackend{prov: prov, db: db}, nil
}

// evalCounters aggregates the cross-category misjudgment accounting.
type evalCounters struct {
	crossHits     int
	crossTotal    int
	falseRefusals int
	wouldAnswer   int
	judgeable     int
}

// evaluate seeds the local driver with the dataset docs and runs every case,
// producing the baseline report. Deterministic: the same dataset yields the
// same metrics on every run (latency aside).
func evaluate(ds *EvalDataset, datasetLabel string, topK int) (*Report, error) {
	if topK <= 0 {
		topK = ds.TopK
	}
	if topK <= 0 {
		topK = 5
	}

	backend, err := newEvalBackend(ds, topK)
	if err != nil {
		return nil, err
	}
	prov, db := backend.prov, backend.db

	indexStart := time.Now()
	for _, doc := range ds.Docs {
		if _, err := prov.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
			ID:          doc.ExternalID,
			TenantID:    doc.TenantID,
			KnowledgeID: doc.KnowledgeID,
			Title:       doc.Title,
			Content:     doc.Content,
		}); err != nil {
			return nil, fmt.Errorf("seed doc %s: %w", doc.ExternalID, err)
		}
	}
	indexMillis := time.Since(indexStart).Milliseconds()

	var chunkCount int64
	if err := db.Model(&knowledgedomain.KnowledgeDoc{}).Count(&chunkCount).Error; err != nil {
		return nil, fmt.Errorf("count chunks: %w", err)
	}

	report := &Report{
		Dataset:     datasetLabel,
		Driver:      ds.Driver,
		Embedder:    ds.Embedder,
		TopK:        topK,
		Threshold:   ds.Threshold,
		DocCount:    len(ds.Docs),
		ChunkCount:  chunkCount,
		IndexMillis: indexMillis,
		Categories:  map[string]*CategoryMetric{},
	}

	var latencies []float64
	counters := &evalCounters{}
	for _, tc := range ds.Cases {
		start := time.Now()
		hits, err := prov.Search(context.Background(), knowledgeprovider.SearchRequest{
			Query:       tc.Query,
			TenantID:    tc.TenantID,
			KnowledgeID: tc.KnowledgeID,
			TopK:        topK,
		})
		if err != nil {
			return nil, fmt.Errorf("search case %s: %w", tc.ID, err)
		}
		latency := float64(time.Since(start).Microseconds())
		latencies = append(latencies, latency)
		report.Cases = append(report.Cases, buildCaseResult(tc, hits, latency))
		recordCase(report, tc, &report.Cases[len(report.Cases)-1], counters)
	}

	for _, metric := range report.Categories {
		if metric.RecallExpected > 0 {
			metric.RecallRate = float64(metric.RecallHits) / float64(metric.RecallExpected)
		}
	}
	if counters.crossTotal > 0 {
		report.CrossLingualHitRate = float64(counters.crossHits) / float64(counters.crossTotal)
	}
	if counters.judgeable > 0 {
		report.RefusalMisjudgeRate = float64(counters.falseRefusals+counters.wouldAnswer) / float64(counters.judgeable)
	}
	report.LatencyP50Micros = percentile(latencies, 50)
	report.LatencyP95Micros = percentile(latencies, 95)
	sort.Slice(report.Cases, func(i, j int) bool { return report.Cases[i].ID < report.Cases[j].ID })
	return report, nil
}

// buildCaseResult maps one search outcome onto the labeled expectation.
func buildCaseResult(tc EvalCase, hits []knowledgeprovider.KnowledgeHit, latency float64) CaseResult {
	hitIDs := make([]string, 0, len(hits))
	leaked := false
	for _, hit := range hits {
		external, _ := hit.Metadata["external_id"].(string)
		hitIDs = append(hitIDs, external)
		if hit.Metadata["tenant_id"] != tc.TenantID {
			leaked = true
		}
	}
	found := 0
	for _, want := range tc.ExpectDocIDs {
		for _, got := range hitIDs {
			if got == want {
				found++
				break
			}
		}
	}
	top1 := ""
	if len(hitIDs) > 0 {
		top1 = hitIDs[0]
	}
	res := CaseResult{
		ID:             tc.ID,
		Category:       tc.Category,
		CrossLingual:   tc.CrossLingual,
		HitExternalIDs: hitIDs,
		Top1:           top1,
		RecallHits:     found,
		ZeroHit:        len(hitIDs) == 0,
		Leaked:         leaked,
		LatencyMicros:  latency,
	}
	if tc.ExpectTop1 != "" {
		res.Top1Correct = top1 == tc.ExpectTop1
	} else {
		res.Top1Correct = true // 无引用预期：不评引用正确率
	}
	return res
}

// recordCase folds one case outcome into its category metric and the global
// misjudgment counters. Cross-lingual cases are informational only: the
// lexical embedder boundary is documented, never gated.
func recordCase(report *Report, tc EvalCase, res *CaseResult, counters *evalCounters) {
	metric := report.Categories[tc.Category]
	if metric == nil {
		metric = &CategoryMetric{}
		report.Categories[tc.Category] = metric
	}
	metric.Cases++
	metric.RecallHits += res.RecallHits
	metric.RecallExpected += len(tc.ExpectDocIDs)
	if tc.ExpectTop1 != "" && !tc.CrossLingual {
		metric.Top1Total++
		if res.Top1Correct {
			metric.Top1Correct++
		}
	}
	if tc.ExpectZeroHit {
		metric.ZeroHitTotal++
		if res.ZeroHit {
			metric.ZeroHitOK++
		} else {
			metric.WouldAnswer++
		}
	}
	if res.Leaked {
		metric.Leaks++
	}
	if tc.CrossLingual {
		counters.crossTotal++
		if !res.ZeroHit {
			counters.crossHits++
		}
		return
	}
	// 拒答误判面：可答空回 = 误拒；无答案却命中 = 会乱答（误答）。
	counters.judgeable++
	if tc.ExpectZeroHit {
		if !res.ZeroHit {
			counters.wouldAnswer++
		}
	} else if res.ZeroHit {
		counters.falseRefusals++
		metric.FalseRefusals++
	}
}

// percentile returns the p-th percentile over sorted-agnostic samples
// (nearest-rank on a sorted copy); empty input yields 0.
func percentile(samples []float64, p float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	sorted := make([]float64, len(samples))
	copy(sorted, samples)
	sort.Float64s(sorted)
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

// renderText prints the human-readable baseline summary.
func renderText(w io.Writer, r *Report) {
	fmt.Fprintf(w, "knowledge evaluation baseline\n")
	fmt.Fprintf(w, "  dataset:   %s\n", r.Dataset)
	fmt.Fprintf(w, "  driver:    %s (embedder: %s)\n", r.Driver, r.Embedder)
	fmt.Fprintf(w, "  corpus:    %d docs, %d chunks, indexed in %dms\n", r.DocCount, r.ChunkCount, r.IndexMillis)
	fmt.Fprintf(w, "  search:    topk=%d threshold=%.2f\n", r.TopK, r.Threshold)
	categories := make([]string, 0, len(r.Categories))
	for name := range r.Categories {
		categories = append(categories, name)
	}
	sort.Strings(categories)
	for _, name := range categories {
		m := r.Categories[name]
		fmt.Fprintf(w, "  %-12s recall %d/%d (%.0f%%)", name, m.RecallHits, m.RecallExpected, m.RecallRate*100)
		if m.Top1Total > 0 {
			fmt.Fprintf(w, ", top1 %d/%d", m.Top1Correct, m.Top1Total)
		}
		if m.ZeroHitTotal > 0 {
			fmt.Fprintf(w, ", zero-hit %d/%d", m.ZeroHitOK, m.ZeroHitTotal)
		}
		if m.FalseRefusals > 0 || m.WouldAnswer > 0 || m.Leaks > 0 {
			fmt.Fprintf(w, ", false-refusal %d would-answer %d leaks %d", m.FalseRefusals, m.WouldAnswer, m.Leaks)
		}
		fmt.Fprint(w, "\n")
	}
	fmt.Fprintf(w, "  refusal misjudge rate: %.1f%%\n", r.RefusalMisjudgeRate*100)
	fmt.Fprintf(w, "  cross-lingual hit rate: %.0f%% (lexical embedder boundary, informational)\n", r.CrossLingualHitRate*100)
	fmt.Fprintf(w, "  latency p50 %.0fµs p95 %.0fµs (in-process local driver baseline)\n", r.LatencyP50Micros, r.LatencyP95Micros)
}

// renderJSON prints the machine-readable report.
func renderJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// evalDBDSN is the scratch corpus DSN; package seam for open-failure tests.
var evalDBDSN = ":memory:"

// openEvalDB opens the scratch in-memory store for the evaluation corpus.
func openEvalDB() (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(evalDBDSN), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open scratch sqlite: %w", err)
	}
	if err := migrateEvalDB(db); err != nil {
		return nil, err
	}
	return db, nil
}

// migrateEvalDB prepares the corpus table; a seam var so openEvalDB's
// propagate branch stays testable without a broken filesystem.
var migrateEvalDB = func(db *gorm.DB) error {
	if err := db.AutoMigrate(&knowledgedomain.KnowledgeDoc{}); err != nil {
		return fmt.Errorf("migrate scratch corpus: %w", err)
	}
	return nil
}
