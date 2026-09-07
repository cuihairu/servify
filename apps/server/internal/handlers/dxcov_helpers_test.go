package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func dxcRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

func dxcDo(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != "" {
		reader = bytes.NewReader([]byte(body))
	}
	var req *http.Request
	if reader != nil {
		req = httptest.NewRequest(method, target, reader)
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func dxcDecode(t *testing.T, w *httptest.ResponseRecorder, out interface{}) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
		t.Fatalf("decode body %q: %v", w.Body.String(), err)
	}
}
