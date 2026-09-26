package domain_test

import (
	"reflect"
	"strings"
	"testing"

	"servify/apps/server/internal/modules/translation/domain"
)

func sentences(seq int64, texts ...string) []domain.Sentence {
	out := make([]domain.Sentence, 0, len(texts))
	for _, text := range texts {
		out = append(out, domain.Sentence{Seq: seq, Text: text})
	}
	return out
}

func TestFeedBoundaryPunctCutsImmediately(t *testing.T) {
	a := domain.NewSentenceAssembler()
	if got := a.Feed(1, "你好。"); !reflect.DeepEqual(got, sentences(1, "你好。")) {
		t.Fatalf("Feed = %+v", got)
	}
	if got := a.Flush(); len(got) != 0 {
		t.Fatalf("Flush after full cut = %+v, want empty", got)
	}
}

func TestFeedMultipleSentencesInOneFinal(t *testing.T) {
	a := domain.NewSentenceAssembler()
	got := a.Feed(1, "你好。再见。谢谢！")
	want := sentences(1, "你好。", "再见。", "谢谢！")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Feed = %+v, want %+v", got, want)
	}
}

func TestFeedUnfinishedTextStaysBufferedUntilFlush(t *testing.T) {
	a := domain.NewSentenceAssembler()
	if got := a.Feed(1, "我想问一下订单"); len(got) != 0 {
		t.Fatalf("Feed without boundary = %+v, want empty", got)
	}
	if got := a.Flush(); !reflect.DeepEqual(got, sentences(1, "我想问一下订单")) {
		t.Fatalf("Flush = %+v", got)
	}
}

func TestFeedWesternPunctAndMixed(t *testing.T) {
	a := domain.NewSentenceAssembler()
	got := a.Feed(1, "Hello there! How are you?")
	want := sentences(1, "Hello there!", "How are you?")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Feed = %+v, want %+v", got, want)
	}
}

func TestFeedLengthHardCutWhenNoPunct(t *testing.T) {
	a := domain.NewSentenceAssemblerWithLimit(5)
	got := a.Feed(1, "abcdefgh") // 8 字 > 5：硬切 5 + 余 3
	want := sentences(1, "abcde")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Feed = %+v, want %+v", got, want)
	}
	if got := a.Flush(); !reflect.DeepEqual(got, sentences(1, "fgh")) {
		t.Fatalf("Flush = %+v, want [fgh]", got)
	}
}

func TestFeedBoundaryWinsAtLimitPosition(t *testing.T) {
	// 第 5 字恰为标点：边界与长度同时满足，标点优先（先到先切同点）。
	a := domain.NewSentenceAssemblerWithLimit(5)
	got := a.Feed(1, "abcd。xyz")
	if !reflect.DeepEqual(got, sentences(1, "abcd。")) {
		t.Fatalf("Feed = %+v, want [abcd。]", got)
	}
	if got := a.Flush(); !reflect.DeepEqual(got, sentences(1, "xyz")) {
		t.Fatalf("Flush = %+v, want [xyz]", got)
	}
}

func TestFeedMultiByteTextCutAtRuneBoundary(t *testing.T) {
	// CJK 多字节：硬切必须落在字符边界（3 字上限）。
	a := domain.NewSentenceAssemblerWithLimit(3)
	got := a.Feed(1, "你好世界")
	if !reflect.DeepEqual(got, sentences(1, "你好世")) {
		t.Fatalf("Feed = %+v, want [你好世]", got)
	}
	if got := a.Flush(); !reflect.DeepEqual(got, sentences(1, "界")) {
		t.Fatalf("Flush = %+v, want [界]", got)
	}
}

func TestSameSeqRefinalOverwritesNotAppends(t *testing.T) {
	a := domain.NewSentenceAssembler()
	if got := a.Feed(1, "abc"); len(got) != 0 {
		t.Fatalf("first Feed = %+v, want empty", got)
	}
	// 同 seq 二次 final：覆盖而非叠加（provider 重发权威全文）。
	if got := a.Feed(1, "abcdef"); len(got) != 0 {
		t.Fatalf("refinal Feed = %+v, want empty", got)
	}
	if got := a.Flush(); !reflect.DeepEqual(got, sentences(1, "abcdef")) {
		t.Fatalf("Flush = %+v, want [abcdef]（不得是 abcabcdef）", got)
	}
}

func TestTurnChangeFlushesResidueFirst(t *testing.T) {
	a := domain.NewSentenceAssembler()
	if got := a.Feed(1, "abc"); len(got) != 0 {
		t.Fatalf("Feed(turn1) = %+v, want empty", got)
	}
	// 新轮到达时上一轮残余按完整句收掉（防御 provider 异常时序）。
	got := a.Feed(2, "def")
	if !reflect.DeepEqual(got, sentences(1, "abc")) {
		t.Fatalf("Feed(turn2) = %+v, want residue [abc] seq1", got)
	}
	if got := a.Flush(); !reflect.DeepEqual(got, sentences(2, "def")) {
		t.Fatalf("Flush = %+v, want [def] seq2", got)
	}
}

func TestFlushBlankResidueDropped(t *testing.T) {
	a := domain.NewSentenceAssembler()
	if got := a.Feed(1, "   "); len(got) != 0 {
		t.Fatalf("Feed = %+v, want empty", got)
	}
	if got := a.Flush(); len(got) != 0 {
		t.Fatalf("Flush blank = %+v, want empty", got)
	}
}

func TestLimitZeroFallsBackToDefault(t *testing.T) {
	a := domain.NewSentenceAssemblerWithLimit(0)
	long := strings.Repeat("x", domain.DefaultMaxSentenceRunes+3)
	if got := a.Feed(1, long); len(got) != 1 || len([]rune(got[0].Text)) != domain.DefaultMaxSentenceRunes {
		t.Fatalf("Feed = %+v, want single cut at default limit %d", got, domain.DefaultMaxSentenceRunes)
	}
}
