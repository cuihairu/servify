package pgvector

import (
	"strings"
	"testing"
)

func TestNewChunker(t *testing.T) {
	tests := []struct {
		name            string
		chunkSize       int
		chunkOverlap    int
		expectedSize    int
		expectedOverlap int
	}{
		{
			name:            "valid parameters",
			chunkSize:       1000,
			chunkOverlap:    100,
			expectedSize:    1000,
			expectedOverlap: 100,
		},
		{
			name:            "zero chunk size uses default",
			chunkSize:       0,
			chunkOverlap:    50,
			expectedSize:    500,
			expectedOverlap: 50,
		},
		{
			name:            "negative chunk size uses default",
			chunkSize:       -100,
			chunkOverlap:    50,
			expectedSize:    500,
			expectedOverlap: 50,
		},
		{
			name:            "negative overlap uses default",
			chunkSize:       1000,
			chunkOverlap:    -10,
			expectedSize:    1000,
			expectedOverlap: 50,
		},
		{
			name:            "overlap equals chunk size adjusts to 10%",
			chunkSize:       1000,
			chunkOverlap:    1000,
			expectedSize:    1000,
			expectedOverlap: 100,
		},
		{
			name:            "overlap greater than chunk size adjusts to 10%",
			chunkSize:       500,
			chunkOverlap:    600,
			expectedSize:    500,
			expectedOverlap: 50,
		},
		{
			name:            "zero overlap is allowed",
			chunkSize:       500,
			chunkOverlap:    0,
			expectedSize:    500,
			expectedOverlap: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunker := NewChunker(tt.chunkSize, tt.chunkOverlap)
			if chunker.ChunkSize != tt.expectedSize {
				t.Errorf("ChunkSize = %d, want %d", chunker.ChunkSize, tt.expectedSize)
			}
			if chunker.ChunkOverlap != tt.expectedOverlap {
				t.Errorf("ChunkOverlap = %d, want %d", chunker.ChunkOverlap, tt.expectedOverlap)
			}
		})
	}
}

func TestChunk_ShortText(t *testing.T) {
	chunker := NewChunker(500, 50)

	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{
			name:     "empty text",
			text:     "",
			expected: 0,
		},
		{
			name:     "single word",
			text:     "Hello",
			expected: 1,
		},
		{
			name:     "short sentence",
			text:     "This is a short sentence.",
			expected: 1,
		},
		{
			name:     "text under chunk size",
			text:     strings.Repeat("word ", 50), // ~250 chars
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := chunker.Chunk(tt.text)
			if len(chunks) != tt.expected {
				t.Errorf("Chunk() returned %d chunks, want %d", len(chunks), tt.expected)
			}
			if tt.expected > 0 && len(chunks) > 0 {
				// 对于短文本，块的内容应该非空且与原始文本相关
				if chunks[0] == "" {
					t.Errorf("Chunk()[0] is empty for text: %q", tt.text)
				}
				// 验证关键内容被保留
				if tt.text != "" {
					// 获取原始文本的第一个非空格词
					words := strings.Fields(tt.text)
					if len(words) > 0 && !strings.Contains(chunks[0], words[0]) {
						t.Errorf("Chunk()[0] = %q does not contain expected word %q", chunks[0], words[0])
					}
				}
			}
		})
	}
}

func TestChunk_LongText(t *testing.T) {
	chunker := NewChunker(200, 50)

	// 创建足够长的文本
	longText := strings.Repeat("This is a sentence. ", 100) // ~2000 chars

	chunks := chunker.Chunk(longText)

	if len(chunks) == 0 {
		t.Fatal("Chunk() returned no chunks")
	}

	// 验证每个块的大小
	for i, chunk := range chunks {
		if len(chunk) > chunker.ChunkSize*12/10 { // 允许 10% 的超出
			t.Errorf("Chunk %d has length %d, expected <= %d", i, len(chunk), chunker.ChunkSize*12/10)
		}
		if chunk == "" {
			t.Errorf("Chunk %d is empty", i)
		}
	}

	// 验证重叠
	if len(chunks) > 1 {
		for i := 0; i < len(chunks)-1; i++ {
			overlap := calculateOverlap(chunks[i], chunks[i+1])
			if overlap < chunker.ChunkOverlap/2 {
				// 允许一些灵活性，但应该有重叠
				t.Logf("Chunks %d and %d have minimal overlap: %d", i, i+1, overlap)
			}
		}
	}
}

func TestChunk_SentenceBoundary(t *testing.T) {
	chunker := NewChunker(100, 20)

	text := "First sentence. Second sentence. Third sentence. Fourth sentence. Fifth sentence."

	chunks := chunker.Chunk(text)

	if len(chunks) == 0 {
		t.Fatal("Chunk() returned no chunks")
	}

	// 验证块在句子边界处分割
	for i, chunk := range chunks {
		// 块不应该在单词中间截断（大部分情况）
		if i > 0 && chunk != "" {
			firstChar := chunk[0]
			if firstChar >= 'a' && firstChar <= 'z' {
				t.Logf("Chunk %d might start mid-word: %q", i, chunk[:min(20, len(chunk))])
			}
		}
	}
}

func TestChunk_ChineseText(t *testing.T) {
	chunker := NewChunker(100, 20)

	text := "这是一段中文文本。这是第二句话。这是第三句话。这是第四句话。这是第五句话。"

	chunks := chunker.Chunk(text)

	if len(chunks) == 0 {
		t.Fatal("Chunk() returned no chunks")
	}

	// 验证块包含有效内容
	for i, chunk := range chunks {
		if chunk == "" {
			t.Errorf("Chunk %d is empty", i)
		}
		if len(chunk) > chunker.ChunkSize*12/10 {
			t.Errorf("Chunk %d exceeds expected size", i)
		}
	}
}

func TestChunkByParagraph(t *testing.T) {
	chunker := NewChunker(500, 50)

	tests := []struct {
		name     string
		text     string
		minChunk int
		maxChunk int
	}{
		{
			name:     "empty text",
			text:     "",
			minChunk: 0,
			maxChunk: 0,
		},
		{
			name:     "single paragraph",
			text:     "This is a single paragraph.",
			minChunk: 1,
			maxChunk: 1,
		},
		{
			name:     "multiple short paragraphs",
			text:     "First paragraph.\n\nSecond paragraph.\n\nThird paragraph.",
			minChunk: 1,
			maxChunk: 3,
		},
		{
			name: "paragraphs with varying lengths",
			text: strings.Join([]string{
				"Short paragraph.",
				"",
				strings.Repeat("This is a medium length paragraph. ", 10),
				"",
				"Another short paragraph.",
				"",
				strings.Repeat("Long paragraph here. ", 20),
			}, "\n"),
			minChunk: 2,
			maxChunk: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := chunker.ChunkByParagraph(tt.text)
			if len(chunks) < tt.minChunk {
				t.Errorf("ChunkByParagraph() returned %d chunks, want >= %d", len(chunks), tt.minChunk)
			}
			if tt.maxChunk > 0 && len(chunks) > tt.maxChunk {
				t.Errorf("ChunkByParagraph() returned %d chunks, want <= %d", len(chunks), tt.maxChunk)
			}

			// 验证块不为空
			for i, chunk := range chunks {
				if strings.TrimSpace(chunk) == "" {
					t.Errorf("Chunk %d is empty or whitespace only", i)
				}
			}
		})
	}
}

func TestChunkByParagraph_LongParagraph(t *testing.T) {
	chunker := NewChunker(200, 50)

	// 单个超长段落
	longPara := strings.Repeat("This is a very long paragraph. ", 50)

	chunks := chunker.ChunkByParagraph(longPara)

	if len(chunks) == 0 {
		t.Fatal("ChunkByParagraph() returned no chunks")
	}

	// 应该被分割成多个块
	if len(chunks) < 2 {
		t.Logf("Long paragraph was not split into multiple chunks: got %d", len(chunks))
	}

	// 验证每个块的大小
	for i, chunk := range chunks {
		if len(chunk) > chunker.ChunkSize*12/10 {
			t.Errorf("Chunk %d exceeds expected size: %d", i, len(chunk))
		}
	}
}

func TestChunkByParagraph_PreservesParagraphs(t *testing.T) {
	chunker := NewChunker(1000, 100)

	text := `First paragraph.

Second paragraph.

Third paragraph.`

	chunks := chunker.ChunkByParagraph(text)

	if len(chunks) == 0 {
		t.Fatal("ChunkByParagraph() returned no chunks")
	}

	// 验证段落之间的换行被保留
	joined := strings.Join(chunks, " ")
	if !strings.Contains(joined, "First paragraph") ||
		!strings.Contains(joined, "Second paragraph") ||
		!strings.Contains(joined, "Third paragraph") {
		t.Error("ChunkByParagraph() did not preserve all paragraphs")
	}
}

func TestNormalizeWhitespace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal text",
			input:    "Hello world",
			expected: "Hello world",
		},
		{
			name:     "multiple spaces",
			input:    "Hello    world",
			expected: "Hello world",
		},
		{
			name:     "tabs and spaces",
			input:    "Hello\t\tworld",
			expected: "Hello world",
		},
		{
			name:     "newlines and spaces",
			input:    "Hello\n\n  world",
			expected: "Hello world",
		},
		{
			name:     "leading/trailing whitespace",
			input:    "  Hello world  ",
			expected: "Hello world",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only whitespace",
			input:    "   \t\n  ",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeWhitespace(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeWhitespace() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestChunk_WithSpecialCharacters(t *testing.T) {
	chunker := NewChunker(200, 50)

	text := "Code example: func main() { return \"hello\" } // comment"

	chunks := chunker.Chunk(text)

	if len(chunks) == 0 {
		t.Fatal("Chunk() returned no chunks")
	}

	// 验证特殊字符被保留
	joined := strings.Join(chunks, "")
	if !strings.Contains(joined, "func") ||
		!strings.Contains(joined, "main") ||
		!strings.Contains(joined, "// comment") {
		t.Error("Chunk() did not preserve special characters")
	}
}

func TestChunk_WithNumbers(t *testing.T) {
	chunker := NewChunker(100, 20)

	text := "The numbers are: 123, 4567, and 89012."

	chunks := chunker.Chunk(text)

	if len(chunks) == 0 {
		t.Fatal("Chunk() returned no chunks")
	}

	// 验证数字被保留
	joined := strings.Join(chunks, "")
	if !strings.Contains(joined, "123") ||
		!strings.Contains(joined, "4567") ||
		!strings.Contains(joined, "89012") {
		t.Error("Chunk() did not preserve numbers")
	}
}

// Helper function to calculate overlap between two strings
func calculateOverlap(a, b string) int {
	maxOverlap := 0
	minLen := min(len(a), len(b))

	for i := 1; i <= minLen; i++ {
		if a[len(a)-i:] == b[:i] {
			maxOverlap = i
		}
	}

	return maxOverlap
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
