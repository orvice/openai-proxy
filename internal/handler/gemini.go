package handler

import (
	"net/http/httputil"
	"net/url"

	"github.com/gin-gonic/gin"
)

var (
	geminiEndpoint = "https://generativelanguage.googleapis.com"
)

func geminiHandler(c *gin.Context) {
	u, err := url.Parse(geminiEndpoint)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	proxy.ServeHTTP(c.Writer, c.Request)
}
