package domain

import (
	"database/sql/driver"
	"reflect"
	"testing"

	"github.com/pgvector/pgvector-go"
)

func TestEmbeddingScanEmptyVariants(t *testing.T) {
	cases := []struct {
		name string
		src  any
	}{
		{"nil src", nil},
		{"empty string", ""},
		{"blank string", "   "},
		{"empty literal string", "[]"},
		{"padded empty literal", " [] "},
		{"empty bytes", []byte("")},
		{"empty literal bytes", []byte("[]")},
		{"padded empty literal bytes", []byte("  []  ")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var e Embedding
			// 预置非空向量，验证空输入会清零而不是残留
			e.Vector = pgvector.NewVector([]float32{1, 2})
			if err := e.Scan(tc.src); err != nil {
				t.Fatalf("Scan(%v) error = %v", tc.src, err)
			}
			if got := e.Vector.Slice(); len(got) != 0 {
				t.Fatalf("Scan(%v) left vector %v, want empty", tc.src, got)
			}
			// 空向量写库必须序列化为 NULL，而不是 "[]" 字面量
			val, err := e.Value()
			if err != nil {
				t.Fatalf("Value() error = %v", err)
			}
			if val != nil {
				t.Fatalf("Value() for empty vector = %v, want nil", val)
			}
		})
	}
}

func TestEmbeddingScanRoundTrip(t *testing.T) {
	original := NewEmbedding([]float32{1.5, -2.25, 0})
	if len(original.Vector.Slice()) != 3 {
		t.Fatalf("NewEmbedding produced %v", original.Vector.Slice())
	}

	val, err := original.Value()
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}
	literal, ok := val.(string)
	if !ok {
		t.Fatalf("Value() type = %T, want string", val)
	}

	var restored Embedding
	if err := restored.Scan(literal); err != nil {
		t.Fatalf("Scan(%v) error = %v", literal, err)
	}
	if !reflect.DeepEqual(original.Vector.Slice(), restored.Vector.Slice()) {
		t.Fatalf("round trip mismatch: %v vs %v", original.Vector.Slice(), restored.Vector.Slice())
	}
}

func TestEmbeddingScanBytesAndUnsupported(t *testing.T) {
	var fromBytes Embedding
	if err := fromBytes.Scan([]byte("[3.5,4.5]")); err != nil {
		t.Fatalf("Scan([]byte) error = %v", err)
	}
	if got := fromBytes.Vector.Slice(); len(got) != 2 || got[0] != 3.5 {
		t.Fatalf("Scan([]byte) = %v", got)
	}

	var unsupported Embedding
	if err := unsupported.Scan(42); err == nil {
		t.Fatal("expected error for unsupported src type")
	}

	// 编译期保证 driver.Valuer 约束仍然成立（空向量 -> nil 已在上面验证）
	var _ driver.Valuer = Embedding{}
}

func TestIsEmptyVectorLiteral(t *testing.T) {
	cases := map[string]bool{
		"":        true,
		"  ":      true,
		"[]":      true,
		" [] ":    true,
		"[1,2]":   false,
		"[0,0]":   false,
		"x":       false,
		"[ ] [] ": false,
	}
	for input, want := range cases {
		if got := isEmptyVectorLiteral(input); got != want {
			t.Fatalf("isEmptyVectorLiteral(%q) = %v, want %v", input, got, want)
		}
	}
}
