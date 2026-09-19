package services

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"

	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// unitScopedContext builds a scoped context for unit tests (kept distinct from
// the integration-only scopedContext helper).
func unitScopedContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

// newServicesTestDB opens an isolated in-memory sqlite database per test.
func newServicesTestDB(t *testing.T, models ...interface{}) *gorm.DB {
	t.Helper()
	dsn := "file:svc_unit_" + t.Name() + "_" + strconv.Itoa(int(atomic.AddUint32(&testDBSeq, 1))) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

var testDBSeq uint32

func TestTestutilPointers(t *testing.T) {
	s := "v"
	if got := stringPtr(s); *got != "v" {
		t.Fatalf("stringPtr = %v", got)
	}
	u := uint(7)
	if got := uintPtr(u); *got != 7 {
		t.Fatalf("uintPtr = %v", got)
	}
	b := true
	if got := boolPtr(b); !*got {
		t.Fatalf("boolPtr = %v", got)
	}
	i := 3
	if got := intPtr(i); *got != 3 {
		t.Fatalf("intPtr = %v", got)
	}
}
