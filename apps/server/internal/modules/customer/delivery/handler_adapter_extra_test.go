package delivery

// V1.0 收敛 B2-2：HandlerServiceAdapter 数据边界装配面的分支行为——
// 未装配时导出/擦除显式不可用、nil 接收者链式安全、装配后透传真实服务。

import (
	"context"
	"strings"
	"testing"

	customerapp "servify/apps/server/internal/modules/customer/application"
	customerinfra "servify/apps/server/internal/modules/customer/infra"

	"github.com/stretchr/testify/require"
)

func TestHandlerServiceAdapterBoundaryNotConfigured(t *testing.T) {
	_ = newCustomerDeliveryTestDB(t)         // 包级测试库口径一致；本例不触库
	adapter := NewHandlerServiceAdapter(nil) // 不挂 boundary
	ctx := context.Background()

	_, err := adapter.ExportCustomerData(ctx, 1)
	require.Error(t, err)
	if !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("export err = %v", err)
	}
	_, err = adapter.EraseCustomerData(ctx, 1)
	require.Error(t, err)
	if !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("erase err = %v", err)
	}
}

func TestHandlerServiceAdapterWithBoundaryChains(t *testing.T) {
	var nilAdapter *HandlerServiceAdapter
	if nilAdapter.WithBoundary(nil) != nil {
		t.Fatal("nil receiver must chain nil")
	}

	db := newCustomerDeliveryTestDB(t)
	adapter := NewHandlerService(db).WithBoundary(
		customerapp.NewDataBoundaryService(customerinfra.NewGormDataBoundaryRepository(db)))
	ctx := context.Background()

	// 装配成功后透传真实边界服务（customer_id=0 由服务层拒收，证明已穿透）。
	if _, err := adapter.ExportCustomerData(ctx, 0); err == nil {
		t.Fatal("export must reach boundary service (zero id rejected)")
	}
	if _, err := adapter.EraseCustomerData(ctx, 0); err == nil {
		t.Fatal("erase must reach boundary service (zero id rejected)")
	}
}
