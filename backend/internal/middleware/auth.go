package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"parily.dev/app/internal/auth"
)

const (
	CtxUserID = "userID"
	CtxEmail  = "email"
)

func RequireAuth(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := auth.ParseToken(c, jwtSecret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Set(CtxUserID, claims.UserID)
		c.Set(CtxEmail, claims.Email)
		c.Next()
	}
}
