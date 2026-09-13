package services

import (
	"testing"

	"servify/apps/server/internal/models"
)

// TestGoldenHandoffKeywords 固化 services 层转人工启发式的关键词矩阵，
// 与 ai/application 的 golden set 同步防劣化：关键词/投诉短路、长会话兜底。
func TestGoldenHandoffKeywords(t *testing.T) {
	svc := &AIService{}
	cases := []struct {
		name    string
		query   string
		history int
		want    bool
	}{
		{name: "人工关键词", query: "帮我转人工", history: 0, want: true},
		{name: "客服关键词", query: "找客服问一下", history: 0, want: true},
		{name: "英文 human", query: "talk to a human", history: 0, want: true},
		{name: "英文 agent", query: "connect me with an agent", history: 0, want: true},
		{name: "英文 manual", query: "manual handling please", history: 0, want: true},
		{name: "投诉关键词", query: "我要投诉", history: 0, want: true},
		{name: "英文 complaint", query: "this is a complaint", history: 0, want: true},
		{name: "普通问题", query: "怎么申请退款", history: 0, want: false},
		{name: "大小写不敏感", query: "HUMAN please", history: 0, want: true},
		{name: "长会话兜底", query: "还有问题想问", history: 6, want: true},
		{name: "短会话普通", query: "还有问题想问", history: 2, want: false},
		{name: "空输入", query: "", history: 0, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			history := make([]models.Message, tc.history)
			if got := svc.ShouldTransferToHuman(tc.query, history); got != tc.want {
				t.Fatalf("ShouldTransferToHuman(%q, %d msgs) = %v, want %v", tc.query, tc.history, got, tc.want)
			}
		})
	}
}
