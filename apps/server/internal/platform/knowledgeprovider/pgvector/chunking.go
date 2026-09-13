package pgvector

import (
	"strings"
	"unicode"
)

// Chunker 负责将长文档分割成适合嵌入的小块
type Chunker struct {
	ChunkSize    int
	ChunkOverlap int
}

// NewChunker 创建新的 Chunker
func NewChunker(chunkSize, chunkOverlap int) *Chunker {
	if chunkSize <= 0 {
		chunkSize = 500
	}
	if chunkOverlap < 0 {
		chunkOverlap = 50
	}
	if chunkOverlap >= chunkSize {
		chunkOverlap = chunkSize / 10
	}
	return &Chunker{
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
	}
}

// Chunk 将文本分割成多个块
func (c *Chunker) Chunk(text string) []string {
	if text == "" {
		return []string{}
	}

	// 预处理文本：规范化空白字符
	text = normalizeWhitespace(text)
	if text == "" {
		return []string{}
	}

	// 如果文本较短，直接返回
	if len(text) <= c.ChunkSize {
		return []string{text}
	}

	var chunks []string
	runes := []rune(text)

	// 使用滑动窗口分块
	start := 0
	for start < len(runes) {
		end := start + c.ChunkSize
		if end > len(runes) {
			end = len(runes)
		}

		// 尝试在句子边界处分割
		chunkEnd := c.findSentenceBoundary(runes, start, end)
		chunk := string(runes[start:chunkEnd])

		if chunk != "" {
			chunks = append(chunks, chunk)
		}

		// 如果已经到达文本末尾，退出循环
		if chunkEnd >= len(runes) {
			break
		}

		// 移动到下一个块的起始位置（考虑重叠）
		nextStart := chunkEnd - c.ChunkOverlap
		if nextStart < 0 {
			nextStart = 0
		}
		// 防止无限循环：确保向前移动
		if nextStart <= start {
			nextStart = chunkEnd
		}
		start = nextStart
	}

	return chunks
}

// ChunkByParagraph 按段落分割文本
func (c *Chunker) ChunkByParagraph(text string) []string {
	if text == "" {
		return []string{}
	}

	// 先按换行分出段落，再在段落内部做空白规范化。
	// 若先对全文 normalizeWhitespace（Fields+Join " "），换行会被折叠成空格，
	// 多个段落会合并成单段，ChunkByParagraph 退化为按字符窗口切分。
	var validParagraphs []string
	for _, para := range strings.Split(text, "\n") {
		para = normalizeWhitespace(para)
		if para != "" {
			validParagraphs = append(validParagraphs, para)
		}
	}

	// 如果没有段落，返回空
	if len(validParagraphs) == 0 {
		return []string{}
	}

	// 如果段落数量较少且都较短，直接返回
	totalLength := 0
	for _, para := range validParagraphs {
		totalLength += len(para)
	}
	if len(validParagraphs) <= 3 && totalLength <= c.ChunkSize {
		return validParagraphs
	}

	// 将段落合并成块
	var chunks []string
	currentChunk := ""

	for _, para := range validParagraphs {
		testChunk := currentChunk
		if testChunk != "" {
			testChunk += "\n\n"
		}
		testChunk += para

		if len(testChunk) <= c.ChunkSize {
			currentChunk = testChunk
		} else {
			// 保存当前块
			if currentChunk != "" {
				chunks = append(chunks, currentChunk)
			}
			// 如果单个段落超过 ChunkSize，需要进一步分割
			if len(para) > c.ChunkSize {
				subChunks := c.chunkLongParagraph(para)
				chunks = append(chunks, subChunks...)
				currentChunk = ""
			} else {
				currentChunk = para
			}
		}
	}

	// 添加最后一个块
	if currentChunk != "" {
		chunks = append(chunks, currentChunk)
	}

	return chunks
}

// chunkLongParagraph 将过长的段落分割成小块
func (c *Chunker) chunkLongParagraph(para string) []string {
	runes := []rune(para)
	var chunks []string
	start := 0

	for start < len(runes) {
		end := start + c.ChunkSize
		if end > len(runes) {
			end = len(runes)
		}

		// 寻找合适的分割点
		chunkEnd := c.findSentenceBoundary(runes, start, end)
		chunk := string(runes[start:chunkEnd])

		if chunk != "" {
			chunks = append(chunks, chunk)
		}

		// 如果已经到达段落末尾，退出循环
		if chunkEnd >= len(runes) {
			break
		}

		// 移动到下一个块的起始位置（考虑重叠）
		nextStart := chunkEnd - c.ChunkOverlap
		if nextStart < 0 {
			nextStart = 0
		}
		// 防止无限循环：确保向前移动
		if nextStart <= start {
			nextStart = chunkEnd
		}
		start = nextStart
	}

	return chunks
}

// findSentenceBoundary 在指定范围内寻找句子边界
func (c *Chunker) findSentenceBoundary(runes []rune, start, end int) int {
	if end >= len(runes) {
		return len(runes)
	}

	// 从 end 向后查找句子结束符（最多查找 20% 的 chunkSize）
	searchLimit := end + c.ChunkSize/5
	if searchLimit > len(runes) {
		searchLimit = len(runes)
	}

	// 优先查找句子结束符
	sentenceEnders := []rune{'.', '!', '?', '。', '！', '？'}
	for i := end; i < searchLimit && i < len(runes); i++ {
		for _, ender := range sentenceEnders {
			if runes[i] == ender {
				// 确保后面是空格或行尾
				if i+1 >= len(runes) || unicode.IsSpace(runes[i+1]) {
					return i + 1
				}
			}
		}
	}

	// 如果找不到句子结束符，查找逗号或分号
	for i := end; i < searchLimit && i < len(runes); i++ {
		if runes[i] == ',' || runes[i] == ';' || runes[i] == '，' || runes[i] == '；' {
			return i + 1
		}
	}

	// 如果找不到标点，在空格处分割
	for i := end; i > start; i-- {
		if unicode.IsSpace(runes[i]) {
			return i + 1
		}
	}

	return end
}

// normalizeWhitespace 规范化文本中的空白字符
func normalizeWhitespace(text string) string {
	// 将多个空白字符替换为单个空格
	words := strings.Fields(text)
	return strings.Join(words, " ")
}

// CountTokens 估算文本的 token 数
// 这是一个简单的估算，实际 token 数取决于具体的 tokenizer
func CountTokens(text string) int {
	if text == "" {
		return 0
	}

	runes := []rune(text)
	tokenCount := 0

	for _, r := range runes {
		// 中文字符通常占用更多 token
		if isChinese(r) {
			tokenCount += 2
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			tokenCount += 1
		} else if unicode.IsSpace(r) {
			tokenCount += 1
		}
		// 标点符号通常不单独计算，与前面的词合并
	}

	// 根据常见的 token 估算公式调整
	// 英文大约每 4 个字符一个 token，中文大约每 2 个字符一个 token
	chineseChars := 0
	otherChars := 0

	for _, r := range runes {
		if isChinese(r) {
			chineseChars++
		} else if !unicode.IsSpace(r) {
			otherChars++
		}
	}

	estimatedTokens := chineseChars/2 + otherChars/4
	if estimatedTokens < 1 {
		estimatedTokens = 1
	}

	return estimatedTokens
}

// isChinese 判断字符是否为中文字符
func isChinese(r rune) bool {
	// 中文字符的 Unicode 范围
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK 统一汉字
		(r >= 0x3400 && r <= 0x4DBF) || // CJK 扩展 A
		(r >= 0x20000 && r <= 0x2A6DF) || // CJK 扩展 B
		(r >= 0x2A700 && r <= 0x2B73F) || // CJK 扩展 C
		(r >= 0x2B740 && r <= 0x2B81F) || // CJK 扩展 D
		(r >= 0x2B820 && r <= 0x2CEAF) || // CJK 扩展 E
		(r >= 0x2CEB0 && r <= 0x2EBEF) || // CJK 扩展 F
		(r >= 0x3000 && r <= 0x303F) || // CJK 符号和标点
		(r >= 0xFF00 && r <= 0xFFEF) // 全角字符
}
