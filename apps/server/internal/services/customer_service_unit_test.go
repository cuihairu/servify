package services

import (
	"context"
	"testing"

	"servify/apps/server/internal/models"
	customerapp "servify/apps/server/internal/modules/customer/application"

	"github.com/sirupsen/logrus"
)

func newCustomerTestService(t *testing.T) (*CustomerService, context.Context) {
	t.Helper()
	db := newServicesTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Session{}, &models.Ticket{}, &models.Message{},
	)
	svc := NewCustomerService(db, logrus.New())
	return svc, unitScopedContext("t1", "w1")
}

func TestCustomerService_CreateGetUpdate(t *testing.T) {
	svc, ctx := newCustomerTestService(t)

	created, err := svc.CreateCustomer(ctx, &CustomerCreateRequest{
		Username: "c1",
		Email:    "c1@x.com",
		Tags:     " a , b ",
		Priority: "high",
	})
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}

	got, err := svc.GetCustomerByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetCustomerByID: %v", err)
	}
	if got.Username != "c1" {
		t.Fatalf("unexpected user: %+v", got)
	}
	if _, err := svc.GetCustomerByID(ctx, 999); err == nil {
		t.Fatal("expected error for missing customer")
	}

	updated, err := svc.UpdateCustomer(ctx, created.ID, &CustomerUpdateRequest{
		Name:     stringPtr("N"),
		Company:  stringPtr("Co"),
		Tags:     stringPtr("x"),
		Priority: stringPtr("low"),
		Status:   stringPtr("inactive"),
	})
	if err != nil {
		t.Fatalf("UpdateCustomer: %v", err)
	}
	if updated.Name != "N" {
		t.Fatalf("unexpected update: %+v", updated)
	}
	if _, err := svc.UpdateCustomer(ctx, 999, &CustomerUpdateRequest{}); err == nil {
		t.Fatal("expected error updating missing customer")
	}
}

func TestCustomerService_ListAndStats(t *testing.T) {
	svc, ctx := newCustomerTestService(t)

	for _, name := range []string{"a", "b"} {
		if _, err := svc.CreateCustomer(ctx, &CustomerCreateRequest{
			Username: name, Email: name + "@x.com", Source: "web", Industry: "tech",
		}); err != nil {
			t.Fatalf("CreateCustomer %s: %v", name, err)
		}
	}

	items, total, err := svc.ListCustomers(ctx, &CustomerListRequest{
		Page: 1, PageSize: 10, Industry: []string{"tech"},
		Source: []string{"web"}, Priority: []string{"normal"}, Status: []string{"active"},
		SortBy: "created_at", SortOrder: "desc",
	})
	if err != nil {
		t.Fatalf("ListCustomers: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("unexpected list: %d %+v", total, items)
	}

	stats, err := svc.GetCustomerStats(ctx)
	if err != nil {
		t.Fatalf("GetCustomerStats: %v", err)
	}
	if stats.Total != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestCustomerService_ActivityAndNotes(t *testing.T) {
	svc, ctx := newCustomerTestService(t)

	created, err := svc.CreateCustomer(ctx, &CustomerCreateRequest{Username: "a", Email: "a@x.com"})
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}

	activity, err := svc.GetCustomerActivity(ctx, created.ID, 5)
	if err != nil {
		t.Fatalf("GetCustomerActivity: %v", err)
	}
	if activity == nil || activity.CustomerID != created.ID {
		t.Fatalf("unexpected activity: %+v", activity)
	}
	missing, err := svc.GetCustomerActivity(ctx, 999, 5)
	if err != nil {
		t.Fatalf("GetCustomerActivity missing: %v", err)
	}
	if missing.CustomerID != 999 || len(missing.RecentSessions) != 0 {
		t.Fatalf("unexpected missing activity: %+v", missing)
	}

	if err := svc.AddCustomerNote(ctx, created.ID, "note", 1); err != nil {
		t.Fatalf("AddCustomerNote: %v", err)
	}
	if err := svc.UpdateCustomerTags(ctx, created.ID, []string{"t1"}); err != nil {
		t.Fatalf("UpdateCustomerTags: %v", err)
	}
}

func TestCustomerService_RevokeTokens(t *testing.T) {
	svc, ctx := newCustomerTestService(t)

	created, err := svc.CreateCustomer(ctx, &CustomerCreateRequest{Username: "a", Email: "a@x.com"})
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}
	if _, err := svc.RevokeCustomerTokens(ctx, 999); err == nil {
		t.Fatal("expected error revoking missing customer")
	}
	if _, err := svc.RevokeCustomerTokens(ctx, created.ID); err != nil {
		t.Fatalf("RevokeCustomerTokens: %v", err)
	}
}

func TestCustomerMappersAndSplitTags(t *testing.T) {
	if customerActivityFromDTO(nil) != nil {
		t.Fatal("nil activity DTO should map to nil")
	}
	if customerStatsFromDTO(nil) != nil {
		t.Fatal("nil stats DTO should map to nil")
	}
	activity := customerActivityFromDTO(&customerapp.CustomerActivityDTO{
		CustomerID:     3,
		RecentSessions: []models.Session{{ID: "s"}},
		RecentTickets:  []models.Ticket{{ID: 1}},
		RecentMessages: []models.Message{{ID: 2}},
	})
	if activity.CustomerID != 3 || len(activity.RecentSessions) != 1 {
		t.Fatalf("unexpected activity: %+v", activity)
	}
	stats := customerStatsFromDTO(&customerapp.CustomerStatsDTO{
		Total:       10,
		Active:      5,
		NewThisWeek: 2,
		BySource:    []customerapp.SourceCount{{Source: "web", Count: 4}},
		ByIndustry:  []customerapp.IndustryCount{{Industry: "tech", Count: 3}},
		ByPriority:  []customerapp.PriorityCount{{Priority: "high", Count: 1}},
	})
	if stats.Total != 10 || len(stats.BySource) != 1 || len(stats.ByIndustry) != 1 || len(stats.ByPriority) != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	if splitTags("") != nil {
		t.Fatal("empty tags should be nil")
	}
	got := splitTags(" a ,, b ")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("unexpected split: %+v", got)
	}
}
