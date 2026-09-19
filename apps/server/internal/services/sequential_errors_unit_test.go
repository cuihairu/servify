package services

import (
	"testing"
	"time"

	"servify/apps/server/internal/models"
)

// failNthQuery registers gorm callbacks that force the n-th SELECT (including
// raw Scan/Row reads) on this connection to fail. Used to exercise sequential
// error branches unreachable with plain table drops.
func TestRouter_EnsureSessionNoopUpdate(t *testing.T) {
	db := newServicesTestDB(t, &models.Session{}, &models.Message{})
	if err := db.Create(&models.Session{ID: "blank", Status: "active", StartedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error; err != nil {
		t.Fatalf("seed blank session: %v", err)
	}
	// RAISE(IGNORE) makes the scope-upgrade update a no-op (RowsAffected == 0)
	execTrigger(t, db, "CREATE TRIGGER ign_sess BEFORE UPDATE ON sessions BEGIN SELECT RAISE(IGNORE); END;")
	r := NewMessageRouter(stubAI{reply: "ok"}, NewWebSocketHub(), db)
	if _, _, err := r.ensureSession("blank", "web", "t1", "w1"); err == nil {
		t.Fatal("expected RowsAffected==0 error")
	}
}

func TestWebSocket_AsICECandidateDecodeError(t *testing.T) {
	if _, err := asICECandidate(map[string]interface{}{"candidate": 123}); err == nil {
		t.Fatal("expected decode error for numeric candidate")
	}
}
