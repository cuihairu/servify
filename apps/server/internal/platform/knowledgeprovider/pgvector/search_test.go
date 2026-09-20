package pgvector

import (
	"math"
	"testing"
)

func TestCosineDistance(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
		epsilon  float32
	}{
		{
			name:     "identical vectors",
			a:        []float32{1, 2, 3},
			b:        []float32{1, 2, 3},
			expected: 0,
			epsilon:  1e-6,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1, 0},
			b:        []float32{0, 1},
			expected: 1,
			epsilon:  1e-6,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1, 1},
			b:        []float32{-1, -1},
			expected: 2,
			epsilon:  1e-6,
		},
		{
			name:     "similar vectors",
			a:        []float32{1, 2, 3},
			b:        []float32{2, 4, 6},
			expected: 0,
			epsilon:  1e-6,
		},
		{
			name:     "zero vectors",
			a:        []float32{0, 0, 0},
			b:        []float32{0, 0, 0},
			expected: 0,
			epsilon:  1e-6,
		},
		{
			name:     "one zero vector",
			a:        []float32{0, 0},
			b:        []float32{1, 1},
			expected: 0,
			epsilon:  1e-6,
		},
		{
			name:     "small vectors",
			a:        []float32{0.1, 0.2},
			b:        []float32{0.3, 0.4},
			expected: 0.016, // 约 0.016
			epsilon:  1e-3,
		},
		{
			name:     "negative values",
			a:        []float32{-1, 2, -3},
			b:        []float32{1, -2, 3},
			expected: 2,
			epsilon:  1e-6,
		},
		{
			name:     "high dimensional",
			a:        []float32{0.1, 0.2, 0.3, 0.4, 0.5},
			b:        []float32{0.5, 0.4, 0.3, 0.2, 0.1},
			expected: 0.3636, // 1 - 0.35/0.55 ≈ 0.3636
			epsilon:  1e-3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CosineDistance(tt.a, tt.b)
			diff := math.Abs(float64(result - tt.expected))
			if diff > float64(tt.epsilon) {
				t.Errorf("CosineDistance() = %v, want %v (diff=%v)", result, tt.expected, diff)
			}
		})
	}
}

func TestCosineDistance_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("CosineDistance should panic with different dimension vectors")
		}
	}()
	CosineDistance([]float32{1, 2}, []float32{1, 2, 3})
}

func TestEuclideanDistance(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
		epsilon  float32
	}{
		{
			name:     "identical vectors",
			a:        []float32{1, 2, 3},
			b:        []float32{1, 2, 3},
			expected: 0,
			epsilon:  1e-6,
		},
		{
			name:     "simple 2D",
			a:        []float32{0, 0},
			b:        []float32{3, 4},
			expected: 5,
			epsilon:  1e-6,
		},
		{
			name:     "simple 3D",
			a:        []float32{0, 0, 0},
			b:        []float32{1, 2, 2},
			expected: 3,
			epsilon:  1e-6,
		},
		{
			name:     "small difference",
			a:        []float32{1, 1},
			b:        []float32{2, 2},
			expected: 1.414, // sqrt(2)
			epsilon:  1e-3,
		},
		{
			name:     "negative coordinates",
			a:        []float32{-1, -1},
			b:        []float32{1, 1},
			expected: 2.828, // 2*sqrt(2)
			epsilon:  1e-3,
		},
		{
			name:     "zero vectors",
			a:        []float32{0, 0, 0},
			b:        []float32{0, 0, 0},
			expected: 0,
			epsilon:  1e-6,
		},
		{
			name:     "single dimension",
			a:        []float32{5},
			b:        []float32{10},
			expected: 5,
			epsilon:  1e-6,
		},
		{
			name:     "high dimensional",
			a:        []float32{1, 2, 3, 4, 5},
			b:        []float32{2, 3, 4, 5, 6},
			expected: 2.236, // sqrt(5)
			epsilon:  1e-3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EuclideanDistance(tt.a, tt.b)
			diff := math.Abs(float64(result - tt.expected))
			if diff > float64(tt.epsilon) {
				t.Errorf("EuclideanDistance() = %v, want %v (diff=%v)", result, tt.expected, diff)
			}
		})
	}
}

func TestEuclideanDistance_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("EuclideanDistance should panic with different dimension vectors")
		}
	}()
	EuclideanDistance([]float32{1, 2}, []float32{1, 2, 3})
}

func TestSqrt32(t *testing.T) {
	tests := []struct {
		name     string
		input    float32
		expected float32
		epsilon  float32
	}{
		{"zero", 0, 0, 1e-6},
		{"one", 1, 1, 1e-6},
		{"four", 4, 2, 1e-6},
		{"nine", 9, 3, 1e-6},
		{"sixteen", 16, 4, 1e-6},
		{"two", 2, 1.4142, 1e-4},
		{"three", 3, 1.732, 1e-3},
		{"quarter", 0.25, 0.5, 1e-6},
		{"hundredth", 0.01, 0.1, 1e-6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sqrt32(tt.input)
			diff := math.Abs(float64(result - tt.expected))
			if diff > float64(tt.epsilon) {
				t.Errorf("sqrt32(%v) = %v, want %v (diff=%v)", tt.input, result, tt.expected, diff)
			}
		})
	}
}

// TestDistanceConsistency 验证距离函数的一致性
func TestDistanceConsistency(t *testing.T) {
	a := []float32{1, 2, 3, 4}
	b := []float32{2, 3, 4, 5}

	// 余弦距离应该在 [0, 2] 范围内
	cosDist := CosineDistance(a, b)
	if cosDist < 0 || cosDist > 2 {
		t.Errorf("Cosine distance should be in [0, 2], got %v", cosDist)
	}

	// 欧几里得距离应该非负
	eucDist := EuclideanDistance(a, b)
	if eucDist < 0 {
		t.Errorf("Euclidean distance should be non-negative, got %v", eucDist)
	}

	// 相同向量的距离应该是 0
	if cosDist := CosineDistance(a, a); cosDist > 1e-6 {
		t.Errorf("Cosine distance of identical vectors should be ~0, got %v", cosDist)
	}
	if eucDist := EuclideanDistance(a, a); eucDist > 1e-6 {
		t.Errorf("Euclidean distance of identical vectors should be ~0, got %v", eucDist)
	}
}

// BenchmarkCosineDistance 性能测试
func BenchmarkCosineDistance(b *testing.B) {
	a := make([]float32, 1536)
	v := make([]float32, 1536)
	for i := range a {
		a[i] = 0.1
		v[i] = 0.2
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CosineDistance(a, v)
	}
}

// BenchmarkEuclideanDistance 性能测试
func BenchmarkEuclideanDistance(b *testing.B) {
	a := make([]float32, 1536)
	v := make([]float32, 1536)
	for i := range a {
		a[i] = 0.1
		v[i] = 0.2
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EuclideanDistance(a, v)
	}
}
