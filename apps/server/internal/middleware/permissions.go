package middleware

import (
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/gin-gonic/gin"
)

func RequireResourcePermission(resource string) gin.HandlerFunc {
	return platformauth.RequireResourcePermission(resource)
}
