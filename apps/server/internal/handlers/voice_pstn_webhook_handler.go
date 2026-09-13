package handlers

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	voicedelivery "servify/apps/server/internal/modules/voice/delivery"
	"servify/apps/server/internal/platform/voiceprotocol"

	"github.com/gin-gonic/gin"
)

// PSTNWebhookHandler receives hosted vendor PSTN webhooks (form-encoded,
// vendor-signed) on the adapter's fixed public path. It is anonymous by
// design: the vendor signature is the authentication. Responses are an
// empty 200 — status callbacks need no TwiML — while invalid payloads
// answer 4xx so the vendor's retry schedule surfaces the problem.
type PSTNWebhookHandler struct {
	coordinator       *voicedelivery.Coordinator
	adapter           voiceprotocol.HostedVendorWebhookAdapter
	validateSignature bool
	publicBaseURL     string
}

func NewPSTNWebhookHandler(coordinator *voicedelivery.Coordinator, adapter voiceprotocol.HostedVendorWebhookAdapter, validateSignature bool, publicBaseURL string) *PSTNWebhookHandler {
	return &PSTNWebhookHandler{
		coordinator:       coordinator,
		adapter:           adapter,
		validateSignature: validateSignature,
		publicBaseURL:     strings.TrimRight(publicBaseURL, "/"),
	}
}

// RegisterPSTNWebhookRoutes mounts the webhook at the adapter's declared
// path (striped of the group prefix). No route exists without a registered
// hosted-vendor adapter, so a disabled provider serves 404.
func RegisterPSTNWebhookRoutes(r *gin.RouterGroup, handler *PSTNWebhookHandler) {
	if r == nil || handler == nil || handler.adapter == nil {
		return
	}
	path := strings.TrimPrefix(handler.adapter.WebhookPath(), "/public")
	r.POST(path, handler.HandleWebhook)
}

// HandleWebhook consumes one vendor status callback: validate signature,
// map the form payload into normalized voice protocol events, and forward
// them to the coordinator.
func (h *PSTNWebhookHandler) HandleWebhook(c *gin.Context) {
	if h.coordinator == nil || h.adapter == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "pstn webhook runtime unavailable"})
		return
	}
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "read request body: " + err.Error()})
		return
	}

	if h.validateSignature {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		err := h.adapter.ValidateSignature(ctx, h.requestURL(c), flattenHeaders(c.Request.Header), body)
		cancel()
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "error": "webhook signature rejected"})
			return
		}
	}

	form, err := url.ParseQuery(string(body))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "parse form body: " + err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	callEvents, mediaEvents, err := h.adapter.MapWebhook(ctx, form)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	for _, event := range callEvents {
		if err := h.coordinator.HandleCallEvent(ctx, event); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
	}
	for _, event := range mediaEvents {
		if err := h.coordinator.HandleMediaEvent(ctx, event); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
	}
	// Status callbacks only need a 2xx; no TwiML response is produced.
	c.Status(http.StatusOK)
}

// requestURL reconstructs the exact URL the vendor signed. The configured
// public base URL wins (proxies change the request Host); otherwise the
// request's own scheme/host is used, which matches direct local calls.
func (h *PSTNWebhookHandler) requestURL(c *gin.Context) string {
	requestURI := c.Request.URL.RequestURI()
	if h.publicBaseURL != "" {
		return h.publicBaseURL + requestURI
	}
	scheme := c.Request.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		if c.Request.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	return scheme + "://" + c.Request.Host + requestURI
}

func flattenHeaders(header http.Header) map[string]string {
	headers := make(map[string]string, len(header))
	for key, values := range header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}
	return headers
}
