package application

import (
	"context"
	"errors"
	"time"
)

// ErrInvalidScoreOutput 表示 LLM 返回内容无法解析为打分 JSON（计入 attempt 重试）。
var ErrInvalidScoreOutput = errors.New("quality: invalid llm score output")

// TranscriptTurn 打分输入的会话轮次。
type TranscriptTurn struct {
	Role    string // user|agent|ai
	At      time.Time
	Content string
}

// ScoringDimension 打分维度（key 固定、权重参与总分加权）。
type ScoringDimension struct {
	Key    string
	Prompt string
	Weight float64
}

// ScoreRequest 单个会话的打分请求。
type ScoreRequest struct {
	SessionID  string
	Turns      []TranscriptTurn
	Dimensions []ScoringDimension
}

// DimensionScore 单维度得分（0-10）与理由。
type DimensionScore struct {
	Score  float64 `json:"score"`
	Weight float64 `json:"weight"`
	Reason string  `json:"reason"`
}

// ScoreResult 打分结果（总分 = 维度加权平均）。
type ScoreResult struct {
	TotalScore float64
	Dimensions map[string]DimensionScore
	Summary    string
	Provider   string
	Model      string
}

// ScoreProvider LLM 打分端口；实现放 infra（模块不 import 具体服务防环）。
// nil 或未启用时 RunScan 降级为 rules-only。
type ScoreProvider interface {
	ScoreSession(ctx context.Context, req ScoreRequest) (ScoreResult, error)
}
