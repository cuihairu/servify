package pgvector

import (
	"math"
)

// CosineDistance 计算两个向量之间的余弦距离
// 余弦距离 = 1 - 余弦相似度
// 余弦相似度 = (a·b) / (||a|| * ||b||)
func CosineDistance(a, b []float32) float32 {
	if len(a) != len(b) {
		panic("vectors must have the same dimension")
	}
	if len(a) == 0 {
		return 0
	}

	var dotProduct float32
	var normA float32
	var normB float32

	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	normA = sqrt32(normA)
	normB = sqrt32(normB)

	if normA == 0 || normB == 0 {
		return 0
	}

	cosineSim := dotProduct / (normA * normB)
	return 1 - cosineSim
}

// EuclideanDistance 计算欧几里得距离（L2 距离）
// d(a,b) = sqrt(sum((a[i] - b[i])^2))
func EuclideanDistance(a, b []float32) float32 {
	if len(a) != len(b) {
		panic("vectors must have the same dimension")
	}
	if len(a) == 0 {
		return 0
	}

	var sum float32
	for i := range a {
		diff := a[i] - b[i]
		sum += diff * diff
	}

	return sqrt32(sum)
}

// sqrt32 计算 float32 的平方根
// 使用 math.Sqrt 然后转换回 float32 以保持精度一致
func sqrt32(x float32) float32 {
	return float32(math.Sqrt(float64(x)))
}
