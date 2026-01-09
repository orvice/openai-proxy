package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"butterfly.orx.me/core/log"
	"github.com/firebase/genkit/go/genkit"
	"github.com/gin-gonic/gin"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
	"github.com/orvice/openapi-proxy/internal/config"
	"github.com/orvice/openapi-proxy/internal/vendor"
	"github.com/orvice/openapi-proxy/internal/workflows"
)

var (
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

func loggingMiddleware(c *gin.Context) {
	logger := log.FromContext(c.Request.Context())
	logger.Info("request",
		"CF-Connecting-IP", c.Request.Header.Get("CF-Connecting-IP"),
		"ua", c.Request.UserAgent(),
		"method", c.Request.Method,
		"path", c.Request.URL.Path)
	c.Next()
}

func Router(r *gin.Engine) {
	r.Use(loggingMiddleware)
	initVendorManager()

	r.GET("/", Pong)
	r.GET("/v1/models", Models)
	r.Any("/v1/chat/completions", ChatComplections)
	r.Any("/v1/responses", Responses)
	r.Any("/v1/responses/:id", ResponseByID)
	r.Any("/v1beta/models/:model", geminiHandler)
	r.Any("/v1beta/models", geminiHandler)

	for _, flow := range genkit.ListFlows(workflows.Genkit()) {
		r.POST("/v1/workflows/"+flow.Name(), func(c *gin.Context) {
			genkit.Handler(flow)(c.Writer, c.Request)
		})
	}

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

func Models(c *gin.Context) {
	logger := log.FromContext(c.Request.Context())
	vendorName := c.Request.Header.Get("x-vendor")

	logger.Info("models request",
		"CF-Connecting-IP", c.Request.Header.Get("CF-Connecting-IP"),
		"ua", c.Request.UserAgent(),
		"vendor", vendorName,
		"path", c.Request.URL.Path,
		"method", c.Request.Method)

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second*10)
	defer cancel()

	logger.Debug("Processing models request", "vendor_requested", vendorName)

	// If a specific vendor is requested, return only that vendor's models
	if vendorName != "" {
		logger.Debug("Fetching models for specific vendor", "vendor", vendorName)
		vender := vendorManager.GetVendor(vendorName)

		// Check if models should be hidden for this vendor
		if vender.ShouldHideModels() {
			logger.Info("Models are hidden for this vendor", "vendor", vendorName)
			// Return empty models list
			c.JSON(http.StatusOK, vendor.ModelList{
				Object: "list",
				Data:   []vendor.ModelObject{},
			})
			return
		}

		modelsData, err := vender.Models(ctx)
		if err != nil {
			logger.Error("error getting models for vendor", "vendor", vendorName, "error", err)
			logger.Warn("Falling back to static models for specific vendor", "vendor", vendorName)
			// Fallback to static models if API call fails
			fallbackToStaticModels(c)
			return
		}
		logger.Info("Successfully returned models for specific vendor",
			"vendor", vendorName,
			"model_count", len(modelsData.Data))
		c.JSON(http.StatusOK, modelsData)
		return
	}

	// If no specific vendor is requested, combine models from all vendors
	logger.Info("Combining models from all vendors")

	// Get all vendor names
	vendorNames := vendorManager.GetAllVendorNames()
	logger.Debug("Retrieved vendor names", "count", len(vendorNames))

	// Create a combined model list
	allModels := make([]vendor.ModelObject, 0)
	modelMap := make(map[string]bool) // To track unique model IDs

	// First try to get models from each vendor
	successCount := 0
	for _, vendorName := range vendorNames {
		logger.Debug("Fetching models from vendor", "vendor", vendorName)
		vender := vendorManager.GetVendor(vendorName)

		// Skip vendors with HideModels set to true
		if vender.ShouldHideModels() {
			logger.Debug("Skipping vendor with hidden models", "vendor", vendorName)
			continue
		}

		modelsData, err := vender.Models(ctx)
		if err != nil {
			logger.Error("error getting models for vendor", "vendor", vendorName, "error", err)
			logger.Warn("Failed to get models from vendor", "vendor", vendorName, "error", err)
			continue
		}

		// Add models to the combined list, avoiding duplicates
		modelCount := 0
		for _, model := range modelsData.Data {
			if _, exists := modelMap[model.ID]; !exists {
				allModels = append(allModels, model)
				modelMap[model.ID] = true
				modelCount++
			}
		}
		logger.Debug("Added models from vendor",
			"vendor", vendorName,
			"models_added", modelCount,
			"total_models", len(allModels))
		successCount++
	}

	// If we couldn't get models from any vendor, fallback to static models
	if successCount == 0 && len(allModels) == 0 {
		logger.Warn("Failed to get models from any vendor, falling back to static models")
		fallbackToStaticModels(c)
		return
	}

	// Return the combined model list
	logger.Info("Successfully combined models from multiple vendors",
		"vendor_count", successCount,
		"total_models", len(allModels))
	response := vendor.ModelList{
		Object: "list",
		Data:   allModels,
	}
	c.JSON(http.StatusOK, response)
}

// Helper function to return static models as a fallback
func fallbackToStaticModels(c *gin.Context) {
	// Pre-allocate the slice with the capacity equal to the number of models
	modelObjects := make([]vendor.ModelObject, 0)
	for _, m := range config.Conf.Models {
		modelObjects = append(modelObjects, vendor.ModelObject{
			ID:      m.Name,
			Object:  "model",
			Created: 1686935002,
			OwnedBy: "organization-owner",
		})
	}

	response := vendor.ModelList{
		Object: "list",
		Data:   modelObjects,
	}
	c.JSON(http.StatusOK, response)
}

type message struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // can be string or array of content parts
}

// chatCompletionsRequest wraps openai.ChatCompletionNewParams for proxy parsing
type chatCompletionsRequest struct {
	Model    shared.ChatModel `json:"model"`
	Messages []message        `json:"messages"`
}

// calculateContextSize calculates the approximate context size from messages
func calculateContextSize(messages []message) int {
	totalSize := 0
	for _, msg := range messages {
		totalSize += calculateContentSize(msg.Content)
	}
	return totalSize
}

// calculateContentSize calculates size from content (string or array)
func calculateContentSize(content any) int {
	switch c := content.(type) {
	case string:
		return len(c)
	case []any:
		size := 0
		for _, part := range c {
			if partMap, ok := part.(map[string]any); ok {
				if text, ok := partMap["text"].(string); ok {
					size += len(text)
				}
			}
		}
		return size
	}
	return 0
}

func ChatComplections(c *gin.Context) {
	logger := log.FromContext(c.Request.Context())
	var req chatCompletionsRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		logger.Error("bind json error",
			"error", err)
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Calculate context size
	contextSize := calculateContextSize(req.Messages)

	// Restore request body for proxy
	if bodyBytes, exists := c.Get(gin.BodyBytesKey); exists {
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes.([]byte)))
	}

	model := string(req.Model)
	// Get the appropriate vendor for the model
	vendorName := vendorManager.GetVendorForModel(model)

	logger.Info("chat completions request",
		"CF-Connecting-IP", c.Request.Header.Get("CF-Connecting-IP"),
		"ua", c.Request.UserAgent(),
		"method", c.Request.Method,
		"model", model,
		"vendor", vendorName,
		"message_count", len(req.Messages),
		"context_size", contextSize,
		"path", c.Request.URL.Path)

	// Get the proxy for the vendor and serve the request
	proxy := vendorManager.GetProxyForModel(model)
	proxy.ServeHTTP(c.Writer, c.Request)
}

func Pong(c *gin.Context) {
	c.JSON(http.StatusOK, map[string]any{
		"time": time.Now().Unix(),
	})
}

type inputItem struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// responsesRequest wraps responses.ResponseNewParams for proxy parsing
type responsesRequest struct {
	Model shared.ResponsesModel `json:"model"`
	Input json.RawMessage       `json:"input"` // can be string or array of input items
}

// parseInputItems parses the input field which can be string or array
func parseInputItems(input json.RawMessage) ([]inputItem, int) {
	if len(input) == 0 {
		return nil, 0
	}

	// Try parsing as string first
	var str string
	if err := json.Unmarshal(input, &str); err == nil {
		return []inputItem{{Content: str}}, len(str)
	}

	// Try parsing as array of input items
	var items []inputItem
	if err := json.Unmarshal(input, &items); err == nil {
		size := 0
		for _, item := range items {
			size += calculateContentSize(item.Content)
		}
		return items, size
	}

	return nil, 0
}

// Responses handles the OpenAI Responses API (/v1/responses)
func Responses(c *gin.Context) {
	logger := log.FromContext(c.Request.Context())

	// For POST requests, parse the model from body
	if c.Request.Method == http.MethodPost {
		var req responsesRequest
		if err := c.ShouldBindBodyWithJSON(&req); err != nil {
			logger.Error("bind json error", "error", err)
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		// Parse input and calculate context size
		items, contextSize := parseInputItems(req.Input)

		// Restore request body for proxy
		if bodyBytes, exists := c.Get(gin.BodyBytesKey); exists {
			c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes.([]byte)))
		}

		model := string(req.Model)
		vendorName := vendorManager.GetVendorForModel(model)
		logger.Info("responses request",
			"method", c.Request.Method,
			"model", model,
			"vendor", vendorName,
			"input_count", len(items),
			"context_size", contextSize,
			"path", c.Request.URL.Path)

		proxy := vendorManager.GetProxyForModel(model)
		proxy.ServeHTTP(c.Writer, c.Request)
		return
	}

	// For other methods (GET for listing), use default vendor
	vendorName := c.Request.Header.Get("x-vendor")
	logger.Info("responses request",
		"method", c.Request.Method,
		"vendor", vendorName,
		"path", c.Request.URL.Path)

	proxy := vendorManager.GetProxyForVendor(vendorName)
	proxy.ServeHTTP(c.Writer, c.Request)
}

// Ensure openai-go types are used (for compile-time verification)
var (
	_ openai.ChatCompletionNewParams
	_ responses.ResponseNewParams
)

// ResponseByID handles individual response operations (/v1/responses/:id)
func ResponseByID(c *gin.Context) {
	logger := log.FromContext(c.Request.Context())
	responseID := c.Param("id")

	vendorName := c.Request.Header.Get("x-vendor")
	logger.Info("response by id request",
		"method", c.Request.Method,
		"response_id", responseID,
		"vendor", vendorName,
		"path", c.Request.URL.Path)

	proxy := vendorManager.GetProxyForVendor(vendorName)
	proxy.ServeHTTP(c.Writer, c.Request)
}
