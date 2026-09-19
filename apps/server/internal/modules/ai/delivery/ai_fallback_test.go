package delivery

import "testing"

func TestAIService_FallbackResponses(t *testing.T) {
	s := NewAIService("", "")
	if resp := s.getFallbackResponse("你好"); !contains(resp, "您好") {
		t.Fatalf("unexpected greeting: %s", resp)
	}
	if resp := s.getFallbackResponse("帮助"); !contains(resp, "帮助您") {
		t.Fatalf("unexpected help: %s", resp)
	}
	if resp := s.getFallbackResponse("谢谢"); !contains(resp, "不客气") {
		t.Fatalf("unexpected thanks: %s", resp)
	}
	if resp := s.getFallbackResponse("其他问题"); resp == "" {
		t.Fatalf("default fallback should not be empty")
	}
}
