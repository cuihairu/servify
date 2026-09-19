package delivery

import (
	"time"
)

type AgentCreateRequest struct {
	UserID        uint   `json:"user_id" binding:"required"`
	Department    string `json:"department"`
	Skills        string `json:"skills"`
	MaxConcurrent int    `json:"max_concurrent"`
}

type AgentInfo struct {
	UserID          uint      `json:"user_id"`
	Username        string    `json:"username"`
	Name            string    `json:"name"`
	Department      string    `json:"department"`
	Skills          []string  `json:"skills"`
	Status          string    `json:"status"`
	MaxConcurrent   int       `json:"max_concurrent"`
	CurrentLoad     int       `json:"current_load"`
	Rating          float64   `json:"rating"`
	AvgResponseTime int       `json:"avg_response_time"`
	LastActivity    time.Time `json:"last_activity"`
	ConnectedAt     time.Time `json:"connected_at"`
}
