package services

import (
	"strings"
	"testing"

	"servify/apps/server/internal/models"
)

func TestAppIntegrationService_Lifecycle(t *testing.T) {
	db := newServicesTestDB(t, &models.AppIntegration{})
	svc := NewAppIntegrationService(db, nil)
	ctx := unitScopedContext("t1", "w1")

	created, err := svc.Create(ctx, &AppIntegrationCreateRequest{
		Name:      "Zapier",
		Slug:      " ZAPIER Tools!! ",
		Vendor:    "Zapier Inc",
		IFrameURL: "https://zapier.example.com",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Slug != "zapier-tools" {
		t.Fatalf("slug = %q", created.Slug)
	}
	if !created.Enabled || created.LastSyncStatus != "never" {
		t.Fatalf("unexpected defaults: %+v", created)
	}

	if _, err := svc.Create(ctx, nil); err == nil {
		t.Fatal("expected error for nil create request")
	}
	if _, err := svc.Create(ctx, &AppIntegrationCreateRequest{Name: "!!!"}); err == nil {
		t.Fatal("expected error when slug cannot be derived")
	}
	if _, err := svc.Create(ctx, &AppIntegrationCreateRequest{Name: "Dup", Slug: "zapier-tools", IFrameURL: "u"}); err == nil {
		t.Fatal("expected duplicate slug error")
	}

	// second record for list coverage
	if _, err := svc.Create(ctx, &AppIntegrationCreateRequest{
		Name: "Slack", Slug: "slack", IFrameURL: "u2",
		Enabled:      boolPtr(false),
		Capabilities: []string{"chat"},
		ConfigSchema: map[string]interface{}{"k": "v"},
	}); err != nil {
		t.Fatalf("Create slack: %v", err)
	}

	items, total, err := svc.List(ctx, &AppIntegrationListRequest{Page: 0, PageSize: 0})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("expected 2 items, got %d/%d", total, len(items))
	}

	enabledItems, _, err := svc.List(ctx, &AppIntegrationListRequest{Status: []string{"enabled"}})
	if err != nil || len(enabledItems) != 2 {
		t.Fatalf("enabled filter: %v %d", err, len(enabledItems))
	}
	disabledItems, _, err := svc.List(ctx, &AppIntegrationListRequest{Status: []string{"disabled"}})
	if err != nil || len(disabledItems) != 0 {
		t.Fatalf("disabled filter: %v %d", err, len(disabledItems))
	}
	svc.List(ctx, &AppIntegrationListRequest{Status: []string{"weird"}})

	// search spans name/vendor/summary via LOWER LIKE, consistent on sqlite and pg
	zapItems, zapTotal, err := svc.List(ctx, &AppIntegrationListRequest{Search: "zap"})
	if err != nil {
		t.Fatalf("List search: %v", err)
	}
	if zapTotal != 1 || len(zapItems) != 1 || zapItems[0].Slug != "zapier-tools" {
		t.Fatalf("expected 1 zapier match, got %d/%d", zapTotal, len(zapItems))
	}

	// forced first-SELECT failure covers the List count error branch
	errDB := newServicesTestDB(t, &models.AppIntegration{})
	failNthQuery(errDB, 1)
	if _, _, err := NewAppIntegrationService(errDB, nil).List(ctx, &AppIntegrationListRequest{Page: 1, PageSize: 20}); err == nil || !strings.Contains(err.Error(), "failed to count integrations") {
		t.Fatalf("expected count integrations error, got %v", err)
	}

	updated, err := svc.Update(ctx, created.ID, &AppIntegrationUpdateRequest{
		Name:         stringPtr("Zapier 2"),
		Vendor:       stringPtr("V"),
		Category:     stringPtr("automation"),
		Summary:      stringPtr("sum"),
		IconURL:      stringPtr("icon"),
		Capabilities: []string{"a", "b"},
		ConfigSchema: map[string]interface{}{"x": 1},
		IFrameURL:    stringPtr("u3"),
		Enabled:      boolPtr(true),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "Zapier 2" || len(updated.Capabilities) != 2 || updated.ConfigSchema == nil {
		t.Fatalf("unexpected update: %+v", updated)
	}

	if _, err := svc.Update(ctx, created.ID, nil); err == nil {
		t.Fatal("expected error for nil update request")
	}
	if _, err := svc.Update(ctx, 9999, &AppIntegrationUpdateRequest{}); err == nil {
		t.Fatal("expected error for missing integration")
	}

	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := svc.Delete(ctx, created.ID); err == nil {
		t.Fatal("expected error deleting missing integration")
	}
}

func TestAppIntegrationService_CrossScope(t *testing.T) {
	db := newServicesTestDB(t, &models.AppIntegration{})
	svc := NewAppIntegrationService(db, nil)
	ctxA := unitScopedContext("t1", "w1")
	ctxB := unitScopedContext("t1", "w2")

	created, err := svc.Create(ctxA, &AppIntegrationCreateRequest{Name: "A", Slug: "a", IFrameURL: "u"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Update(ctxB, created.ID, &AppIntegrationUpdateRequest{}); err == nil {
		t.Fatal("expected cross-scope update to fail")
	}
	if err := svc.Delete(ctxB, created.ID); err == nil {
		t.Fatal("expected cross-scope delete to fail")
	}
}

func TestAppIntegrationHelpers(t *testing.T) {
	if encodeJSON(nil) != "" {
		t.Fatal("nil encode should be empty")
	}
	if encodeJSON(map[string]int{"a": 1}) != `{"a":1}` {
		t.Fatal("unexpected encode result")
	}
	if v := decodeStringArray(""); v != nil {
		t.Fatal("empty decode should be nil")
	}
	if v := decodeStringArray("not-json"); v != nil {
		t.Fatal("bad json decode should be nil")
	}
	if v := decodeStringArray(`["a"]`); len(v) != 1 {
		t.Fatal("expected decoded slice")
	}
	if v := decodeObject(""); v != nil {
		t.Fatal("empty object decode should be nil")
	}
	if v := decodeObject("[1]"); v != nil {
		t.Fatal("bad object decode should be nil")
	}
	if v := decodeObject(`{"a":1}`); v["a"] != float64(1) {
		t.Fatal("expected decoded object")
	}
	if normalizeSlug("  MiXed Case--Slug!!  ") != "mixed-case--slug" {
		t.Fatal("unexpected slug normalization")
	}
	if normalizeSlug("") != "" {
		t.Fatal("empty slug")
	}
}
