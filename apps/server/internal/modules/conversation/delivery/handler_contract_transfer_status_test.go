package delivery

// TransferHTTPStatus 状态映射契约（模块边界：handlers 经 delivery 消费
// application sentinel，映射收口在 delivery）。

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
)

func TestTransferHTTPStatusMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"conversation missing", ErrConversationNotFound, http.StatusNotFound},
		{"target missing", ErrTransferTargetNotFound, http.StatusNotFound},
		{"out of scope", ErrTransferTargetOutOfScope, http.StatusForbidden},
		{"target unavailable", ErrTransferTargetUnavailable, http.StatusConflict},
		{"concurrent claim", ErrConcurrentTransfer, http.StatusConflict},
		{"wrapped sentinel", fmt.Errorf("wrap: %w", conversationapp.ErrTransferTargetOutOfScope), http.StatusForbidden},
		{"unknown error", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		if got := TransferHTTPStatus(tc.err); got != tc.want {
			t.Fatalf("%s: TransferHTTPStatus() = %d, want %d", tc.name, got, tc.want)
		}
	}
}
