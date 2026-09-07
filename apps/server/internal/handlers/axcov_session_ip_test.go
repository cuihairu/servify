package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAxcDescribeSessionIPFallbacks(t *testing.T) {
	t.Run("nil provider uses heuristics", func(t *testing.T) {
		desc := describeSessionIP(nil, "203.0.113.10")
		if desc.NetworkLabel != "public" || desc.LocationLabel != "documentation" {
			t.Fatalf("unexpected desc: %+v", desc)
		}
	})
	t.Run("labels are trimmed", func(t *testing.T) {
		provider := axcFixedIPIntel{desc: sessionIPDescription{NetworkLabel: "  public  ", LocationLabel: "  geo:cn  "}}
		got := describeSessionIP(provider, "8.8.8.8")
		if got.NetworkLabel != "public" || got.LocationLabel != "geo:cn" {
			t.Fatalf("unexpected desc: %+v", got)
		}
	})
	t.Run("empty labels fall back to heuristics", func(t *testing.T) {
		provider := axcFixedIPIntel{}
		got := describeSessionIP(provider, "10.2.3.4")
		if got.NetworkLabel != "private" || got.LocationLabel != "private" {
			t.Fatalf("unexpected desc: %+v", got)
		}
	})
	t.Run("provider labels win when present", func(t *testing.T) {
		provider := axcFixedIPIntel{desc: sessionIPDescription{NetworkLabel: "corp", LocationLabel: "geo:office"}}
		got := describeSessionIP(provider, "8.8.8.8")
		if got.NetworkLabel != "corp" || got.LocationLabel != "geo:office" {
			t.Fatalf("unexpected desc: %+v", got)
		}
	})
	t.Run("heuristic provider classifies", func(t *testing.T) {
		got := heuristicSessionIPIntelligence{}.DescribeIP("100.64.0.1")
		if got.NetworkLabel != "private" || got.LocationLabel != "shared_address_space" {
			t.Fatalf("unexpected desc: %+v", got)
		}
	})
}

func TestAxcNewHTTPSessionIPIntelligence(t *testing.T) {
	p := NewHTTPSessionIPIntelligence(" http://geo.example.com/ ", "  key-1  ", "  X-Geo-Key  ", 0)
	if p.baseURL != "http://geo.example.com" {
		t.Fatalf("baseURL=%q", p.baseURL)
	}
	if p.apiKey != "key-1" {
		t.Fatalf("apiKey=%q", p.apiKey)
	}
	if p.authHeader != "X-Geo-Key" {
		t.Fatalf("authHeader=%q", p.authHeader)
	}
	if p.client.Timeout != 1500*time.Millisecond {
		t.Fatalf("timeout=%v", p.client.Timeout)
	}
	if got := p.lookupURL("1.2.3.4"); got != "http://geo.example.com/1.2.3.4" {
		t.Fatalf("lookupURL=%q", got)
	}

	p2 := NewHTTPSessionIPIntelligence("http://geo.example.com/base/{ip}", "k", "", -time.Second)
	if p2.authHeader != "Authorization" {
		t.Fatalf("default authHeader=%q", p2.authHeader)
	}
	if p2.client.Timeout != 1500*time.Millisecond {
		t.Fatalf("timeout=%v", p2.client.Timeout)
	}
	if got := p2.lookupURL(" 1.2.3.4 "); got != "http://geo.example.com/base/1.2.3.4" {
		t.Fatalf("lookupURL=%q", got)
	}
}

func TestAxcHTTPSessionIPIntelligenceDescribeIPBranches(t *testing.T) {
	t.Run("nil receiver returns zero", func(t *testing.T) {
		var p *HTTPSessionIPIntelligence
		if got := p.DescribeIP("8.8.8.8"); got != (sessionIPDescription{}) {
			t.Fatalf("unexpected desc: %+v", got)
		}
	})
	t.Run("empty base url returns zero", func(t *testing.T) {
		p := NewHTTPSessionIPIntelligence("", "k", "", time.Second)
		if got := p.DescribeIP("8.8.8.8"); got != (sessionIPDescription{}) {
			t.Fatalf("unexpected desc: %+v", got)
		}
	})
	t.Run("blank ip returns zero", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("server should not be called for blank ip")
		}))
		defer srv.Close()
		p := NewHTTPSessionIPIntelligence(srv.URL, "", "", time.Second)
		if got := p.DescribeIP("   "); got != (sessionIPDescription{}) {
			t.Fatalf("unexpected desc: %+v", got)
		}
	})
	t.Run("invalid url returns zero", func(t *testing.T) {
		p := NewHTTPSessionIPIntelligence("http://127.0.0.7\x7f/{ip}", "k", "", time.Second)
		if got := p.DescribeIP("8.8.8.8"); got != (sessionIPDescription{}) {
			t.Fatalf("unexpected desc: %+v", got)
		}
	})
	t.Run("transport failure returns zero", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		closedURL := srv.URL
		srv.Close()
		p := NewHTTPSessionIPIntelligence(closedURL, "", "", time.Second)
		if got := p.DescribeIP("8.8.8.8"); got != (sessionIPDescription{}) {
			t.Fatalf("unexpected desc: %+v", got)
		}
	})
	t.Run("invalid payload returns zero", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("not-json"))
		}))
		defer srv.Close()
		p := NewHTTPSessionIPIntelligence(srv.URL, "", "", time.Second)
		if got := p.DescribeIP("8.8.8.8"); got != (sessionIPDescription{}) {
			t.Fatalf("unexpected desc: %+v", got)
		}
	})
	t.Run("top level labels preferred over nested data", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"network_label":"public","location_label":"geo:top","data":{"network_label":"private","location_label":"geo:nested"}}`))
		}))
		defer srv.Close()
		p := NewHTTPSessionIPIntelligence(srv.URL, "", "", time.Second)
		got := p.DescribeIP("8.8.8.8")
		if got.NetworkLabel != "public" || got.LocationLabel != "geo:top" {
			t.Fatalf("unexpected desc: %+v", got)
		}
	})
	t.Run("nested data fills missing labels", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":{"network_label":"vpn","location_label":"geo:nested"}}`))
		}))
		defer srv.Close()
		p := NewHTTPSessionIPIntelligence(srv.URL+"/ip/{ip}", "", "", time.Second)
		got := p.DescribeIP("8.8.8.8")
		if got.NetworkLabel != "vpn" || got.LocationLabel != "geo:nested" {
			t.Fatalf("unexpected desc: %+v", got)
		}
	})
	t.Run("empty json object returns zero labels", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()
		p := NewHTTPSessionIPIntelligence(srv.URL, "", "", time.Second)
		if got := p.DescribeIP("8.8.8.8"); got != (sessionIPDescription{}) {
			t.Fatalf("unexpected desc: %+v", got)
		}
	})
}
