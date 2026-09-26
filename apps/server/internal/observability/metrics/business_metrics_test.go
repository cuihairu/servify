package metrics

import (
	"testing"
)

func TestBusinessMetrics_RecordConversationCreated(t *testing.T) {
	reg := NewRegistry()
	bm := NewBusinessMetrics(reg)

	bm.RecordConversationCreated("tenant-1", "web")

	mfs, _ := reg.Gatherer().Gather()
	found := false
	for _, mf := range mfs {
		if mf.GetName() == "conversations_created_total" {
			found = true
			if len(mf.GetMetric()) == 0 {
				t.Fatal("expected at least one metric sample")
			}
			if mf.GetMetric()[0].GetCounter().GetValue() != 1 {
				t.Fatalf("expected counter value 1")
			}
		}
	}
	if !found {
		t.Fatal("expected conversations_created_total metric")
	}
}

func TestBusinessMetrics_RecordAIRequest(t *testing.T) {
	reg := NewRegistry()
	bm := NewBusinessMetrics(reg)

	bm.RecordAIRequest("openai", "gpt-4", "success", "primary", 0.5)

	mfs, _ := reg.Gatherer().Gather()
	foundCount := false
	foundDuration := false
	for _, mf := range mfs {
		if mf.GetName() == "ai_requests_total" {
			foundCount = true
			if mf.GetMetric()[0].GetCounter().GetValue() != 1 {
				t.Fatalf("expected counter value 1")
			}
		}
		if mf.GetName() == "ai_request_duration_seconds" {
			foundDuration = true
			if len(mf.GetMetric()[0].GetHistogram().GetBucket()) == 0 {
				t.Fatal("expected histogram buckets")
			}
		}
	}
	if !foundCount {
		t.Fatal("expected ai_requests_total metric")
	}
	if !foundDuration {
		t.Fatal("expected ai_request_duration_seconds metric")
	}
}

func TestBusinessMetrics_RecordAILLMTokens(t *testing.T) {
	reg := NewRegistry()
	bm := NewBusinessMetrics(reg)

	bm.RecordAILLMTokens("openai", "input", 150)
	bm.RecordAILLMTokens("openai", "output", 50)

	mfs, _ := reg.Gatherer().Gather()
	for _, mf := range mfs {
		if mf.GetName() == "ai_llm_tokens_total" {
			total := 0.0
			for _, m := range mf.GetMetric() {
				total += m.GetCounter().GetValue()
			}
			if total != 200 {
				t.Fatalf("expected total 200 tokens, got %v", total)
			}
			return
		}
	}
	t.Fatal("expected ai_llm_tokens_total metric")
}

func TestBusinessMetrics_RecordVoiceTranslationSentence(t *testing.T) {
	reg := NewRegistry()
	bm := NewBusinessMetrics(reg)

	bm.RecordVoiceTranslationSentence("translated")
	bm.RecordVoiceTranslationSentence("caption_only")
	bm.RecordVoiceTranslationSentence("degraded")
	bm.RecordVoiceTranslationSentence("degraded")

	mfs, _ := reg.Gatherer().Gather()
	found := false
	for _, mf := range mfs {
		if mf.GetName() != "voice_translation_sentences_total" {
			continue
		}
		found = true
		byOutcome := map[string]float64{}
		for _, m := range mf.GetMetric() {
			byOutcome[m.GetLabel()[0].GetValue()] = m.GetCounter().GetValue()
		}
		if byOutcome["translated"] != 1 || byOutcome["caption_only"] != 1 || byOutcome["degraded"] != 2 {
			t.Fatalf("unexpected outcome counts: %v", byOutcome)
		}
	}
	if !found {
		t.Fatal("expected voice_translation_sentences_total metric")
	}
}

func TestBusinessMetrics_RecordTicketCreated(t *testing.T) {
	reg := NewRegistry()
	bm := NewBusinessMetrics(reg)

	bm.RecordTicketCreated("tenant-1", "high")

	mfs, _ := reg.Gatherer().Gather()
	for _, mf := range mfs {
		if mf.GetName() == "tickets_created_total" {
			if mf.GetMetric()[0].GetCounter().GetValue() != 1 {
				t.Fatalf("expected counter value 1")
			}
			return
		}
	}
	t.Fatal("expected tickets_created_total metric")
}

func TestBusinessMetrics_RecordRoutingDecision(t *testing.T) {
	reg := NewRegistry()
	bm := NewBusinessMetrics(reg)

	bm.RecordRoutingDecision("tenant-1", "round-robin", "success")

	mfs, _ := reg.Gatherer().Gather()
	for _, mf := range mfs {
		if mf.GetName() == "routing_decisions_total" {
			if mf.GetMetric()[0].GetCounter().GetValue() != 1 {
				t.Fatalf("expected counter value 1")
			}
			return
		}
	}
	t.Fatal("expected routing_decisions_total metric")
}
