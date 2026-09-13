package contract

import "testing"

// 覆盖 MapTicketStats 对 nil 入参的短路返回。
func TestMapTicketStatsNil(t *testing.T) {
	if got := MapTicketStats(nil); got != nil {
		t.Fatalf("expected nil for nil stats, got %+v", got)
	}
}
