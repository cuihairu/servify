package delivery

import (
	"context"
	"testing"

	"servify/apps/server/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSessionTransferAdapterListRoutingAssignmentsPassthrough 覆盖评分审计
// 读口直通 application 层的路径（B2-1 管理面展示分配理由）。
func TestSessionTransferAdapterListRoutingAssignmentsPassthrough(t *testing.T) {
	db := newRoutingDeliveryTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.RoutingAssignment{}))
	adapter := newRoutingDeliveryAdapter(db)

	items, err := adapter.ListRoutingAssignments(context.Background(), "sess-none", 10)
	require.NoError(t, err)
	assert.Empty(t, items)
}
