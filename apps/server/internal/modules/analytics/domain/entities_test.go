package domain

import "testing"

func TestReadModelsCarryFields(t *testing.T) {
	dashboard := DashboardReadModel{
		TotalCustomers:              10,
		TotalAgents:                 2,
		TotalTickets:                30,
		TotalSessions:               40,
		TodayTickets:                5,
		TodaySessions:               6,
		TodayMessages:               60,
		OpenTickets:                 7,
		AssignedTickets:             8,
		ResolvedTickets:             9,
		ClosedTickets:               10,
		OnlineAgents:                1,
		BusyAgents:                  1,
		ActiveSessions:              3,
		AvgResponseTime:             12.5,
		AvgResolutionTime:           120.5,
		CustomerSatisfaction:        4.5,
		AIUsageToday:                11,
		KnowledgeProviderUsageToday: 12,
		WeKnoraUsageToday:           13,
	}
	if dashboard.TotalCustomers != 10 || dashboard.WeKnoraUsageToday != 13 {
		t.Fatalf("unexpected dashboard read model: %+v", dashboard)
	}

	trend := TicketTrendReadModel{
		Date:                 "2026-04-08",
		Tickets:              3,
		Sessions:             4,
		Messages:             5,
		ResolvedTickets:      2,
		AvgResponseTime:      30,
		CustomerSatisfaction: 4.2,
	}
	if trend.Date != "2026-04-08" || trend.CustomerSatisfaction != 4.2 {
		t.Fatalf("unexpected ticket trend read model: %+v", trend)
	}

	perf := AgentPerformanceReadModel{
		AgentID:           7,
		AgentName:         "Agent A",
		Department:        "support",
		TotalTickets:      20,
		ResolvedTickets:   15,
		AvgResponseTime:   9.5,
		AvgResolutionTime: 3600,
		Rating:            4.8,
		OnlineTime:        28800,
	}
	if perf.AgentID != 7 || perf.OnlineTime != 28800 {
		t.Fatalf("unexpected agent performance read model: %+v", perf)
	}

	satisfaction := SatisfactionTrendReadModel{Date: "2026-04-08", Score: 4.6}
	if satisfaction.Score != 4.6 {
		t.Fatalf("unexpected satisfaction trend read model: %+v", satisfaction)
	}

	sla := SLATrendReadModel{Date: "2026-04-08", Violations: 2}
	if sla.Violations != 2 {
		t.Fatalf("unexpected SLA trend read model: %+v", sla)
	}
}
