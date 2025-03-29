package handler

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/gin-gonic/gin"
)

var (
	geminiEndpoint = "https://generativelanguage.googleapis.com"
	geminiProxy    *httputil.ReverseProxy
)

func geminiHandler(c *gin.Context) {
	geminiProxy.ServeHTTP(c.Writer, c.Request)
}

func initGeminiProxy() {
	u, err := url.Parse(geminiEndpoint)
	if err != nil {
		slog.Error("parse url error", "error", err)
		return
	}
	geminiProxy = httputil.NewSingleHostReverseProxy(u)

	originalDirector := geminiProxy.Director
	geminiProxy.Director = func(req *http.Request) {
		originalDirector(req)
		slog.Info("new gemini request",
			"path", req.URL.Path)
		req.Host = u.Host
		req.URL.Host = u.Host
	}
}
