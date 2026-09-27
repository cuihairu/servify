// Package local 提供零外部依赖的确定性哈希嵌入器（本地知识引擎的
// 向量底座）。与 openai/tei/xinference 等 HTTP provider 不同，它不发出
// 任何网络请求：文本经 CJK 二元组 + 拉丁词元切分后做特征哈希（FNV-1a）
// 折叠进固定维度，词频取次线性权重并做 L2 归一化。
//
// 定位是「本地零依赖部署的词法级语义基线」：同词元重叠的文本向量夹角
// 小、不重叠的正交，足以支撑检索排序；生产环境语义检索仍建议配置
// tei/xinference 等真实向量服务。同一文本恒得同一向量（可复现）。
package local

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

// Config 是本地嵌入器配置；Dim 非正时取默认维度。
type Config struct {
	Dim int
}

// DefaultDim 是未显式配置时的向量维度。
const DefaultDim = 256

// Provider 是零依赖确定性嵌入器，多 goroutine 并发安全（只读）。
type Provider struct {
	dim int
}

// NewProvider 构造本地嵌入器。
func NewProvider(cfg Config) *Provider {
	dim := cfg.Dim
	if dim <= 0 {
		dim = DefaultDim
	}
	return &Provider{dim: dim}
}

// Dimension 返回向量维度。
func (p *Provider) Dimension() int {
	if p == nil {
		return DefaultDim
	}
	return p.dim
}

// HealthCheck 本地嵌入器无外部依赖，恒健康。
func (p *Provider) HealthCheck(_ context.Context) error { return nil }

// Embed 把每段文本折叠为 L2 归一化的定维向量；空文本返回零向量。
func (p *Provider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = p.EmbedOne(text)
	}
	return out, nil
}

// EmbedOne 单文本嵌入（Embed 的单条形式，供检索侧重用与测试）。
func (p *Provider) EmbedOne(text string) []float32 {
	vec := make([]float32, p.Dimension())
	if strings.TrimSpace(text) == "" {
		return vec
	}
	counts := make(map[string]int, len(text)/2)
	for _, tok := range tokenize(text) {
		counts[tok]++
	}
	for tok, tf := range counts {
		weight := float32(1 + math.Log(float64(tf)))
		idx, sign := project(tok, p.Dimension())
		if sign {
			vec[idx] += weight
		} else {
			vec[idx] -= weight
		}
	}
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	if norm == 0 {
		return vec
	}
	inv := float32(1 / math.Sqrt(norm))
	for i := range vec {
		vec[i] *= inv
	}
	return vec
}

// project 把词元哈希折叠进 [0,dim) 桶；符号位取同一哈希的高位段。
// 注意不能对 token 拼后缀再哈希取 LSB：FNV-1a 的哈希 LSB 遥等于全部
// 输入字节 LSB 的异或，与桶下标的低位完全相关（同桶恒同号，符号通道
// 失效）；高位段与其低位近独立，才是真正的第二通道（标准特征哈希做法）。
func project(token string, dim int) (int, bool) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(token))
	sum := h.Sum32()
	return int(sum) % dim, sum>>16&1 == 1
}

// tokenize 切分文本：拉丁/数字连续段为一个词元；CJK 连续段取字符二元
// 组（单字符段退化为二元组含自身）。全部小写化以稳定哈希。pool 复用
// 避免热路径反复分配。
func tokenize(text string) []string {
	tokens := make([]string, 0, len(text)/2+1)
	var latin strings.Builder
	var cjk strings.Builder

	flushLatin := func() {
		if latin.Len() > 0 {
			tokens = append(tokens, strings.ToLower(latin.String()))
			latin.Reset()
		}
	}
	flushCJK := func() {
		runes := []rune(cjk.String())
		cjk.Reset()
		if len(runes) == 1 {
			tokens = append(tokens, strings.ToLower(string(runes[0])))
			return
		}
		for i := 0; i+1 < len(runes); i++ {
			tokens = append(tokens, strings.ToLower(string(runes[i])+string(runes[i+1])))
		}
	}

	for _, r := range text {
		switch {
		case unicode.IsLetter(r) && isCJK(r):
			flushLatin()
			cjk.WriteRune(r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushCJK()
			latin.WriteRune(r)
		default:
			flushLatin()
			flushCJK()
		}
	}
	flushLatin()
	flushCJK()
	return tokens
}

// isCJK 判断是否中日韩统一表意文字（检索主要面向中文客服语料）。
func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF)
}
