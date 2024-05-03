package whitelist

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func IPWhiteListMiddleware(whitelist map[string]bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()

		if !whitelist[ip] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"data": "You are not authorised to use this endpoint",
			})
			return
		}
	}
}
