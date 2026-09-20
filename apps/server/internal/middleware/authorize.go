package middleware

import (
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/gin-gonic/gin"
)

func RequirePrincipalKinds(required ...string) gin.HandlerFunc {
	return platformauth.RequirePrincipalKinds(required...)
}
