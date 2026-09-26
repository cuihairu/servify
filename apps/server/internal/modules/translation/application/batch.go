// batch.go 历史消息批量子段翻译（Phase 1 收尾，docs/realtime-translation-design.md
// §1.5）：多条待译文本合并为单次 LLM 调用（编号分段协议），解析不符时自动
// 退回逐条 Translate——批量只优化成本/延迟，不改变 Phase 0 的单条语义与失败面。
package application

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"servify/apps/server/internal/platform/llm"
)

// MaxBatchTexts 单次批量翻译的段数上限（工作台历史分页 ≤ 200，但一次装配
// 20 段已覆盖常见屏容量；更多文本由调用方分页装配）。
const MaxBatchTexts = 20

// 段标记协议：编号行独立成行，正则容错（序号前导零、行尾空白）。
var batchSegmentPattern = regexp.MustCompile(`(?m)^<<<SEG\s*0*(\d+)\s*>>>[ \t]*$`)

var ErrTranslationBatchTooLarge = errors.New("batch translation exceeds segment limit")

// BatchTranslateCommand 批量翻译指令：Texts 按调用方顺序排列的待译段。
type BatchTranslateCommand struct {
	Texts      []string
	TargetLang string
}

// BatchTranslateResult 逐段译文（与 Texts 同序同长）；某段译文为空串表示
// 该段翻译失败被跳过（批量是增强能力，单段失败不否决整批）。
type BatchTranslateResult struct {
	Texts      []string
	TargetLang string
}

// BatchTranslate 批量翻译：单段走 Translate 原路径；多段合并单次 LLM 调用
// （编号分段协议），响应段数/顺序不符或出现空段时退回逐条翻译。provider
// 错误原样上抛。
func (s *Service) BatchTranslate(ctx context.Context, cmd BatchTranslateCommand) (BatchTranslateResult, error) {
	if s == nil || s.llm == nil {
		return BatchTranslateResult{}, ErrTranslationUnavailable
	}
	target := normalizeLang(cmd.TargetLang)
	if target == "" {
		return BatchTranslateResult{}, ErrTranslationTargetRequired
	}
	if !langTagPattern.MatchString(target) {
		return BatchTranslateResult{}, fmt.Errorf("%w: %q", ErrTranslationLangInvalid, cmd.TargetLang)
	}
	if len(cmd.Texts) == 0 {
		return BatchTranslateResult{Texts: []string{}, TargetLang: target}, nil
	}
	if len(cmd.Texts) > MaxBatchTexts {
		return BatchTranslateResult{}, fmt.Errorf("%w: %d > %d", ErrTranslationBatchTooLarge, len(cmd.Texts), MaxBatchTexts)
	}
	texts := make([]string, len(cmd.Texts))
	for i, text := range cmd.Texts {
		text = strings.TrimSpace(text)
		if text == "" {
			return BatchTranslateResult{}, ErrTranslationTextRequired
		}
		if utf8.RuneCountInString(text) > MaxTextRunes {
			return BatchTranslateResult{}, fmt.Errorf("%w", ErrTranslationTextTooLong)
		}
		texts[i] = text
	}

	if len(texts) == 1 {
		result, err := s.Translate(ctx, TranslateCommand{Text: texts[0], TargetLang: target})
		if err != nil {
			return BatchTranslateResult{}, err
		}
		return BatchTranslateResult{Texts: []string{result.Text}, TargetLang: target}, nil
	}

	merged, err := s.batchViaSegments(ctx, texts, target)
	if err != nil {
		return BatchTranslateResult{}, err
	}
	if batchComplete(merged) {
		return BatchTranslateResult{Texts: merged, TargetLang: target}, nil
	}
	// 分段协议失配（模型漏段/乱序/空段）：退回逐条，保证不差于单条链路；
	// 首个 provider 错误直接上抛（fail fast）。
	return s.batchViaFallback(ctx, texts, target)
}

// batchViaSegments 单次 LLM 调用翻译全部段：段间以编号标记行分隔，解析后
// 与输入对齐。任何解析失配表现为结果中存在空串（由调用方决定回退）。
func (s *Service) batchViaSegments(ctx context.Context, texts []string, target string) ([]string, error) {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("待译内容共 %d 段，逐段翻译成 %s；输出时保持每个段标记行（<<<SEG n>>>）原样，段标记行之外不要输出任何内容。\n", len(texts), target))
	b.WriteString("待译内容：\n")
	for i, text := range texts {
		b.WriteString(fmt.Sprintf("<<<SEG %d>>>\n%s\n", i+1, text))
	}
	resp, err := s.llm.Chat(ctx, llm.ChatRequest{
		Model:       s.params.Model,
		Messages:    []llm.ChatMessage{{Role: "system", Content: translationSystemPrompt}, {Role: "user", Content: b.String()}},
		Temperature: s.params.Temperature,
		MaxTokens:   s.params.MaxTokens,
		Options:     llm.RequestOptions{TimeoutMs: s.params.TimeoutMs},
	})
	if err != nil {
		return nil, fmt.Errorf("translation batch chat: %w", err)
	}
	return parseBatchSegments(resp.Content, len(texts)), nil
}

// parseBatchSegments 解析编号分段响应：标记必须恰好是 1..count 的完整升序
// 序列（缺失/乱序/越界/重复都算失配，返回全空由调用方退回逐条）——消息
// 正文可能被注入段标记，宽松解析会产生错位译文。
func parseBatchSegments(content string, count int) []string {
	out := make([]string, count)
	type segmentMark struct {
		num      int
		startEnd [2]int
	}
	var marks []segmentMark
	for _, m := range batchSegmentPattern.FindAllStringSubmatchIndex(content, -1) {
		num, err := strconv.Atoi(content[m[2]:m[3]])
		if err != nil {
			return out
		}
		marks = append(marks, segmentMark{num: num, startEnd: [2]int{m[0], m[1]}})
	}
	if len(marks) != count {
		return out
	}
	for i, mark := range marks {
		if mark.num != i+1 {
			return out
		}
		end := len(content)
		if i+1 < len(marks) {
			end = marks[i+1].startEnd[0]
		}
		out[i] = strings.TrimSpace(content[mark.startEnd[1]:end])
	}
	return out
}

// batchComplete 全段非空才算批量成功。
func batchComplete(texts []string) bool {
	for _, text := range texts {
		if strings.TrimSpace(text) == "" {
			return false
		}
	}
	return true
}

// batchViaFallback 逐条退路：单段失败留空串（不否决整批），provider 故障
// 直接上抛。
func (s *Service) batchViaFallback(ctx context.Context, texts []string, target string) (BatchTranslateResult, error) {
	out := make([]string, len(texts))
	for i, text := range texts {
		result, err := s.Translate(ctx, TranslateCommand{Text: text, TargetLang: target})
		if err != nil {
			return BatchTranslateResult{}, err
		}
		out[i] = result.Text
	}
	return BatchTranslateResult{Texts: out, TargetLang: target}, nil
}
