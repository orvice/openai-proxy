package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"butterfly.orx.me/core/log"
	"github.com/gin-gonic/gin"

	"github.com/orvice/aiproxy/internal/controlplane"
	"github.com/orvice/aiproxy/internal/vendor"
	llmv1 "github.com/orvice/aiproxy/pkg/proto/llm/v1"
)

const (
	gatewayContextTenantIDKey  = "gateway.tenant_id"
	gatewayContextProjectIDKey = "gateway.project_id"
	gatewayContextKeyIDKey     = "gateway.key_id"
)

func gatewayAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if controlPlaneManager == nil || !controlPlaneManager.Enabled() {
			c.Next()
			return
		}

		apiKey := resolveGatewayAPIKey(c.Request)
		principal, err := controlPlaneManager.AuthenticateGatewayAPIKey(c.Request.Context(), apiKey)
		if err != nil {
			writeGatewayAuthError(c, err)
			return
		}
		if principal == nil || principal.GatewayKey == nil || principal.Tenant == nil {
			writeGatewayAuthError(c, controlplane.ErrGatewayKeyInvalid)
			return
		}

		tenantID := principal.Tenant.GetId()
		projectID := ""
		if principal.Project != nil {
			projectID = principal.Project.GetId()
		}

		c.Set(gatewayContextTenantIDKey, tenantID)
		c.Set(gatewayContextProjectIDKey, projectID)
		c.Set(gatewayContextKeyIDKey, principal.GatewayKey.GetId())

		c.Request.Header.Set("X-Tenant-Id", tenantID)
		c.Request.Header.Set("X-Gateway-Key-Id", principal.GatewayKey.GetId())
		if projectID != "" {
			c.Request.Header.Set("X-Project-Id", projectID)
		} else {
			c.Request.Header.Del("X-Project-Id")
		}

		_ = controlPlaneManager.RecordAuditEvent(c.Request.Context(), controlplane.AuditEventInput{
			TenantID:           tenantID,
			ProjectID:          projectID,
			ActorID:            principal.GatewayKey.GetId(),
			ActorType:          "gateway_key",
			Action:             llmv1.AuditAction_AUDIT_ACTION_ALLOWED,
			Outcome:            llmv1.AuditOutcome_AUDIT_OUTCOME_SUCCEEDED,
			TargetResourceType: "inference_request",
			TargetResourceID:   c.Request.URL.Path,
			RequestID:          strings.TrimSpace(c.Request.Header.Get("X-Request-Id")),
			Message:            "gateway request admitted",
		})

		c.Next()
	}
}

func resolveGatewayAPIKey(r *http.Request) string {
	if r == nil {
		return ""
	}

	if key := strings.TrimSpace(r.Header.Get("X-AI-Gateway-Key")); key != "" {
		return key
	}
	if key := strings.TrimSpace(r.Header.Get("X-API-Key")); key != "" {
		return key
	}
	if auth := strings.TrimSpace(r.Header.Get("Authorization")); auth != "" {
		const bearerPrefix = "bearer "
		if len(auth) > len(bearerPrefix) && strings.EqualFold(auth[:len(bearerPrefix)], bearerPrefix) {
			return strings.TrimSpace(auth[len(bearerPrefix):])
		}
	}

	return ""
}

func writeGatewayAuthError(c *gin.Context, err error) {
	code := "invalid_api_key"
	message := "Invalid API key provided"
	status := http.StatusUnauthorized

	switch {
	case errors.Is(err, controlplane.ErrGatewayKeyRequired):
		code = "missing_api_key"
		message = "API key is required for /v1 requests"
	case errors.Is(err, controlplane.ErrGatewayKeyInvalid):
		code = "invalid_api_key"
		message = "Invalid API key provided"
	case errors.Is(err, controlplane.ErrGatewayKeyInactive):
		code = "api_key_inactive"
		message = "API key is inactive"
	case errors.Is(err, controlplane.ErrGatewayKeyExpired):
		code = "api_key_expired"
		message = "API key has expired"
	case errors.Is(err, controlplane.ErrGatewayTenantDenied):
		code = "tenant_inactive"
		message = "Tenant is not active for this API key"
	case errors.Is(err, controlplane.ErrGatewayProjectDenied):
		code = "project_inactive"
		message = "Project is not active for this API key"
	default:
		code = "auth_internal_error"
		message = "Unable to validate API key"
		status = http.StatusInternalServerError
	}

	logger := log.FromContext(c.Request.Context())
	logger.Warn("gateway auth failed", "path", c.Request.URL.Path, "code", code, "error", err)

	_ = controlPlaneManager.RecordAuditEvent(c.Request.Context(), controlplane.AuditEventInput{
		TenantID:           c.GetString(gatewayContextTenantIDKey),
		ProjectID:          c.GetString(gatewayContextProjectIDKey),
		ActorID:            c.GetString(gatewayContextKeyIDKey),
		ActorType:          "gateway_key",
		Action:             llmv1.AuditAction_AUDIT_ACTION_DENIED,
		Outcome:            llmv1.AuditOutcome_AUDIT_OUTCOME_FAILED,
		TargetResourceType: "inference_request",
		TargetResourceID:   c.Request.URL.Path,
		RequestID:          strings.TrimSpace(c.Request.Header.Get("X-Request-Id")),
		Message:            message,
		Metadata:           map[string]string{"code": code},
	})

	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "invalid_request_error",
			"code":    code,
		},
	})
}

func writeGatewayRoutingError(c *gin.Context, err error) {
	code := "routing_error"
	message := "unable to route this request"
	status := http.StatusBadRequest

	switch {
	case errors.Is(err, controlplane.ErrLogicalModelNotFound):
		code = "model_not_found"
		message = "requested model is not available for this tenant"
	case errors.Is(err, controlplane.ErrQuotaExceeded):
		code = "quota_exhausted"
		message = "request quota exceeded for this key"
		status = http.StatusTooManyRequests
	case errors.Is(err, controlplane.ErrRoutingTargetMissing):
		code = "routing_target_unavailable"
		message = "no active routing target for requested model"
	case errors.Is(err, controlplane.ErrRoutingVendorInvalid):
		code = "routing_vendor_unavailable"
		message = "configured routing vendor is unavailable"
	default:
		code = "routing_internal_error"
		message = "failed to resolve model route"
		status = http.StatusInternalServerError
	}

	logger := log.FromContext(c.Request.Context())
	logger.Warn("gateway routing failed", "path", c.Request.URL.Path, "code", code, "error", err)

	_ = controlPlaneManager.RecordAuditEvent(c.Request.Context(), controlplane.AuditEventInput{
		TenantID:           c.GetString(gatewayContextTenantIDKey),
		ProjectID:          c.GetString(gatewayContextProjectIDKey),
		ActorID:            c.GetString(gatewayContextKeyIDKey),
		ActorType:          "gateway_key",
		Action:             llmv1.AuditAction_AUDIT_ACTION_DENIED,
		Outcome:            llmv1.AuditOutcome_AUDIT_OUTCOME_FAILED,
		TargetResourceType: "routing_policy",
		TargetResourceID:   strings.TrimSpace(c.Request.Header.Get("X-Logical-Model-Id")),
		RequestID:          strings.TrimSpace(c.Request.Header.Get("X-Request-Id")),
		Message:            message,
		Metadata:           map[string]string{"code": code},
	})

	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "invalid_request_error",
			"code":    code,
		},
	})
}

func rewriteRequestModel(c *gin.Context, model string) error {
	if c == nil {
		return fmt.Errorf("gin context is nil")
	}

	var body []byte
	if bodyBytes, exists := c.Get(gin.BodyBytesKey); exists {
		cached, ok := bodyBytes.([]byte)
		if !ok {
			return fmt.Errorf("invalid body cache type")
		}
		body = append([]byte(nil), cached...)
	} else if c.Request != nil && c.Request.Body != nil {
		payload, err := io.ReadAll(c.Request.Body)
		if err != nil {
			return fmt.Errorf("read request body: %w", err)
		}
		body = payload
	}
	if len(body) == 0 {
		return fmt.Errorf("request body is empty")
	}

	payload := make(map[string]any)
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("unmarshal request body: %w", err)
	}
	payload["model"] = model

	updated, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request body: %w", err)
	}

	c.Set(gin.BodyBytesKey, updated)
	c.Request.Body = io.NopCloser(bytes.NewBuffer(updated))
	c.Request.ContentLength = int64(len(updated))
	c.Request.Header.Set("Content-Length", fmt.Sprint(len(updated)))
	return nil
}

func mapLogicalModelsToModelList(tenantID string, models []*llmv1.LogicalModel) vendor.ModelList {
	data := make([]vendor.ModelObject, 0, len(models))
	seen := make(map[string]struct{}, len(models))

	for _, model := range models {
		if model == nil {
			continue
		}
		modelID := strings.TrimSpace(model.GetName())
		if modelID == "" {
			modelID = strings.TrimSpace(model.GetId())
		}
		if modelID == "" {
			continue
		}
		if _, exists := seen[modelID]; exists {
			continue
		}
		seen[modelID] = struct{}{}

		createdAt := time.Now().Unix()
		if model.GetAuditInfo() != nil && model.GetAuditInfo().GetCreatedAt() != nil {
			if ts := model.GetAuditInfo().GetCreatedAt().AsTime(); !ts.IsZero() {
				createdAt = ts.Unix()
			}
		}

		data = append(data, vendor.ModelObject{
			ID:          modelID,
			Object:      "model",
			Created:     createdAt,
			OwnedBy:     tenantID,
			Name:        model.GetDisplayName(),
			Description: model.GetDescription(),
		})
	}

	sort.SliceStable(data, func(i, j int) bool {
		return data[i].ID < data[j].ID
	})

	return vendor.ModelList{
		Object: "list",
		Data:   data,
	}
}

func recordGatewayUsage(c *gin.Context, vendorID, upstreamModel string, usage *tokenUsage, startedAt, completedAt time.Time) {
	if controlPlaneManager == nil || !controlPlaneManager.Enabled() {
		return
	}

	tenantID := c.GetString(gatewayContextTenantIDKey)
	if tenantID == "" {
		return
	}

	var promptTokens int64
	var completionTokens int64
	var totalTokens int64
	if usage != nil {
		promptTokens = usage.PromptTokens
		completionTokens = usage.CompletionTokens
		totalTokens = usage.TotalTokens
	}

	requestID := strings.TrimSpace(c.Writer.Header().Get("X-Request-Id"))
	if requestID == "" {
		requestID = strings.TrimSpace(c.Writer.Header().Get("x-request-id"))
	}

	err := controlPlaneManager.RecordUsage(c.Request.Context(), controlplane.UsageEvent{
		TenantID:          tenantID,
		ProjectID:         c.GetString(gatewayContextProjectIDKey),
		GatewayKeyID:      c.GetString(gatewayContextKeyIDKey),
		LogicalModelID:    strings.TrimSpace(c.Request.Header.Get("X-Logical-Model-Id")),
		PricingSnapshotID: strings.TrimSpace(c.Request.Header.Get("X-Pricing-Snapshot-Id")),
		VendorID:          strings.TrimSpace(vendorID),
		UpstreamModel:     strings.TrimSpace(upstreamModel),
		RequestID:         requestID,
		PromptTokens:      promptTokens,
		CompletionTokens:  completionTokens,
		TotalTokens:       totalTokens,
		StartedAt:         startedAt,
		CompletedAt:       completedAt,
		Status:            llmv1.ResourceStatus_RESOURCE_STATUS_ACTIVE,
	})
	if err != nil {
		logger := log.FromContext(c.Request.Context())
		logger.Warn("record usage failed", "tenant_id", tenantID, "vendor", vendorID, "error", err)
	}
}

func recordGatewayRequestTrace(c *gin.Context, vendorID, upstreamModel string, usage *tokenUsage, startedAt, completedAt time.Time, failureCategory, failureMessage string) {
	if controlPlaneManager == nil || !controlPlaneManager.Enabled() {
		return
	}

	tenantID := c.GetString(gatewayContextTenantIDKey)
	if tenantID == "" {
		return
	}

	requestID := strings.TrimSpace(c.Writer.Header().Get("X-Request-Id"))
	if requestID == "" {
		requestID = strings.TrimSpace(c.Writer.Header().Get("x-request-id"))
	}
	if requestID == "" {
		requestID = strings.TrimSpace(c.Request.Header.Get("X-Request-Id"))
	}
	if requestID == "" {
		return
	}

	var totalTokens int64
	if usage != nil {
		totalTokens = usage.TotalTokens
	}

	status := llmv1.ResourceStatus_RESOURCE_STATUS_ACTIVE
	if failureCategory != "" || c.Writer.Status() >= http.StatusBadRequest {
		status = llmv1.ResourceStatus_RESOURCE_STATUS_SUSPENDED
	}

	err := controlPlaneManager.RecordRequestTrace(c.Request.Context(), controlplane.RequestTraceEvent{
		RequestID:       requestID,
		TenantID:        tenantID,
		ProjectID:       c.GetString(gatewayContextProjectIDKey),
		GatewayKeyID:    c.GetString(gatewayContextKeyIDKey),
		LogicalModelID:  strings.TrimSpace(c.Request.Header.Get("X-Logical-Model-Id")),
		VendorID:        strings.TrimSpace(vendorID),
		UpstreamModel:   strings.TrimSpace(upstreamModel),
		Status:          status,
		FailureCategory: failureCategory,
		FailureMessage:  failureMessage,
		LatencyMs:       completedAt.Sub(startedAt).Milliseconds(),
		TotalTokens:     totalTokens,
		StartedAt:       startedAt,
		CompletedAt:     completedAt,
	})
	if err != nil {
		logger := log.FromContext(c.Request.Context())
		logger.Warn("record request trace failed", "request_id", requestID, "error", err)
	}
}
