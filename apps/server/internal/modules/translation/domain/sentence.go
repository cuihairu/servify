package domain

import (
	"strings"
	"unicode"
)

// Sentence 切出的完整句：Seq 为 ASR 说话轮序号（同轮切出的多句共享），
// Text 为含结尾标点的原文字句。
type Sentence struct {
	Seq  int64
	Text string
}

// DefaultMaxSentenceRunes 单句长度上限（设计文档 §2.2：长度 ≥ 60 字切句，
// 防长句霸屏与翻译超时）。
const DefaultMaxSentenceRunes = 60

// sentenceBoundaryPuncts 句界标点（§2.2 边界触发：。？！.?!?）。
const sentenceBoundaryPuncts = "。？！?!"

// SentenceAssembler 分句器（设计文档 §2.2 双触发，先到先切）：
//  1. 静音触发（主）：说话轮结束（Flush）把残余 buffer 切为一整句——
//     VAD 尾点静音 ≥ 500ms 由 ASR provider 侧判定的 speech_end 驱动；
//  2. 边界触发（辅）：句界标点即切；无标点且当前句长 ≥ 上限（默认 60 字）
//     硬切。文本流按字符位置先到先切：同一位置标点与长度同时满足时标点优先。
//
// 同轮二次 final 按 seq 覆盖（provider 重发权威文本时不叠加）。partial 不进
// 分句器（只驱动"正在说"），仅 final 进翻译队列。
type SentenceAssembler struct {
	maxRunes int
	buffer   string
	lastSeq  int64
	hasBuf   bool
}

// NewSentenceAssembler 构造默认上限的分句器。
func NewSentenceAssembler() *SentenceAssembler {
	return &SentenceAssembler{maxRunes: DefaultMaxSentenceRunes}
}

// NewSentenceAssemblerWithLimit 以自定义单句上限构造（≤ 0 回落默认值）。
func NewSentenceAssemblerWithLimit(maxRunes int) *SentenceAssembler {
	if maxRunes <= 0 {
		maxRunes = DefaultMaxSentenceRunes
	}
	return &SentenceAssembler{maxRunes: maxRunes}
}

// Feed 喂入一条 ASR final 文本，返回因本次喂入而切出的完整句。
// 换轮（seq 变化）时先按静音语义收掉上一轮残余。
func (a *SentenceAssembler) Feed(seq int64, text string) []Sentence {
	if a.hasBuf && seq != a.lastSeq {
		// 防御性轮边界：上一轮没等 Flush 就来了新轮（provider 异常时序），
		// 残余按完整句收掉，不与新轮内容混排。
		return append(a.flushLocked(), a.cut(seq, text)...)
	}
	a.buffer = text // 同 seq 二次 final：覆盖，不追加
	a.lastSeq = seq
	a.hasBuf = true
	return a.cutReady(seq)
}

// Flush 说话轮结束（VAD 尾点）或会话收尾：残余 buffer 切为一整句。
func (a *SentenceAssembler) Flush() []Sentence {
	return a.flushLocked()
}

func (a *SentenceAssembler) flushLocked() []Sentence {
	out := []Sentence{}
	if trimmed := strings.TrimSpace(a.buffer); trimmed != "" {
		out = append(out, Sentence{Seq: a.lastSeq, Text: a.buffer})
	}
	a.buffer = ""
	a.hasBuf = false
	return out
}

// cutReady 对当前 buffer 反复施用双触发切句，直到余量既无句界标点
// 也未达长度上限。
func (a *SentenceAssembler) cutReady(seq int64) []Sentence {
	return a.cut(seq, "")
}

// cut 以 newText 作为 buffer 新内容（"" = 保持现内容）执行切句扫描：
// 沿字符位置单趟扫描，标点与长度上限先到者定切点；切出的空白片段丢弃，
// 余量修剪前导空白（句子不以空格开头）。
func (a *SentenceAssembler) cut(seq int64, newText string) []Sentence {
	if newText != "" || !a.hasBuf {
		a.buffer = strings.TrimLeftFunc(newText, unicode.IsSpace)
		a.lastSeq = seq
		a.hasBuf = true
	}
	out := []Sentence{}
	for a.buffer != "" {
		cutAt := -1
		runes := 0
		for i, r := range a.buffer {
			runes++
			if strings.ContainsRune(sentenceBoundaryPuncts, r) {
				cutAt = i + len(string(r)) // 边界触发：标点处切句（含标点）
				break
			}
			if runes == a.maxRunes {
				cutAt = i + len(string(r)) // 长度触发：未见标点，硬切
				break
			}
		}
		if cutAt < 0 {
			break // 余量既无标点也未满上限：留在 buffer 等 Flush/后续 final
		}
		piece := a.buffer[:cutAt]
		a.buffer = strings.TrimLeftFunc(a.buffer[cutAt:], unicode.IsSpace)
		if strings.TrimSpace(piece) != "" {
			out = append(out, Sentence{Seq: seq, Text: piece})
		}
	}
	return out
}
