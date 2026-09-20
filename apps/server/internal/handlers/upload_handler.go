package handlers

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"servify/apps/server/internal/platform/storage"

	"github.com/gin-gonic/gin"
)

// FileUploadHandler handles file upload endpoints using the storage.Provider abstraction.
type FileUploadHandler struct {
	provider    storage.Provider
	maxSize     int64 // bytes
	allowedExts map[string]bool
}

// UploadHandlerConfig parameterizes the upload handler.
// An empty AllowedExts list falls back to the built-in attachment whitelist.
type UploadHandlerConfig struct {
	MaxSize     int64
	AllowedExts []string
}

// NewFileUploadHandlerWithConfig creates a handler with an explicit size limit
// and extension whitelist (usually derived from config.Upload).
func NewFileUploadHandlerWithConfig(provider storage.Provider, cfg UploadHandlerConfig) *FileUploadHandler {
	allowed := make(map[string]bool, len(cfg.AllowedExts))
	for _, ext := range cfg.AllowedExts {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext != "" {
			allowed[ext] = true
		}
	}
	if len(allowed) == 0 {
		allowed = defaultAllowedExts()
	}
	return &FileUploadHandler{provider: provider, maxSize: cfg.MaxSize, allowedExts: allowed}
}

// defaultAllowedExts 返回内建附件白名单（与 config 默认值保持一致）。
func defaultAllowedExts() map[string]bool {
	return map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
		".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
		".txt": true, ".csv": true, ".md": true, ".zip": true, ".mp3": true, ".mp4": true,
	}
}

// Upload handles file upload.
func (h *FileUploadHandler) Upload(c *gin.Context) {
	if h.maxSize > 0 {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.maxSize)
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择文件", "message": err.Error()})
		return
	}
	defer file.Close()

	// Validate extension
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !h.allowedExts[ext] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的文件类型", "message": fmt.Sprintf("文件类型 %s 不被允许", ext)})
		return
	}

	// Build storage key with date partition
	dateDir := time.Now().Format("2006/01/02")
	key := dateDir + "/" + fmt.Sprintf("%d_%s", time.Now().UnixNano(), filepath.Base(header.Filename))

	info, err := h.provider.Save(key, file, header.Size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存文件失败"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":  "上传成功",
		"filename": header.Filename,
		"url":      info.URL,
		"size":     info.Size,
	})
}
