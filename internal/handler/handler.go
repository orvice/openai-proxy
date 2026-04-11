package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"butterfly.orx.me/core/log"
	"github.com/firebase/genkit/go/genkit"
	"github.com/gin-gonic/gin"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
	"github.com/orvice/aiproxy/internal/config"
	"github.com/orvice/aiproxy/internal/controlplane"
	"github.com/orvice/aiproxy/internal/mcp"
	"github.com/orvice/aiproxy/internal/vendor"
	"github.com/orvice/aiproxy/internal/workflows"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	// VendorManager instance
	vendorManager       *vendor.VendorManager
	mcpManager          *mcp.Manager
	controlPlaneManager *controlplane.Manager
)

// These functions are no longer needed as they are now part of the vendor.Vender implementation

func initVendorManager() {
	// Create a new vendor manager with the configuration
	vendorManager = vendor.NewVendorManager(config.Conf)
	mcpManager = mcp.NewManager(config.Conf)

	// Initialize the vendor manager
	err := vendorManager.Initialize()
	if err != nil {
		slog.Error("Failed to initialize vendor manager", "error", err)
	}
	if err := mcpManager.Initialize(); err != nil {
		slog.Error("Failed to initialize mcp manager", "error", err)
	}
	// Still initialize Gemini separately since it's not part of the vendor manager yet
	initGeminiProxy()
}

func initControlPlane() {
	controlPlaneManager = controlplane.NewManager(config.Conf)
	if err := controlPlaneManager.Initialize(); err != nil {
		slog.Error("Failed to initialize control plane manager", "error", err)
		controlPlaneManager = nil
	}
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
	r.Use(otelgin.Middleware("aiproxy"))
	initVendorManager()
	initControlPlane()

	r.GET("/", Pong)

	v1 := r.Group("/v1")
	if controlPlaneManager != nil && controlPlaneManager.Enabled() {
		v1.Use(gatewayAuthMiddleware())
	}
	v1.GET("/models", Models)
	v1.Any("/chat/completions", ChatComplections)
	v1.Any("/responses", Responses)
	v1.Any("/responses/:id", ResponseByID)

	r.Any("/v1beta/models/:model", geminiHandler)
	r.Any("/v1beta/models", geminiHandler)
	r.GET("/mcp/servers", MCPServers)
	r.Any("/mcp", MCPGateway)
	r.Any("/mcp/:server", MCPGateway)
	r.Any("/mcp/:server/*path", MCPGateway)

	for _, flow := range genkit.ListFlows(workflows.Genkit()) {
		v1.POST("/workflows/"+flow.Name(), func(c *gin.Context) {
			genkit.Handler(flow)(c.Writer, c.Request)
		})
	}

	if controlPlaneManager != nil && controlPlaneManager.Enabled() {
		controlPlaneManager.Mount(r)
	}

	r.NoRoute(proxy)
}

func MCPServers(c *gin.Context) {
	if mcpManager == nil || !mcpManager.HasServers() {
		c.JSON(http.StatusOK, gin.H{
			"data": []string{},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": mcpManager.ListServerNames(),
	})
}

func MCPGateway(c *gin.Context) {
	logger := log.FromContext(c.Request.Context())
	if mcpManager == nil || !mcpManager.HasServers() {
		c.JSON(http.StatusNotFound, gin.H{"error": "no mcp servers configured"})
		return
	}

	serverName := c.Param("server")
	if serverName == "" {
		serverName = c.Request.Header.Get("x-mcp-server")
	}

	resolvedName, ok := mcpManager.ResolveServerName(serverName)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown mcp server"})
		return
	}

	proxy, ok := mcpManager.GetProxy(resolvedName)
	if !ok {
		c.JSON(http.StatusBadGateway, gin.H{"error": "mcp proxy unavailable"})
		return
	}

	routePrefix := "/mcp"
	if c.Param("server") != "" {
		routePrefix = "/mcp/" + c.Param("server")
	}
	c.Request.Header.Set("X-Mcp-Route-Prefix", routePrefix)

	start := time.Now()
	rpcMethods, rpcIDs, batchSize := parseMCPRequestMeta(c)
	span := trace.SpanFromContext(c.Request.Context())
	span.SetAttributes(
		attribute.String("mcp.server", resolvedName),
		attribute.String("mcp.route_prefix", routePrefix),
		attribute.String("mcp.request.path", c.Request.URL.Path),
		attribute.String("mcp.http.method", c.Request.Method),
		attribute.Int("mcp.batch_size", batchSize),
	)
	if len(rpcMethods) > 0 {
		span.SetAttributes(attribute.StringSlice("mcp.rpc.methods", rpcMethods))
	}
	if len(rpcIDs) > 0 {
		span.SetAttributes(attribute.StringSlice("mcp.rpc.ids", rpcIDs))
	}
	logger.Info("mcp gateway request",
		"method", c.Request.Method,
		"server", resolvedName,
		"rpc_methods", rpcMethods,
		"rpc_ids", rpcIDs,
		"batch_size", batchSize,
		"path", c.Request.URL.Path)

	capture := newResponseCapture(c.Writer)
	c.Writer = capture

	proxy.ServeHTTP(c.Writer, c.Request)

	attrs := []any{
		"method", c.Request.Method,
		"server", resolvedName,
		"status", capture.Status(),
		"duration_ms", time.Since(start).Milliseconds(),
		"path", c.Request.URL.Path,
	}
	if len(rpcMethods) > 0 {
		attrs = append(attrs, "rpc_methods", rpcMethods)
	}
	if len(rpcIDs) > 0 {
		attrs = append(attrs, "rpc_ids", rpcIDs)
	}
	if batchSize > 0 {
		attrs = append(attrs, "batch_size", batchSize)
	}
	if rpcErr := parseMCPErrorResponse(capture.body.Bytes()); rpcErr != nil {
		attrs = append(attrs,
			"rpc_error_code", rpcErr.Code,
			"rpc_error_message", rpcErr.Message)
		span.SetAttributes(
			attribute.Int("mcp.rpc.error_code", rpcErr.Code),
			attribute.String("mcp.rpc.error_message", rpcErr.Message),
		)
		span.SetStatus(codes.Error, rpcErr.Message)
	}
	if capture.Status() >= http.StatusBadRequest {
		span.SetStatus(codes.Error, http.StatusText(capture.Status()))
		span.SetAttributes(attribute.Int("mcp.http.status_code", capture.Status()))
	} else {
		span.SetAttributes(attribute.Int("mcp.http.status_code", capture.Status()))
	}
	logger.Info("mcp gateway response", attrs...)
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
	tenantID := c.GetString(gatewayContextTenantIDKey)

	logger.Info("models request",
		"CF-Connecting-IP", c.Request.Header.Get("CF-Connecting-IP"),
		"ua", c.Request.UserAgent(),
		"vendor", vendorName,
		"tenant_id", tenantID,
		"path", c.Request.URL.Path,
		"method", c.Request.Method)

	if controlPlaneManager != nil && controlPlaneManager.Enabled() && tenantID != "" {
		models, err := controlPlaneManager.ListVisibleLogicalModels(c.Request.Context(), tenantID)
		if err != nil {
			logger.Error("list tenant logical models failed", "tenant_id", tenantID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{
					"message": "failed to list tenant models",
					"type":    "server_error",
					"code":    "model_list_failed",
				},
			})
			return
		}

		response := mapLogicalModelsToModelList(tenantID, models)
		logger.Info("returned tenant-filtered logical models", "tenant_id", tenantID, "model_count", len(response.Data))
		c.JSON(http.StatusOK, response)
		return
	}

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

// responseCapture wraps http.ResponseWriter to capture response body
type responseCapture struct {
	gin.ResponseWriter
	body        *bytes.Buffer
	isStreaming bool
}

func newResponseCapture(w gin.ResponseWriter) *responseCapture {
	return &responseCapture{
		ResponseWriter: w,
		body:           &bytes.Buffer{},
	}
}

func (r *responseCapture) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

func (r *responseCapture) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return r.ResponseWriter.Hijack()
}

// tokenUsage represents token consumption from API response
type tokenUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

type rawTokenUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

// usageEnvelope parses usage from both chat-completions and responses APIs.
type usageEnvelope struct {
	Usage *rawTokenUsage `json:"usage"`
}

type mcpRPCMessage struct {
	ID     any    `json:"id"`
	Method string `json:"method"`
}

type mcpRPCErrorEnvelope struct {
	Error *mcpRPCError `json:"error"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func parseMCPRequestMeta(c *gin.Context) ([]string, []string, int) {
	if c.Request.Body == nil {
		return nil, nil, 0
	}
	if c.Request.Method != http.MethodPost && c.Request.Method != http.MethodPut && c.Request.Method != http.MethodPatch {
		return nil, nil, 0
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, nil, 0
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

	if len(bytes.TrimSpace(body)) == 0 {
		return nil, nil, 0
	}

	var single mcpRPCMessage
	if err := json.Unmarshal(body, &single); err == nil {
		return compactMCPMethods([]string{single.Method}), compactMCPIDs([]any{single.ID}), 1
	}

	var batch []mcpRPCMessage
	if err := json.Unmarshal(body, &batch); err == nil {
		methods := make([]string, 0, len(batch))
		ids := make([]any, 0, len(batch))
		for _, item := range batch {
			methods = append(methods, item.Method)
			ids = append(ids, item.ID)
		}
		return compactMCPMethods(methods), compactMCPIDs(ids), len(batch)
	}

	return nil, nil, 0
}

func compactMCPMethods(methods []string) []string {
	result := make([]string, 0, len(methods))
	for _, method := range methods {
		if method == "" {
			continue
		}
		result = append(result, method)
	}
	return result
}

func compactMCPIDs(ids []any) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		switch v := id.(type) {
		case nil:
			continue
		case string:
			if v != "" {
				result = append(result, v)
			}
		default:
			result = append(result, fmt.Sprint(v))
		}
	}
	return result
}

func parseMCPErrorResponse(body []byte) *mcpRPCError {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}

	var single mcpRPCErrorEnvelope
	if err := json.Unmarshal(body, &single); err == nil && single.Error != nil {
		return single.Error
	}

	var batch []mcpRPCErrorEnvelope
	if err := json.Unmarshal(body, &batch); err == nil {
		for _, item := range batch {
			if item.Error != nil {
				return item.Error
			}
		}
	}

	return nil
}

// parseTokenUsage extracts token usage from response body (handles both streaming and non-streaming)
func parseTokenUsage(body []byte, isStreaming bool) *tokenUsage {
	if isStreaming {
		// For streaming responses, look for the last data chunk with usage
		lines := strings.Split(string(body), "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			line := strings.TrimPrefix(lines[i], "data: ")
			if line == "" || line == "[DONE]" {
				continue
			}
			if usage := parseTokenUsagePayload([]byte(line)); usage != nil {
				return usage
			}
		}
		return nil
	}

	return parseTokenUsagePayload(body)
}

func parseTokenUsagePayload(payload []byte) *tokenUsage {
	var resp usageEnvelope
	if err := json.Unmarshal(payload, &resp); err != nil || resp.Usage == nil {
		return nil
	}

	promptTokens := resp.Usage.PromptTokens
	if promptTokens == 0 {
		promptTokens = resp.Usage.InputTokens
	}

	completionTokens := resp.Usage.CompletionTokens
	if completionTokens == 0 {
		completionTokens = resp.Usage.OutputTokens
	}

	totalTokens := resp.Usage.TotalTokens
	if totalTokens == 0 {
		totalTokens = promptTokens + completionTokens
	}

	return &tokenUsage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
	}
}

func ChatComplections(c *gin.Context) {
	logger := log.FromContext(c.Request.Context())
	startedAt := time.Now().UTC()
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
	requestedModel := model
	vendorName := vendorManager.GetVendorForModel(model)
	tenantID := c.GetString(gatewayContextTenantIDKey)
	projectID := c.GetString(gatewayContextProjectIDKey)
	gatewayKeyID := c.GetString(gatewayContextKeyIDKey)
	if controlPlaneManager != nil && controlPlaneManager.Enabled() && tenantID != "" {
		if err := controlPlaneManager.AdmitInferenceRequest(c.Request.Context(), tenantID, projectID, gatewayKeyID); err != nil {
			writeGatewayRoutingError(c, err)
			return
		}

		decision, err := controlPlaneManager.ResolveInferenceRoute(c.Request.Context(), tenantID, projectID, gatewayKeyID, requestedModel)
		if err != nil {
			writeGatewayRoutingError(c, err)
			return
		}
		if decision != nil {
			vendorName = decision.VendorName
			model = decision.UpstreamModel
			if model != requestedModel {
				if err := rewriteRequestModel(c, model); err != nil {
					logger.Error("rewrite request model failed", "requested_model", requestedModel, "upstream_model", model, "error", err)
					c.JSON(http.StatusInternalServerError, gin.H{
						"error": gin.H{
							"message": "failed to prepare upstream request",
							"type":    "server_error",
							"code":    "request_rewrite_failed",
						},
					})
					return
				}
			}
			c.Request.Header.Set("X-Logical-Model-Id", decision.LogicalModelID)
			if decision.PricingSnapshotID != "" {
				c.Request.Header.Set("X-Pricing-Snapshot-Id", decision.PricingSnapshotID)
			}
		}
	}

	logger.Info("chat completions request",
		"CF-Connecting-IP", c.Request.Header.Get("CF-Connecting-IP"),
		"ua", c.Request.UserAgent(),
		"method", c.Request.Method,
		"requested_model", requestedModel,
		"upstream_model", model,
		"vendor", vendorName,
		"tenant_id", tenantID,
		"project_id", projectID,
		"message_count", len(req.Messages),
		"context_size", contextSize,
		"path", c.Request.URL.Path)

	// Wrap response writer to capture response
	capture := newResponseCapture(c.Writer)
	c.Writer = capture

	// Get the proxy for the vendor and serve the request
	proxy := vendorManager.GetProxyForVendor(vendorName)
	proxy.ServeHTTP(c.Writer, c.Request)
	completedAt := time.Now().UTC()

	// Check if streaming by content-type
	contentType := capture.Header().Get("Content-Type")
	isStreaming := strings.Contains(contentType, "text/event-stream")
	failureCategory := ""
	failureMessage := ""
	if capture.Status() >= http.StatusBadGateway {
		failureCategory = "upstream_error"
		failureMessage = http.StatusText(capture.Status())
	}

	// Parse and log token usage
	if usage := parseTokenUsage(capture.body.Bytes(), isStreaming); usage != nil {
		logger.Info("chat completions token usage",
			"requested_model", requestedModel,
			"upstream_model", model,
			"vendor", vendorName,
			"prompt_tokens", usage.PromptTokens,
			"completion_tokens", usage.CompletionTokens,
			"total_tokens", usage.TotalTokens)
		recordGatewayUsage(c, vendorName, model, usage, startedAt, completedAt)
		recordGatewayRequestTrace(c, vendorName, model, usage, startedAt, completedAt, failureCategory, failureMessage)
		return
	}

	recordGatewayUsage(c, vendorName, model, nil, startedAt, completedAt)
	recordGatewayRequestTrace(c, vendorName, model, nil, startedAt, completedAt, failureCategory, failureMessage)
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
		startedAt := time.Now().UTC()
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
		requestedModel := model
		vendorName := vendorManager.GetVendorForModel(model)
		tenantID := c.GetString(gatewayContextTenantIDKey)
		projectID := c.GetString(gatewayContextProjectIDKey)
		gatewayKeyID := c.GetString(gatewayContextKeyIDKey)
		if controlPlaneManager != nil && controlPlaneManager.Enabled() && tenantID != "" {
			if err := controlPlaneManager.AdmitInferenceRequest(c.Request.Context(), tenantID, projectID, gatewayKeyID); err != nil {
				writeGatewayRoutingError(c, err)
				return
			}

			decision, err := controlPlaneManager.ResolveInferenceRoute(c.Request.Context(), tenantID, projectID, gatewayKeyID, requestedModel)
			if err != nil {
				writeGatewayRoutingError(c, err)
				return
			}
			if decision != nil {
				vendorName = decision.VendorName
				model = decision.UpstreamModel
				if model != requestedModel {
					if err := rewriteRequestModel(c, model); err != nil {
						logger.Error("rewrite responses model failed", "requested_model", requestedModel, "upstream_model", model, "error", err)
						c.JSON(http.StatusInternalServerError, gin.H{
							"error": gin.H{
								"message": "failed to prepare upstream request",
								"type":    "server_error",
								"code":    "request_rewrite_failed",
							},
						})
						return
					}
				}
				c.Request.Header.Set("X-Logical-Model-Id", decision.LogicalModelID)
				if decision.PricingSnapshotID != "" {
					c.Request.Header.Set("X-Pricing-Snapshot-Id", decision.PricingSnapshotID)
				}
			}
		}
		logger.Info("responses request",
			"method", c.Request.Method,
			"requested_model", requestedModel,
			"upstream_model", model,
			"vendor", vendorName,
			"tenant_id", tenantID,
			"project_id", projectID,
			"input_count", len(items),
			"context_size", contextSize,
			"path", c.Request.URL.Path)

		capture := newResponseCapture(c.Writer)
		c.Writer = capture

		proxy := vendorManager.GetProxyForVendor(vendorName)
		proxy.ServeHTTP(c.Writer, c.Request)
		completedAt := time.Now().UTC()

		contentType := capture.Header().Get("Content-Type")
		isStreaming := strings.Contains(contentType, "text/event-stream")
		failureCategory := ""
		failureMessage := ""
		if capture.Status() >= http.StatusBadGateway {
			failureCategory = "upstream_error"
			failureMessage = http.StatusText(capture.Status())
		}
		if usage := parseTokenUsage(capture.body.Bytes(), isStreaming); usage != nil {
			logger.Info("responses token usage",
				"requested_model", requestedModel,
				"upstream_model", model,
				"vendor", vendorName,
				"prompt_tokens", usage.PromptTokens,
				"completion_tokens", usage.CompletionTokens,
				"total_tokens", usage.TotalTokens)
			recordGatewayUsage(c, vendorName, model, usage, startedAt, completedAt)
			recordGatewayRequestTrace(c, vendorName, model, usage, startedAt, completedAt, failureCategory, failureMessage)
			return
		}
		recordGatewayUsage(c, vendorName, model, nil, startedAt, completedAt)
		recordGatewayRequestTrace(c, vendorName, model, nil, startedAt, completedAt, failureCategory, failureMessage)
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
