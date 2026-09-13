package infra

import (
	"errors"
	"io"
	"strings"
	"testing"
)

var errQPWriter = errors.New("qp writer closed")

// qpFailWriter 前 failAfter 次底层 Write 成功，之后一律失败。
// quoted-printable 编码在行结束（\r\n）或 76 列时 flush 底层，
// 剩余内容在 Close 时 flush——据此分别触发 Write / Close 两条错误分支。
type qpFailWriter struct {
	failAfter int
	calls     int
}

func (w *qpFailWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls > w.failAfter {
		return 0, errQPWriter
	}
	return len(p), nil
}

func TestComposeQuotedPrintableWriteError(t *testing.T) {
	// 首次底层写即失败：带换行的正文会在 qp.Write 阶段 flush 底层。
	err := composeQuotedPrintable(&qpFailWriter{failAfter: 0}, "第一行\r\n第二行")
	if !errors.Is(err, errQPWriter) {
		t.Fatalf("expected quoted-printable write error, got %v", err)
	}
}

func TestComposeQuotedPrintableCloseError(t *testing.T) {
	// 无换行的短正文在 qp.Write 阶段不触达底层（<76 列缓冲），
	// Close 阶段 flush 才失败。
	err := composeQuotedPrintable(&qpFailWriter{failAfter: 0}, "short body without newline")
	if !errors.Is(err, errQPWriter) {
		t.Fatalf("expected quoted-printable close error, got %v", err)
	}
}

func TestComposeQuotedPrintableDefaultPathEncodesBody(t *testing.T) {
	// seam 默认路径：真实 quoted-printable 写入 *bytes.Buffer。
	var sb strings.Builder
	if err := composeQuotedPrintable(&sb, "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sb.String() != "hello" {
		t.Fatalf("expected plain ascii passthrough, got %q", sb.String())
	}
}

func TestComposeTextMessageBodyComposeFailure(t *testing.T) {
	orig := composeQuotedPrintable
	composeQuotedPrintable = func(w io.Writer, text string) error { return errQPWriter }
	defer func() { composeQuotedPrintable = orig }()

	msg, err := ComposeTextMessage("from@example.com", "to@example.com", "subj", "body", "")
	if !errors.Is(err, errQPWriter) {
		t.Fatalf("expected compose body error, got %v", err)
	}
	if msg != nil {
		t.Fatalf("expected nil message on failure, got %d bytes", len(msg))
	}
}
