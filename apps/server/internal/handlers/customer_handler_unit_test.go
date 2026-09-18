package handlers

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"servify/apps/server/internal/models"
	customerapi "servify/apps/server/internal/modules/customer/api"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func newCustomerUnitRouter(svc *unitCustomerService) (*gin.Engine, *CustomerHandler) {
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	h := NewCustomerHandler(svc, logger)
	r := gin.New()
	RegisterCustomerRoutes(&r.RouterGroup, h)
	return r, h
}

func customerUnitRequest(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCustomerHandlerUnitCreate(t *testing.T) {
	body := `{"username":"c1","email":"c1@e.com"}`
	customer := &models.User{ID: 11, Username: "c1"}
	cases := []struct {
		name string
		svc  *unitCustomerService
		want int
	}{
		{"success", &unitCustomerService{customer: customer}, http.StatusCreated},
		{"invalid body", &unitCustomerService{customer: customer}, http.StatusBadRequest},
		{"invalid input", &unitCustomerService{createErr: errors.New("username is required")}, http.StatusBadRequest},
		{"conflict", &unitCustomerService{createErr: errors.New("username already exists")}, http.StatusConflict},
		{"internal", &unitCustomerService{createErr: errors.New("boom")}, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sendBody := body
			if tc.name == "invalid body" {
				sendBody = `{`
			}
			r, _ := newCustomerUnitRouter(tc.svc)
			w := customerUnitRequest(r, http.MethodPost, "/customers", sendBody)
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestCustomerHandlerUnitGet(t *testing.T) {
	customer := &models.User{ID: 12}
	cases := []struct {
		name string
		svc  *unitCustomerService
		path string
		want int
	}{
		{"success", &unitCustomerService{customer: customer}, "/customers/12", http.StatusOK},
		{"bad id", &unitCustomerService{}, "/customers/nope", http.StatusBadRequest},
		{"not found", &unitCustomerService{getErr: errors.New("customer not found")}, "/customers/12", http.StatusNotFound},
		{"internal", &unitCustomerService{getErr: errors.New("boom")}, "/customers/12", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newCustomerUnitRouter(tc.svc)
			w := customerUnitRequest(r, http.MethodGet, tc.path, "")
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestCustomerHandlerUnitUpdate(t *testing.T) {
	customer := &models.User{ID: 13, Name: "n"}
	body := `{"name":"n2"}`
	cases := []struct {
		name string
		svc  *unitCustomerService
		path string
		body string
		want int
	}{
		{"success", &unitCustomerService{customer: customer}, "/customers/13", body, http.StatusOK},
		{"zero id skips snapshot", &unitCustomerService{customer: customer}, "/customers/0", body, http.StatusOK},
		{"bad id", &unitCustomerService{}, "/customers/abc", body, http.StatusBadRequest},
		{"invalid body", &unitCustomerService{}, "/customers/13", `{`, http.StatusBadRequest},
		{"not found", &unitCustomerService{updateErr: errors.New("customer not found")}, "/customers/13", body, http.StatusNotFound},
		{"invalid input", &unitCustomerService{updateErr: errors.New("invalid field")}, "/customers/13", body, http.StatusBadRequest},
		{"internal", &unitCustomerService{updateErr: errors.New("boom")}, "/customers/13", body, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newCustomerUnitRouter(tc.svc)
			w := customerUnitRequest(r, http.MethodPut, tc.path, tc.body)
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestCustomerHandlerUnitRevokeTokens(t *testing.T) {
	cases := []struct {
		name string
		svc  *unitCustomerService
		path string
		want int
	}{
		{"success", &unitCustomerService{customer: &models.User{ID: 14}, revokeVer: 1}, "/customers/14/revoke-tokens", http.StatusOK},
		{"bad id", &unitCustomerService{}, "/customers/x/revoke-tokens", http.StatusBadRequest},
		{"not found", &unitCustomerService{revokeErr: errors.New("customer not found")}, "/customers/14/revoke-tokens", http.StatusNotFound},
		{"invalid", &unitCustomerService{revokeErr: errors.New("invalid id")}, "/customers/14/revoke-tokens", http.StatusBadRequest},
		{"internal", &unitCustomerService{revokeErr: errors.New("boom")}, "/customers/14/revoke-tokens", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newCustomerUnitRouter(tc.svc)
			w := customerUnitRequest(r, http.MethodPost, tc.path, "")
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestCustomerHandlerUnitList(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &unitCustomerService{customers: []customerapi.CustomerInfo{{User: models.User{ID: 1}}}}
		r, _ := newCustomerUnitRouter(svc)
		w := customerUnitRequest(r, http.MethodGet, "/customers?page=2&page_size=5", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("bad query", func(t *testing.T) {
		r, _ := newCustomerUnitRouter(&unitCustomerService{})
		w := customerUnitRequest(r, http.MethodGet, "/customers?page=abc", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("error", func(t *testing.T) {
		r, _ := newCustomerUnitRouter(&unitCustomerService{listErr: errors.New("boom")})
		w := customerUnitRequest(r, http.MethodGet, "/customers", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestCustomerHandlerUnitActivity(t *testing.T) {
	cases := []struct {
		name string
		svc  *unitCustomerService
		path string
		want int
	}{
		{"success default limit", &unitCustomerService{activity: &customerapi.CustomerActivity{CustomerID: 15}}, "/customers/15/activity", http.StatusOK},
		{"bad limit falls back", &unitCustomerService{activity: &customerapi.CustomerActivity{}}, "/customers/15/activity?limit=zzz", http.StatusOK},
		{"explicit limit", &unitCustomerService{activity: &customerapi.CustomerActivity{}}, "/customers/15/activity?limit=3", http.StatusOK},
		{"bad id", &unitCustomerService{}, "/customers/bad/activity", http.StatusBadRequest},
		{"not found", &unitCustomerService{activityErr: errors.New("customer not found")}, "/customers/15/activity", http.StatusNotFound},
		{"internal", &unitCustomerService{activityErr: errors.New("boom")}, "/customers/15/activity", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newCustomerUnitRouter(tc.svc)
			w := customerUnitRequest(r, http.MethodGet, tc.path, "")
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestCustomerHandlerUnitNotesAndTags(t *testing.T) {
	t.Run("note without auth", func(t *testing.T) {
		r, _ := newCustomerUnitRouter(&unitCustomerService{})
		w := customerUnitRequest(r, http.MethodPost, "/customers/16/notes", `{"note":"hi"}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", w.Code)
		}
	})

	gin.SetMode(gin.TestMode)
	withUser := func(svc *unitCustomerService) *gin.Engine {
		logger := logrus.New()
		logger.SetLevel(logrus.ErrorLevel)
		h := NewCustomerHandler(svc, logger)
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Set("user_id", uint(9))
			c.Next()
		})
		RegisterCustomerRoutes(&r.RouterGroup, h)
		return r
	}

	t.Run("note bad id", func(t *testing.T) {
		r := withUser(&unitCustomerService{})
		w := customerUnitRequest(r, http.MethodPost, "/customers/x/notes", `{"note":"hi"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("note invalid body", func(t *testing.T) {
		r := withUser(&unitCustomerService{})
		w := customerUnitRequest(r, http.MethodPost, "/customers/16/notes", `{}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("note service errors", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			svc  *unitCustomerService
			want int
		}{
			{"not found", &unitCustomerService{noteErr: errors.New("customer not found")}, http.StatusNotFound},
			{"invalid", &unitCustomerService{noteErr: errors.New("invalid note")}, http.StatusBadRequest},
			{"internal", &unitCustomerService{noteErr: errors.New("boom")}, http.StatusInternalServerError},
		} {
			r := withUser(tc.svc)
			w := customerUnitRequest(r, http.MethodPost, "/customers/16/notes", `{"note":"hi"}`)
			if w.Code != tc.want {
				t.Fatalf("%s: status = %d want %d", tc.name, w.Code, tc.want)
			}
		}
	})

	t.Run("note success with after snapshot", func(t *testing.T) {
		r := withUser(&unitCustomerService{customer: &models.User{ID: 16}})
		w := customerUnitRequest(r, http.MethodPost, "/customers/16/notes", `{"note":"hi"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("note success but after snapshot fails", func(t *testing.T) {
		svc := &unitCustomerService{getErr: errors.New("lookup failed after")}
		r := withUser(svc)
		w := customerUnitRequest(r, http.MethodPost, "/customers/16/notes", `{"note":"hi"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("tags bad id", func(t *testing.T) {
		r := withUser(&unitCustomerService{})
		w := customerUnitRequest(r, http.MethodPut, "/customers/x/tags", `{"tags":["a"]}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("tags invalid body", func(t *testing.T) {
		r := withUser(&unitCustomerService{})
		w := customerUnitRequest(r, http.MethodPut, "/customers/16/tags", `{}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("tags not found", func(t *testing.T) {
		r := withUser(&unitCustomerService{tagsErr: errors.New("customer not found")})
		w := customerUnitRequest(r, http.MethodPut, "/customers/16/tags", `{"tags":["a"]}`)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("tags internal error", func(t *testing.T) {
		r := withUser(&unitCustomerService{tagsErr: errors.New("boom")})
		w := customerUnitRequest(r, http.MethodPut, "/customers/16/tags", `{"tags":["a"]}`)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("tags success with snapshot", func(t *testing.T) {
		r := withUser(&unitCustomerService{customer: &models.User{ID: 16}})
		w := customerUnitRequest(r, http.MethodPut, "/customers/16/tags", `{"tags":["a","b"]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("tags success snapshot lookup fails", func(t *testing.T) {
		r := withUser(&unitCustomerService{getErr: errors.New("gone")})
		w := customerUnitRequest(r, http.MethodPut, "/customers/16/tags", `{"tags":["a"]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestCustomerHandlerUnitStats(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		r, _ := newCustomerUnitRouter(&unitCustomerService{stats: &customerapi.CustomerStats{Total: 4}})
		w := customerUnitRequest(r, http.MethodGet, "/customers/stats", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("error", func(t *testing.T) {
		r, _ := newCustomerUnitRouter(&unitCustomerService{statsErr: errors.New("boom")})
		w := customerUnitRequest(r, http.MethodGet, "/customers/stats", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("nil logger path", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		h := NewCustomerHandler(&unitCustomerService{statsErr: errors.New("boom")}, nil)
		r := gin.New()
		r.GET("/stats", h.GetCustomerStats)
		w := customerUnitRequest(r, http.MethodGet, "/stats", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
}
