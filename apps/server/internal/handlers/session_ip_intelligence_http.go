package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HTTPSessionIPIntelligence struct {
	baseURL    string
	apiKey     string
	authHeader string
	client     *http.Client
}

type sessionIPHTTPResponse struct {
	NetworkLabel  string                 `json:"network_label"`
	LocationLabel string                 `json:"location_label"`
	Data          *sessionIPHTTPResponse `json:"data"`
}

func NewHTTPSessionIPIntelligence(baseURL, apiKey, authHeader string, timeout time.Duration) *HTTPSessionIPIntelligence {
	baseURL = strings.TrimSpace(baseURL)
	if timeout <= 0 {
		timeout = 1500 * time.Millisecond
	}
	authHeader = strings.TrimSpace(authHeader)
	if authHeader == "" {
		authHeader = "Authorization"
	}
	return &HTTPSessionIPIntelligence{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     strings.TrimSpace(apiKey),
		authHeader: authHeader,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (p *HTTPSessionIPIntelligence) DescribeIP(ip string) sessionIPDescription {
	if p == nil || p.baseURL == "" || strings.TrimSpace(ip) == "" {
		return sessionIPDescription{}
	}

	req, err := http.NewRequest(http.MethodGet, p.lookupURL(ip), nil)
	if err != nil {
		return sessionIPDescription{}
	}
	if p.apiKey != "" {
		if strings.EqualFold(p.authHeader, "Authorization") {
			req.Header.Set(p.authHeader, "Bearer "+p.apiKey)
		} else {
			req.Header.Set(p.authHeader, p.apiKey)
		}
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return sessionIPDescription{}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return sessionIPDescription{}
	}

	var payload sessionIPHTTPResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return sessionIPDescription{}
	}
	if payload.Data != nil {
		if payload.NetworkLabel == "" {
			payload.NetworkLabel = payload.Data.NetworkLabel
		}
		if payload.LocationLabel == "" {
			payload.LocationLabel = payload.Data.LocationLabel
		}
	}
	return sessionIPDescription{
		NetworkLabel:  payload.NetworkLabel,
		LocationLabel: payload.LocationLabel,
	}
}

// LoginNetworkLabel 使 HTTPSessionIPIntelligence 满足 authapp.LoginRiskIntel
// （P2-5 第二刀登录风险执行，契约现居 modules/auth/application）。ctx 未下沉到 DescribeIP：查询超时由内部
// client 自带 timeout 兜底。
func (p *HTTPSessionIPIntelligence) LoginNetworkLabel(ctx context.Context, ip string) string {
	if p == nil {
		return ""
	}
	return p.DescribeIP(ip).NetworkLabel
}

func (p *HTTPSessionIPIntelligence) lookupURL(ip string) string {
	if strings.Contains(p.baseURL, "{ip}") {
		return strings.ReplaceAll(p.baseURL, "{ip}", url.PathEscape(strings.TrimSpace(ip)))
	}
	return p.baseURL + "/" + url.PathEscape(strings.TrimSpace(ip))
}
