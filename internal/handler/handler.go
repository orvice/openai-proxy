package handler

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"

	"butterfly.orx.me/core/log"
	"github.com/gin-gonic/gin"
	"github.com/orvice/openapi-proxy/internal/config"
	"github.com/orvice/openapi-proxy/internal/vendor"
)

var (
	authHeader = "Authorization"
	
	// VendorManager instance
	vendorManager *vendor.VendorManager
)

// These functions are no longer needed as they are now part of the vendor.Vender implementation

func initVendorManager() {
	// Create a new vendor manager with the configuration
	vendorManager = vendor.NewVendorManager(config.Conf)
	
	// Initialize the vendor manager
	err := vendorManager.Initialize()
	if err != nil {
		slog.Error("Failed to initialize vendor manager", "error", err)
	}
	
	// Still initialize Gemini separately since it's not part of the vendor manager yet
	initGeminiProxy()
}

func Router(r *gin.Engine) {
	initVendorManager()
	r.GET("/v1/models", Models)
	r.Any("/v1/chat/completions", ChatComplections)
	r.Any("/v1beta/models/:model", geminiHandler)
	r.NoRoute(proxy)
}

func proxy(c *gin.Context) {
	slog.Info("proxy request",
		"CF-Connecting-IP", c.Request.Header.Get("CF-Connecting-IP"),
		"ua", c.Request.UserAgent(),
		"method", c.Request.Method,
		"path", c.Request.URL.Path)

	vendorName := c.Request.Header.Get("x-vendor")
	
	// Get the proxy for the specified vendor
	proxy := vendorManager.GetProxyForVendor(vendorName)
	
	// Serve the request using the proxy
	proxy.ServeHTTP(c.Writer, c.Request)
}

type ModelList struct {
	Object string        `json:"object"`
	Data   []ModelObject `json:"data"`
}

type ModelObject struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func Models(c *gin.Context) {
	var modelObjects []ModelObject
	for _, m := range config.Conf.Models {
		modelObjects = append(modelObjects, ModelObject{
			ID:      m.Name,
			Object:  "model",
			Created: 1686935002,
			OwnedBy: "organization-owner",
		})
	}

	response := ModelList{
		Object: "list",
		Data:   modelObjects,
	}
	c.JSON(http.StatusOK, response)
}

type completionsRequest struct {
	Model string `json:"model"`
}

func ChatComplections(c *gin.Context) {
	logger := log.FromContext(c.Request.Context())
	var req completionsRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		logger.Error("bind json error",
			"error", err)
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Restore request body for proxy
	if bodyBytes, exists := c.Get(gin.BodyBytesKey); exists {
		logger.Info("restoring request body", "len", len(bodyBytes.([]byte)))
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes.([]byte)))
	}

	// Get the appropriate vendor for the model
	vendorName := vendorManager.GetVendorForModel(req.Model)

	logger.Info("proxy request",
		"CF-Connecting-IP", c.Request.Header.Get("CF-Connecting-IP"),
		"ua", c.Request.UserAgent(),
		"method", c.Request.Method,
		"model", req.Model,
		"vendor", vendorName,
		"path", c.Request.URL.Path)

	// Get the proxy for the vendor and serve the request
	proxy := vendorManager.GetProxyForModel(req.Model)
	proxy.ServeHTTP(c.Writer, c.Request)
}
