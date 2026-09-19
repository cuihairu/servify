package services

import (
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

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
