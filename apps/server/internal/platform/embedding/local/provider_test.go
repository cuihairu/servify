package local

import (
	"context"
	"math"
	"testing"
)

// TestProviderDeterministicAndNormalized 同一文本恒得同一向量，且非零向量
// L2 范数为 1（检索侧阈值语义依赖归一化）。
func TestProviderDeterministicAndNormalized(t *testing.T) {
	p := NewProvider(Config{})
	a := p.EmbedOne("如何申请退款退货政策")
	b := p.EmbedOne("如何申请退款退货政策")
	if len(a) != DefaultDim || len(b) != DefaultDim {
		t.Fatalf("dim = %d/%d, want %d", len(a), len(b), DefaultDim)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("vector not deterministic at %d: %v vs %v", i, a[i], b[i])
		}
	}
	var norm float64
	for _, v := range a {
		norm += float64(v) * float64(v)
	}
	if math.Abs(norm-1) > 1e-5 {
		t.Fatalf("L2 norm = %v, want ~1", norm)
	}
}

// TestProviderCJKBigramsAndLatinTokens 中文按字符二元组切分、拉丁词按整词
// 切分：中文相邻文本共享二元组（相似度>0），不同语言文本互不重叠（正交）。
func TestProviderCJKBigramsAndLatinTokens(t *testing.T) {
	p := NewProvider(Config{})
	cjkA := p.EmbedOne("退货政策说明")
	cjkB := p.EmbedOne("退货政策与流程")
	if similar := cosine(cjkA, cjkB); similar <= 0 {
		t.Fatalf("overlapping CJK bigrams should give positive cosine, got %v", similar)
	}
	latin := p.EmbedOne("refund policy overview")
	if cosine(cjkA, latin) != 0 {
		t.Fatalf("disjoint scripts should be orthogonal, got %v", cosine(cjkA, latin))
	}
	if len(p.EmbedOne("refund 123 mix 混合")) == 0 {
		t.Fatalf("mixed text should embed to full dim")
	}
}

// TestProviderEmptyText 空白文本返回零向量（不参与相似度排序）。
func TestProviderEmptyText(t *testing.T) {
	p := NewProvider(Config{})
	for _, text := range []string{"", "   ", "\n\t"} {
		vec := p.EmbedOne(text)
		if len(vec) != DefaultDim {
			t.Fatalf("dim = %d, want %d", len(vec), DefaultDim)
		}
		for _, v := range vec {
			if v != 0 {
				t.Fatalf("blank text should produce zero vector, got %v", vec)
			}
		}
	}
}

// TestProviderDimensionOverride 与零值/负值兜底默认维度。
func TestProviderDimensionOverride(t *testing.T) {
	if got := NewProvider(Config{Dim: 64}).Dimension(); got != 64 {
		t.Fatalf("dim = %d, want 64", got)
	}
	if got := NewProvider(Config{Dim: 0}).Dimension(); got != DefaultDim {
		t.Fatalf("dim = %d, want default %d", got, DefaultDim)
	}
	if got := NewProvider(Config{Dim: -3}).Dimension(); got != DefaultDim {
		t.Fatalf("dim = %d, want default %d", got, DefaultDim)
	}
}

// TestProviderEmbedBatch 与 HealthCheck：批量逐条等价、健康恒可用、nil 接收者兜底。
func TestProviderEmbedBatchAndHealth(t *testing.T) {
	p := NewProvider(Config{Dim: 128})
	vecs, err := p.Embed(context.Background(), []string{"订单查询", "退款进度"})
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("len = %d, want 2", len(vecs))
	}
	for i, text := range []string{"订单查询", "退款进度"} {
		if vecs[i][0] != p.EmbedOne(text)[0] {
			t.Fatalf("batch item %d mismatch with EmbedOne", i)
		}
	}
	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck = %v, want nil", err)
	}
	var nilP *Provider
	if nilP.Dimension() != DefaultDim {
		t.Fatalf("nil receiver dim = %d, want default", nilP.Dimension())
	}
}

// TestProviderSingleCharCJK 单字符 CJK 段退化为单字符词元（无二元组可成）。
func TestProviderSingleCharCJK(t *testing.T) {
	p := NewProvider(Config{})
	vec := p.EmbedOne("好")
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	if math.Abs(norm-1) > 1e-5 {
		t.Fatalf("single CJK char should normalize to unit vector, norm = %v", norm)
	}
}

// TestProviderZeroNormCollision 词元哈希可在同一桶内符号相消（±w 抵平），
// 向量必须安全回到零向量而不产生 NaN。测试用确定性搜索构造碰撞对。
func TestProviderZeroNormCollision(t *testing.T) {
	p := NewProvider(Config{Dim: 256})
	var words []string
	for a := 'a'; a <= 'z'; a++ {
		for b := 'a'; b <= 'z'; b++ {
			words = append(words, string(a)+string(b))
		}
	}
	found := 0
	for i := 0; i < len(words) && found < 2; i++ {
		for j := i + 1; j < len(words) && found < 2; j++ {
			idxI, signI := project(words[i], 256)
			idxJ, signJ := project(words[j], 256)
			if idxI == idxJ && signI != signJ {
				vec := p.EmbedOne(words[i] + " " + words[j])
				zero := true
				for _, v := range vec {
					if v != 0 {
						zero = false
						break
					}
				}
				if !zero {
					continue
				}
				found++
				// 相消向量应恒为零，不与任何文本产生虚假相似度。
				other := p.EmbedOne("退款退货政策与流程说明")
				if cosine(vec, other) != 0 || vec[0] != 0 {
					t.Fatalf("zero-norm collision vector must stay zero and orthogonal")
				}
			}
		}
	}
	if found == 0 {
		t.Fatalf("collision pair not found in search space")
	}
}

// TestEmbedEmptyBatch 空批量返回空结果。
func TestEmbedEmptyBatch(t *testing.T) {
	p := NewProvider(Config{})
	vecs, err := p.Embed(context.Background(), nil)
	if err != nil || len(vecs) != 0 {
		t.Fatalf("Embed(nil) = %v, %v; want empty, nil", vecs, err)
	}
}

// cosine 计算两条向量的余弦相似度（测试断言专用）。
func cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return math.NaN()
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
