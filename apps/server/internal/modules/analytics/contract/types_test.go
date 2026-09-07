package contract

import (
	"encoding/json"
	"testing"
)

func TestDashboardStatsJSONTags(t *testing.T) {
	stats := DashboardStats{
		TotalCustomers:              1,
		TotalAgents:                 2,
		TotalTickets:                3,
		TotalSessions:               4,
		TodayTickets:                5,
		TodaySessions:               6,
		TodayMessages:               7,
		OpenTickets:                 8,
		AssignedTickets:             9,
		ResolvedTickets:             10,
		ClosedTickets:               11,
		OnlineAgents:                12,
		BusyAgents:                  13,
		ActiveSessions:              14,
		AvgResponseTime:             15.5,
		AvgResolutionTime:           16.5,
		CustomerSatisfaction:        4.5,
		AIUsageToday:                17,
		KnowledgeProviderUsageToday: 18,
		WeKnoraUsageToday:           19,
	}
	data, err := json.Marshal(&stats)
	if err != nil {
		t.Fatalf("marshal dashboard stats: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal dashboard stats: %v", err)
	}
	for _, key := range []string{
		"total_customers", "total_agents", "total_tickets", "total_sessions",
		"today_tickets", "today_sessions", "today_messages", "open_tickets",
		"assigned_tickets", "resolved_tickets", "closed_tickets", "online_agents",
		"busy_agents", "active_sessions", "avg_response_time", "avg_resolution_time",
		"customer_satisfaction", "ai_usage_today", "knowledge_provider_usage_today",
		"weknora_usage_today",
	} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("dashboard stats JSON missing key %q", key)
		}
	}
}

func TestStatsJSONRoundTrip(t *testing.T) {
	timeRange := TimeRangeStats{Date: "2026-04-08", Tickets: 1, Sessions: 2, Messages: 3, ResolvedTickets: 4, AvgResponseTime: 5.5, CustomerSatisfaction: 4.5}
	data, err := json.Marshal(&timeRange)
	if err != nil {
		t.Fatalf("marshal time range stats: %v", err)
	}
	if string(data) != `{"date":"2026-04-08","tickets":1,"sessions":2,"messages":3,"resolved_tickets":4,"avg_response_time":5.5,"customer_satisfaction":4.5}` {
		t.Fatalf("unexpected time range JSON: %s", data)
	}

	agent := AgentPerformanceStats{AgentID: 7, AgentName: "A", Department: "support", TotalTickets: 10, ResolvedTickets: 8, AvgResponseTime: 9.5, AvgResolutionTime: 3600, Rating: 4.8, OnlineTime: 100}
	data, err = json.Marshal(&agent)
	if err != nil {
		t.Fatalf("marshal agent performance stats: %v", err)
	}
	if string(data) != `{"agent_id":7,"agent_name":"A","department":"support","total_tickets":10,"resolved_tickets":8,"avg_response_time":9.5,"avg_resolution_time":3600,"rating":4.8,"online_time":100}` {
		t.Fatalf("unexpected agent performance JSON: %s", data)
	}

	category := CategoryStats{Category: "billing", Count: 3}
	data, err = json.Marshal(&category)
	if err != nil {
		t.Fatalf("marshal category stats: %v", err)
	}
	if string(data) != `{"category":"billing","count":3}` {
		t.Fatalf("unexpected category JSON: %s", data)
	}

	remote := RemoteAssistTicketStats{Total: 10, Open: 4, Resolved: 5, Closed: 1, ResolvedRate: 0.5, ClosedRate: 0.1, AvgCloseHours: 2.5}
	data, err = json.Marshal(&remote)
	if err != nil {
		t.Fatalf("marshal remote assist stats: %v", err)
	}
	if string(data) != `{"total":10,"open":4,"resolved":5,"closed":1,"resolved_rate":0.5,"closed_rate":0.1,"avg_close_hours":2.5}` {
		t.Fatalf("unexpected remote assist JSON: %s", data)
	}
}
