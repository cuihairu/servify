package channel

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

func sampleInstallation(tenantID, workspaceID, installationID string) ChatConnectorInstallation {
	return ChatConnectorInstallation{
		TenantID:               tenantID,
		WorkspaceID:            workspaceID,
		Platform:               "memex",
		InstallationID:         installationID,
		ConversationPlatformID: "group:5",
		ConversationKind:       "group",
		ConversationIsGroup:    true,
		UpdatesEndpoint:        "https://memex.example/bot",
		TokenEnvVar:            "MEMEX_BOT_TOKEN",
		LongPollMS:             25000,
		Enabled:                true,
	}
}

func TestInstallationSaveCreatesThenUpdates(t *testing.T) {
	store := NewInstallationStore(newChannelTestDB(t))
	first := sampleInstallation("tenant-1", "workspace-1", "inst-1")
	saved, err := store.Save(context.Background(), first)
	if err != nil {
		t.Fatalf("save created: %v", err)
	}
	if saved.ID == 0 {
		t.Error("save must return the created row id")
	}

	// a second save for the same scope updates in place: one row, and the
	// zero-valued fields must be written through (Enabled/IsGroup false)
	second := first
	second.Enabled = false
	second.ConversationIsGroup = false
	second.UpdatesEndpoint = "https://memex-2.example/bot"
	second.LongPollMS = 1000
	if _, err := store.Save(context.Background(), second); err != nil {
		t.Fatalf("save updated: %v", err)
	}

	rows, err := store.List(context.Background(), "tenant-1", "workspace-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (update in place)", len(rows))
	}
	got := rows[0]
	if got.Enabled || got.ConversationIsGroup {
		t.Errorf("zero values not written through: enabled=%v is_group=%v", got.Enabled, got.ConversationIsGroup)
	}
	if got.UpdatesEndpoint != "https://memex-2.example/bot" || got.LongPollMS != 1000 {
		t.Errorf("update not applied: %+v", got)
	}
}

func TestInstallationSaveRejectsInvalidConfig(t *testing.T) {
	valid := sampleInstallation("tenant-1", "workspace-1", "inst-1")
	for _, tc := range []struct {
		name   string
		mutate func(*ChatConnectorInstallation)
	}{
		{"empty tenant", func(i *ChatConnectorInstallation) { i.TenantID = "" }},
		{"empty workspace", func(i *ChatConnectorInstallation) { i.WorkspaceID = "" }},
		{"empty platform", func(i *ChatConnectorInstallation) { i.Platform = "" }},
		{"empty installation", func(i *ChatConnectorInstallation) { i.InstallationID = "" }},
		{"empty conversation", func(i *ChatConnectorInstallation) { i.ConversationPlatformID = "" }},
		{"empty kind", func(i *ChatConnectorInstallation) { i.ConversationKind = "" }},
		{"empty endpoint", func(i *ChatConnectorInstallation) { i.UpdatesEndpoint = "" }},
		{"empty token env var", func(i *ChatConnectorInstallation) { i.TokenEnvVar = "" }},
		{"zero long poll", func(i *ChatConnectorInstallation) { i.LongPollMS = 0 }},
		{"over cap long poll", func(i *ChatConnectorInstallation) { i.LongPollMS = memexLongPollCapMS + 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inst := valid
			tc.mutate(&inst)
			store := NewInstallationStore(newChannelTestDB(t))
			if _, err := store.Save(context.Background(), inst); err == nil {
				t.Fatalf("save accepted invalid config")
			}
			if got := installationCount(t, store, "tenant-1", "workspace-1"); got != 0 {
				t.Errorf("rows = %d, want 0 (invalid config never stored)", got)
			}
		})
	}
}

func TestInstallationListScopesTenantAndWorkspace(t *testing.T) {
	store := NewInstallationStore(newChannelTestDB(t))
	for _, inst := range []ChatConnectorInstallation{
		sampleInstallation("tenant-1", "workspace-1", "inst-a"),
		sampleInstallation("tenant-1", "workspace-1", "inst-b"),
		sampleInstallation("tenant-1", "workspace-2", "inst-c"),
		sampleInstallation("tenant-2", "workspace-1", "inst-d"),
	} {
		if _, err := store.Save(context.Background(), inst); err != nil {
			t.Fatalf("save %s: %v", inst.InstallationID, err)
		}
	}

	rows, err := store.List(context.Background(), "tenant-1", "workspace-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (tenant+workspace scoped)", len(rows))
	}
	// ordering is stable: platform then installation id
	if rows[0].InstallationID != "inst-a" || rows[1].InstallationID != "inst-b" {
		t.Errorf("rows = %v %v, want inst-a inst-b", rows[0].InstallationID, rows[1].InstallationID)
	}

	if empty, err := store.List(context.Background(), "tenant-3", "workspace-3"); err != nil || len(empty) != 0 {
		t.Errorf("empty scope = %v (%v), want no rows", empty, err)
	}
}

func TestInstallationGet(t *testing.T) {
	store := NewInstallationStore(newChannelTestDB(t))
	inst := sampleInstallation("tenant-1", "workspace-1", "inst-1")
	if _, err := store.Save(context.Background(), inst); err != nil {
		t.Fatalf("save: %v", err)
	}

	scope := InstallationScope{"tenant-1", "workspace-1", "memex", "inst-1"}
	got, err := store.Get(context.Background(), scope)
	if err != nil || got == nil {
		t.Fatalf("get = %v (%v)", got, err)
	}
	if got.TokenEnvVar != "MEMEX_BOT_TOKEN" || !got.Enabled {
		t.Errorf("get = %+v", got)
	}

	missing, err := store.Get(context.Background(), InstallationScope{"tenant-1", "workspace-1", "memex", "absent"})
	if err != nil || missing != nil {
		t.Errorf("missing install = %v (%v), want nil nil", missing, err)
	}
}

func TestInstallationListEnabled(t *testing.T) {
	store := NewInstallationStore(newChannelTestDB(t))
	on1 := sampleInstallation("tenant-1", "workspace-1", "on-1")
	on2 := sampleInstallation("tenant-2", "workspace-2", "on-2")
	off := sampleInstallation("tenant-1", "workspace-1", "off")
	off.Enabled = false
	for _, inst := range []ChatConnectorInstallation{on1, on2, off} {
		if _, err := store.Save(context.Background(), inst); err != nil {
			t.Fatalf("save %s: %v", inst.InstallationID, err)
		}
	}

	rows, err := store.ListEnabled(context.Background())
	if err != nil {
		t.Fatalf("list enabled: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (disabled row excluded)", len(rows))
	}
	for _, row := range rows {
		if !row.Enabled {
			t.Errorf("disabled row leaked into ListEnabled: %+v", row)
		}
	}
	if rows[0].InstallationID != "on-1" || rows[1].InstallationID != "on-2" {
		t.Errorf("rows = %s %s, want on-1 on-2 (stable order)", rows[0].InstallationID, rows[1].InstallationID)
	}

	// a store with no rows returns an empty slice, not an error
	empty, err := NewInstallationStore(newChannelTestDB(t)).ListEnabled(context.Background())
	if err != nil || len(empty) != 0 {
		t.Errorf("empty store list = %v (%v), want no rows", empty, err)
	}
}

func TestInstallationStoreErrors(t *testing.T) {
	db := newChannelTestDB(t)
	// read path: Save and Get both fail through the capabilities-shaped query
	if err := db.Callback().Query().Before("gorm:query").Register("inst_query_error",
		func(tx *gorm.DB) { tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	store := NewInstallationStore(db)
	if _, err := store.Save(context.Background(), sampleInstallation("t", "w", "i")); err == nil {
		t.Error("save: expected query error")
	}
	if _, err := store.Get(context.Background(), InstallationScope{"t", "w", "memex", "i"}); err == nil {
		t.Error("get: expected query error")
	}
	if _, err := store.List(context.Background(), "t", "w"); err == nil {
		t.Error("list: expected query error")
	}
	if _, err := store.ListEnabled(context.Background()); err == nil {
		t.Error("list enabled: expected query error")
	}
}

func TestInstallationSaveCreateAndUpdateErrors(t *testing.T) {
	// create path
	db := newChannelTestDB(t)
	if err := db.Callback().Create().Before("gorm:create").Register("inst_create_error",
		func(tx *gorm.DB) { tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register create callback: %v", err)
	}
	if _, err := NewInstallationStore(db).Save(context.Background(),
		sampleInstallation("t", "w", "i")); err == nil {
		t.Error("save create: expected error")
	}

	// update path
	db2 := newChannelTestDB(t)
	store := NewInstallationStore(db2)
	if _, err := store.Save(context.Background(), sampleInstallation("t", "w", "i")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db2.Callback().Update().Before("gorm:update").Register("inst_update_error",
		func(tx *gorm.DB) { tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register update callback: %v", err)
	}
	if _, err := store.Save(context.Background(), sampleInstallation("t", "w", "i")); err == nil {
		t.Error("save update: expected error")
	}
}

func installationCount(t *testing.T, store *InstallationStore, tenantID, workspaceID string) int {
	t.Helper()
	rows, err := store.List(context.Background(), tenantID, workspaceID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return len(rows)
}
