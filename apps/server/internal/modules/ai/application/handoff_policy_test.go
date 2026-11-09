package application

import (
	"fmt"
	"testing"

	"servify/apps/server/internal/models"
)

// userTurns 构造 n 条用户消息（Sender=user），模拟真实会话历史。
func userTurns(n int) []models.Message {
	msgs := make([]models.Message, 0, n)
	for i := 0; i < n; i++ {
		msgs = append(msgs, models.Message{Sender: "user", Content: fmt.Sprintf("问题 %d", i)})
	}
	return msgs
}

func TestShouldTransferToHuman(t *testing.T) {
	if !ShouldTransferToHuman("请帮我转人工客服", nil) {
		t.Fatal("expected handoff for explicit handoff phrase")
	}
	if !ShouldTransferToHuman("我要投诉", nil) {
		t.Fatal("expected handoff for complaint keyword")
	}
	if !ShouldTransferToHuman("普通问题", userTurns(10)) {
		t.Fatal("expected handoff once user turns reach the threshold")
	}
	if ShouldTransferToHuman("普通问题", userTurns(9)) {
		t.Fatal("did not expect handoff below the user-turn threshold")
	}
	// assistant 回复不计入：历史 3 轮问答（6 条消息）不再触发兜底。
	mixed := append(userTurns(3), models.Message{Sender: "ai"}, models.Message{Sender: "ai"}, models.Message{Sender: "ai"})
	if ShouldTransferToHuman("普通问题", mixed) {
		t.Fatal("assistant messages must not count toward the history rule")
	}
	if ShouldTransferToHuman("普通问题", nil) {
		t.Fatal("did not expect handoff for normal query")
	}
}

// TestGoldenHandoffKeywords 固化转人工启发式的意图边界矩阵：显式中文意图
// 短语、英文全词边界（裸 agent/manual 不再是关键词）、投诉意图、长会话
// 兜底只数用户消息、大小写与空输入边界。
func TestGoldenHandoffKeywords(t *testing.T) {
	assistant := models.Message{Sender: "ai"}
	cases := []struct {
		name       string
		query      string
		userMsgs   int
		assistantN int
		want       bool
	}{
		{name: "转人工短语", query: "帮我转人工", want: true},
		{name: "找客服短语", query: "找客服问一下", want: true},
		{name: "英文 human 词", query: "talk to a human", want: true},
		{name: "英文 agent 短语", query: "connect me with an agent", want: true},
		{name: "大小写不敏感", query: "HUMAN please", want: true},
		{name: "投诉关键词", query: "我要投诉", want: true},
		{name: "英文 complaint", query: "this is a complaint", want: true},
		{name: "普通问题", query: "怎么申请退款", want: false},
		{name: "人工智能不误转", query: "人工智能是怎么实现的", want: false},
		{name: "user agent 不误转", query: "set the user agent header", want: false},
		{name: "裸 agent 不误转", query: "what can your agent do", want: false},
		{name: "user manual 不误转", query: "check the user manual", want: false},
		{name: "humanoid 词边界", query: "humanoid robots are cool", want: false},
		{name: "边界后继续搜", query: "inhuman or human", want: true},
		{name: "长会话兜底达阈值", query: "还有问题想问", userMsgs: 10, want: true},
		{name: "阈值边界差一轮", query: "还有问题想问", userMsgs: 9, want: false},
		{name: "assistant 不计入", query: "还有问题想问", userMsgs: 9, assistantN: 20, want: false},
		{name: "混合历史达阈值", query: "还有问题想问", userMsgs: 10, assistantN: 10, want: true},
		{name: "空输入", query: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			history := userTurns(tc.userMsgs)
			for i := 0; i < tc.assistantN; i++ {
				history = append(history, assistant)
			}
			if got := ShouldTransferToHuman(tc.query, history); got != tc.want {
				t.Fatalf("ShouldTransferToHuman(%q, %d user msgs) = %v, want %v", tc.query, tc.userMsgs, got, tc.want)
			}
		})
	}
}

// TestHandoffRulesConfigurable 规则面可配置：阈值收紧、关闭兜底、自定义
// 意图短语都按传入规则生效。
func TestHandoffRulesConfigurable(t *testing.T) {
	rules := DefaultHandoffRules()
	rules.HistoryUserTurns = 2
	if !ShouldTransferToHumanWithRules(rules, "普通问题", userTurns(2)) {
		t.Fatal("lowered threshold must transfer at 2 user turns")
	}
	rules.HistoryUserTurns = 0
	if ShouldTransferToHumanWithRules(rules, "普通问题", userTurns(2)) {
		t.Fatal("zero threshold must disable the history rule")
	}
	custom := HandoffRules{ZhHandoffPhrases: []string{"找我经理"}}
	if !ShouldTransferToHumanWithRules(custom, "请找我经理", nil) {
		t.Fatal("custom phrase must transfer")
	}
	if ShouldTransferToHumanWithRules(custom, "转人工", nil) {
		t.Fatal("custom rules must not inherit the default phrase list")
	}
	if ShouldTransferToHumanWithRules(HandoffRules{}, "转人工投诉 talk to a human", userTurns(99)) {
		t.Fatal("empty rules must disable every rule")
	}
}

// TestHandoffConfusionReport 标注样本集上的误报漏报报告：正样本（应转人工）
// 与负样本（不应转）过默认规则，输出混淆矩阵并断言零误报零漏报。
func TestHandoffConfusionReport(t *testing.T) {
	samples := []struct {
		query   string
		handoff bool
	}{
		{"转人工", true},
		{"我要投诉", true},
		{"帮我转接人工客服", true},
		{"我想举报这个功能", true},
		{"talk to a human", true},
		{"connect me with an agent", true},
		{"HUMAN please", true},
		{"this is a complaint", true},
		{"怎么申请退款", false},
		{"退款多久到账", false},
		{"人工智能是什么", false},
		{"你们的客服机器人多少钱", false},
		{"set the user agent header", false},
		{"check the user manual", false},
		{"what can your agent do", false},
		{"humanoid robots are cool", false},
		{"", false},
	}
	var tp, tn, fp, fn int
	for _, s := range samples {
		got := ShouldTransferToHuman(s.query, nil)
		switch {
		case s.handoff && got:
			tp++
		case !s.handoff && !got:
			tn++
		case !s.handoff && got:
			fp++
			t.Errorf("false positive: %q judged as handoff", s.query)
		case s.handoff && !got:
			fn++
			t.Errorf("false negative: %q judged as normal", s.query)
		}
	}
	t.Logf("handoff heuristic confusion: TP=%d TN=%d FP=%d FN=%d (samples=%d)", tp, tn, fp, fn, len(samples))
	if fp != 0 || fn != 0 {
		t.Fatalf("confusion matrix must be clean on the sample set: FP=%d FN=%d", fp, fn)
	}
}

func TestBuildSessionSummaryUnavailable(t *testing.T) {
	if got := BuildSessionSummaryUnavailable(); got != "无法生成会话摘要" {
		t.Fatalf("BuildSessionSummaryUnavailable() = %q", got)
	}
}
