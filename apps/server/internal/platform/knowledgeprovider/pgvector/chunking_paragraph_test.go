package pgvector

import (
	"strings"
	"testing"
)

// TestChunkByParagraphKeepsParagraphsSeparate 是行为修复的回归测试:
// 修复前 ChunkByParagraph 先对全文 normalizeWhitespace,换行被折叠成空格,
// strings.Split(text, "\n") 永远只有一个元素,多个段落被合并后按字符窗口
// 重新切分。修复后按段落切分先于段内规范化,段落结构得以保留。
func TestChunkByParagraphKeepsParagraphsSeparate(t *testing.T) {
	c := NewChunker(500, 50)

	chunks := c.ChunkByParagraph("first paragraph body\n\nsecond paragraph body\n\nthird paragraph body")
	if len(chunks) != 3 {
		t.Fatalf("ChunkByParagraph() = %v, want 3 separate paragraphs", chunks)
	}
	want := []string{"first paragraph body", "second paragraph body", "third paragraph body"}
	for i, chunk := range chunks {
		if chunk != want[i] {
			t.Fatalf("chunk %d = %q, want %q", i, chunk, want[i])
		}
	}
}

// TestChunkByParagraphMergesLongParagraphs 覆盖多段落进入合并循环的路径:
// 段落数 > 3 时不再提前返回,短段落以 "\n\n" 连接合并,超长段落由
// chunkLongParagraph 进一步切分。
func TestChunkByParagraphMergesLongParagraphs(t *testing.T) {
	c := NewChunker(40, 5)

	chunks := c.ChunkByParagraph("alpha beta\n\ngamma delta\n\nepsilon zeta\n\n" + strings.Repeat("omega ", 30))
	if len(chunks) < 2 {
		t.Fatalf("expected merged/split chunks, got %v", chunks)
	}
	joined := strings.Join(chunks, "\n---\n")
	for _, word := range []string{"alpha beta", "gamma delta", "epsilon zeta", "omega"} {
		if !strings.Contains(joined, word) {
			t.Fatalf("joined chunks lost content %q: %v", word, chunks)
		}
	}
	// 合并块内的段落分隔符是 "\n\n",证明段落边界在合并时被保留。
	if !strings.Contains(joined, "alpha beta\n\ngamma delta") {
		t.Fatalf("paragraph boundary not preserved during merge: %v", chunks)
	}
}

// TestChunkByParagraphWhitespaceOnlyLines 覆盖全部段落为空白时
// len(validParagraphs) == 0 的空返回分支。
func TestChunkByParagraphWhitespaceOnlyLines(t *testing.T) {
	c := NewChunker(100, 10)

	if got := c.ChunkByParagraph("\n \n\t\n   \n"); len(got) != 0 {
		t.Fatalf("ChunkByParagraph(whitespace-only lines) = %v, want empty", got)
	}
}
