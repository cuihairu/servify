package delivery

import (
	"testing"

	"servify/apps/server/internal/models"
	aimodule "servify/apps/server/internal/modules/ai/application"
)

func TestKnowledgeBase_Search_Basic(t *testing.T) {
	kb := &KnowledgeBase{}
	kb.AddDocument(models.KnowledgeDoc{Title: "A", Content: "hello world"})
	kb.AddDocument(models.KnowledgeDoc{Title: "B", Content: "foo bar"})
	got := kb.Search("hello", 5)
	if len(got) == 0 {
		t.Fatalf("expected >=1 result")
	}
}

func TestAIService_ShouldTransfer_Complaint(t *testing.T) {
	if !aimodule.ShouldTransferToHuman("我要投诉你们的服务", nil) {
		t.Fatalf("expected complaint to transfer")
	}
}

func TestAIService_ShouldTransferToHuman_Delegates(t *testing.T) {
	// 刀 16 起启发式实现只在 application 包；AIService 方法仅委托转发。
	svc := NewAIService("", "")
	if !svc.ShouldTransferToHuman("帮我转人工", nil) {
		t.Fatal("expected delegation to return true for human keyword")
	}
	if svc.ShouldTransferToHuman("普通问题", nil) {
		t.Fatal("expected delegation to return false for normal query")
	}
}
