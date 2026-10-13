package application

import (
	"reflect"
	"testing"
)

// 保留键编解码单元回归：坏 JSON/空表静默降级（nil），有效表按语言键
// 解析；合并语义先到先得、异语言并存、既有键表原样保留。
func TestDecodeMessageTranslations(t *testing.T) {
	if got := DecodeMessageTranslations(""); got != nil {
		t.Fatalf("empty raw must decode to nil, got %+v", got)
	}
	if got := DecodeMessageTranslations("{bad json"); got != nil {
		t.Fatalf("broken JSON must decode to nil, got %+v", got)
	}
	if got := DecodeMessageTranslations(`{"zh":"你好"}`); !reflect.DeepEqual(got, map[string]string{"zh": "你好"}) {
		t.Fatalf("valid table = %+v", got)
	}
	if got := DecodeMessageTranslations(`{"zh":"","en":"x"}`); got["zh"] != "" || got["en"] != "x" {
		t.Fatalf("empty-text language key must be kept, got %+v", got)
	}
	if got := DecodeMessageTranslations(`{}`); got != nil {
		t.Fatalf("empty object must decode to nil, got %+v", got)
	}
	if got := DecodeMessageTranslations(`["array"]`); got != nil {
		t.Fatalf("non-object JSON must decode to nil, got %+v", got)
	}
}

func TestMergeMessageTranslations(t *testing.T) {
	// 无既有表：新语言键写入。
	got := MergeMessageTranslations(map[string]string{"source": "admin"}, "en", "hello")
	if got["source"] != "admin" || got["translations"] != `{"en":"hello"}` {
		t.Fatalf("merge = %+v", got)
	}

	// 异语言并存。
	got = MergeMessageTranslations(got, "zh", "你好")
	if got["translations"] != `{"en":"hello","zh":"你好"}` {
		t.Fatalf("multi-language merge = %+v", got)
	}

	// 同语言键先到先得（并发标注竞态不互相覆盖）。
	got = MergeMessageTranslations(got, "en", "overwritten")
	if got["translations"] != `{"en":"hello","zh":"你好"}` {
		t.Fatalf("existing language key must win, got %q", got["translations"])
	}

	// 既有值是坏 JSON：视为无表，新键写入且不崩。
	got = MergeMessageTranslations(map[string]string{"translations": "{bad"}, "en", "hello")
	if got["translations"] != `{"en":"hello"}` {
		t.Fatalf("broken existing table must be replaced, got %q", got["translations"])
	}

	// nil base 不崩。
	if got = MergeMessageTranslations(nil, "ja", "こんにちは"); got["translations"] != `{"ja":"こんにちは"}` {
		t.Fatalf("nil base merge = %+v", got)
	}
}
