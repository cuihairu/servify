package application

import "servify/apps/server/internal/modules/ticket/domain"

type ListTicketsQuery struct {
	Page               int
	PageSize           int
	Status             []string
	Priority           []string
	Category           []string
	Source             []string
	Tag                string
	AgentID            *uint
	CustomerID         *uint
	SessionID          *string
	Search             string
	SortBy             string
	SortOrder          string
	CustomFieldFilters map[string]string
}

type ListTicketsResult struct {
	Items []domain.Ticket
	Total int64
}
