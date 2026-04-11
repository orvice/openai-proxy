package controlplane

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	llmv1 "github.com/orvice/aiproxy/pkg/proto/llm/v1"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var (
	ErrGatewayKeyRequired   = errors.New("gateway api key is required")
	ErrGatewayKeyInvalid    = errors.New("gateway api key is invalid")
	ErrGatewayKeyInactive   = errors.New("gateway api key is inactive")
	ErrGatewayKeyExpired    = errors.New("gateway api key is expired")
	ErrGatewayTenantDenied  = errors.New("tenant is not active")
	ErrGatewayProjectDenied = errors.New("project is not active")
	ErrLogicalModelNotFound = errors.New("logical model not found")
	ErrRoutingTargetMissing = errors.New("routing target is not available")
	ErrRoutingVendorInvalid = errors.New("routing vendor is not available")
	ErrQuotaExceeded        = errors.New("quota exceeded")
)

type GatewayPrincipal struct {
	GatewayKey *llmv1.GatewayKey
	Tenant     *llmv1.Tenant
	Project    *llmv1.Project
}

type RouteDecision struct {
	LogicalModelID    string
	PricingSnapshotID string
	VendorID          string
	VendorName        string
	UpstreamModel     string
	RoutingPolicy     *llmv1.RoutingPolicy
}

type UsageEvent struct {
	TenantID          string
	ProjectID         string
	GatewayKeyID      string
	LogicalModelID    string
	VendorID          string
	UpstreamModel     string
	RequestID         string
	PricingSnapshotID string
	PromptTokens      int64
	CompletionTokens  int64
	TotalTokens       int64
	StartedAt         time.Time
	CompletedAt       time.Time
	Status            llmv1.ResourceStatus
	Metadata          map[string]string
}

type AuditEventInput struct {
	TenantID           string
	ProjectID          string
	ActorID            string
	ActorType          string
	Action             llmv1.AuditAction
	Outcome            llmv1.AuditOutcome
	TargetResourceType string
	TargetResourceID   string
	RequestID          string
	Message            string
	Metadata           map[string]string
	OccurredAt         time.Time
}

type RequestTraceEvent struct {
	RequestID       string
	TenantID        string
	ProjectID       string
	GatewayKeyID    string
	LogicalModelID  string
	VendorID        string
	UpstreamModel   string
	Status          llmv1.ResourceStatus
	FailureCategory string
	FailureMessage  string
	LatencyMs       int64
	TotalTokens     int64
	StartedAt       time.Time
	CompletedAt     time.Time
	Metadata        map[string]string
}

func (m *Manager) ListVisibleLogicalModels(ctx context.Context, tenantID string) ([]*llmv1.LogicalModel, error) {
	if !m.Enabled() || m.repo == nil {
		return nil, nil
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, nil
	}

	models, err := m.repo.ListLogicalModels(ctx, &llmv1.ListLogicalModelsRequest{
		TenantId: tenantID,
		Status:   llmv1.ResourceStatus_RESOURCE_STATUS_ACTIVE,
	})
	if err != nil {
		return nil, fmt.Errorf("list visible logical models: %w", err)
	}

	return models, nil
}

func (m *Manager) AuthenticateGatewayAPIKey(ctx context.Context, plaintextKey string) (*GatewayPrincipal, error) {
	if !m.Enabled() || m.repo == nil {
		return nil, nil
	}

	plaintextKey = strings.TrimSpace(plaintextKey)
	if plaintextKey == "" {
		return nil, ErrGatewayKeyRequired
	}

	gatewayKey, err := m.repo.GetGatewayKeyBySecret(ctx, plaintextKey)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, ErrGatewayKeyInvalid
		}
		return nil, fmt.Errorf("authenticate gateway key: %w", err)
	}

	if gatewayKey.GetStatus() != llmv1.KeyStatus_KEY_STATUS_ACTIVE {
		return nil, ErrGatewayKeyInactive
	}
	if expiresAt := gatewayKey.GetExpiresAt(); expiresAt != nil {
		if !expiresAt.AsTime().IsZero() && time.Now().UTC().After(expiresAt.AsTime()) {
			return nil, ErrGatewayKeyExpired
		}
	}

	tenant, err := m.repo.GetTenant(ctx, gatewayKey.GetTenantId())
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, ErrGatewayTenantDenied
		}
		return nil, fmt.Errorf("resolve tenant for gateway key: %w", err)
	}
	if tenant.GetStatus() != llmv1.ResourceStatus_RESOURCE_STATUS_ACTIVE {
		return nil, ErrGatewayTenantDenied
	}

	var project *llmv1.Project
	if gatewayKey.GetProjectId() != "" {
		project, err = m.repo.GetProject(ctx, gatewayKey.GetProjectId())
		if err != nil {
			if errors.Is(err, errNotFound) {
				return nil, ErrGatewayProjectDenied
			}
			return nil, fmt.Errorf("resolve project for gateway key: %w", err)
		}
		if project.GetTenantId() != gatewayKey.GetTenantId() || project.GetStatus() != llmv1.ResourceStatus_RESOURCE_STATUS_ACTIVE {
			return nil, ErrGatewayProjectDenied
		}
	}

	return &GatewayPrincipal{
		GatewayKey: gatewayKey,
		Tenant:     tenant,
		Project:    project,
	}, nil
}

func (m *Manager) ResolveInferenceRoute(
	ctx context.Context,
	tenantID, projectID, gatewayKeyID, requestedModel string,
) (*RouteDecision, error) {
	if !m.Enabled() || m.repo == nil {
		return nil, nil
	}

	requestedModel = strings.TrimSpace(requestedModel)
	tenantID = strings.TrimSpace(tenantID)
	projectID = strings.TrimSpace(projectID)
	gatewayKeyID = strings.TrimSpace(gatewayKeyID)
	if requestedModel == "" || tenantID == "" {
		return nil, ErrLogicalModelNotFound
	}

	models, err := m.repo.ListLogicalModels(ctx, &llmv1.ListLogicalModelsRequest{
		TenantId: tenantID,
		Status:   llmv1.ResourceStatus_RESOURCE_STATUS_ACTIVE,
	})
	if err != nil {
		return nil, fmt.Errorf("list logical models for route resolution: %w", err)
	}

	logicalModel := matchLogicalModel(models, requestedModel)
	if logicalModel == nil {
		return nil, ErrLogicalModelNotFound
	}

	policy, err := m.resolveRoutingPolicy(ctx, tenantID, projectID, logicalModel.GetId())
	if err != nil {
		return nil, err
	}

	target := selectRoutingTarget(policy, logicalModel)
	if target == nil {
		return nil, ErrRoutingTargetMissing
	}

	if policy == nil && gatewayKeyID != "" {
		// Reserved for key-specific routing extension. Keep parameter live for now.
		_ = gatewayKeyID
	}

	vendorEntity, err := m.repo.GetVendor(ctx, target.GetVendorId())
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, ErrRoutingVendorInvalid
		}
		return nil, fmt.Errorf("resolve vendor %s: %w", target.GetVendorId(), err)
	}
	if vendorEntity.GetStatus() != llmv1.ResourceStatus_RESOURCE_STATUS_ACTIVE {
		return nil, ErrRoutingVendorInvalid
	}

	vendorName := strings.TrimSpace(vendorEntity.GetName())
	if vendorName == "" {
		vendorName = strings.TrimSpace(target.GetVendorId())
	}
	if vendorName == "" {
		return nil, ErrRoutingVendorInvalid
	}

	upstreamModel := strings.TrimSpace(target.GetUpstreamModel())
	if upstreamModel == "" {
		upstreamModel = requestedModel
	}

	return &RouteDecision{
		LogicalModelID:    logicalModel.GetId(),
		PricingSnapshotID: logicalModel.GetDefaultPricingSnapshotId(),
		VendorID:          target.GetVendorId(),
		VendorName:        vendorName,
		UpstreamModel:     upstreamModel,
		RoutingPolicy:     policy,
	}, nil
}

func (m *Manager) RecordUsage(ctx context.Context, event UsageEvent) error {
	if !m.Enabled() || m.backend == nil {
		return nil
	}
	if strings.TrimSpace(event.TenantID) == "" {
		return nil
	}

	startedAt := event.StartedAt.UTC()
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	completedAt := event.CompletedAt.UTC()
	if completedAt.IsZero() {
		completedAt = startedAt
	}
	status := event.Status
	if status == llmv1.ResourceStatus_RESOURCE_STATUS_UNSPECIFIED {
		status = llmv1.ResourceStatus_RESOURCE_STATUS_ACTIVE
	}

	doc := bson.M{
		"_id":                 uuid.NewString(),
		"tenant_id":           strings.TrimSpace(event.TenantID),
		"project_id":          strings.TrimSpace(event.ProjectID),
		"gateway_key_id":      strings.TrimSpace(event.GatewayKeyID),
		"logical_model_id":    strings.TrimSpace(event.LogicalModelID),
		"vendor_id":           strings.TrimSpace(event.VendorID),
		"upstream_model":      strings.TrimSpace(event.UpstreamModel),
		"request_id":          strings.TrimSpace(event.RequestID),
		"pricing_snapshot_id": strings.TrimSpace(event.PricingSnapshotID),
		"prompt_tokens":       event.PromptTokens,
		"completion_tokens":   event.CompletionTokens,
		"total_tokens":        event.TotalTokens,
		"billable_units":      event.TotalTokens,
		"started_at":          startedAt,
		"completed_at":        completedAt,
		"status":              int32(status),
		"metadata":            event.Metadata,
	}

	if _, err := m.backend.collection(usageRecordsCollection).InsertOne(ctx, doc); err != nil {
		return fmt.Errorf("insert usage record: %w", err)
	}

	return nil
}

func (m *Manager) RecordAuditEvent(ctx context.Context, event AuditEventInput) error {
	if !m.Enabled() || m.backend == nil {
		return nil
	}

	occurredAt := event.OccurredAt.UTC()
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	action := event.Action
	if action == llmv1.AuditAction_AUDIT_ACTION_UNSPECIFIED {
		action = llmv1.AuditAction_AUDIT_ACTION_UPDATED
	}
	outcome := event.Outcome
	if outcome == llmv1.AuditOutcome_AUDIT_OUTCOME_UNSPECIFIED {
		outcome = llmv1.AuditOutcome_AUDIT_OUTCOME_SUCCEEDED
	}

	doc := bson.M{
		"_id":                  uuid.NewString(),
		"tenant_id":            strings.TrimSpace(event.TenantID),
		"project_id":           strings.TrimSpace(event.ProjectID),
		"actor_id":             strings.TrimSpace(event.ActorID),
		"actor_type":           firstNonEmpty(strings.TrimSpace(event.ActorType), "system"),
		"action":               int32(action),
		"outcome":              int32(outcome),
		"target_resource_type": strings.TrimSpace(event.TargetResourceType),
		"target_resource_id":   strings.TrimSpace(event.TargetResourceID),
		"request_id":           strings.TrimSpace(event.RequestID),
		"message":              strings.TrimSpace(event.Message),
		"occurred_at":          occurredAt,
		"metadata":             event.Metadata,
	}

	if _, err := m.backend.collection(auditEventsCollection).InsertOne(ctx, doc); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

func (m *Manager) RecordRequestTrace(ctx context.Context, event RequestTraceEvent) error {
	if !m.Enabled() || m.backend == nil {
		return nil
	}
	if strings.TrimSpace(event.RequestID) == "" {
		return nil
	}

	startedAt := event.StartedAt.UTC()
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	completedAt := event.CompletedAt.UTC()
	if completedAt.IsZero() {
		completedAt = startedAt
	}
	status := event.Status
	if status == llmv1.ResourceStatus_RESOURCE_STATUS_UNSPECIFIED {
		status = llmv1.ResourceStatus_RESOURCE_STATUS_ACTIVE
	}

	doc := bson.M{
		"_id":              strings.TrimSpace(event.RequestID),
		"request_id":       strings.TrimSpace(event.RequestID),
		"tenant_id":        strings.TrimSpace(event.TenantID),
		"project_id":       strings.TrimSpace(event.ProjectID),
		"gateway_key_id":   strings.TrimSpace(event.GatewayKeyID),
		"logical_model_id": strings.TrimSpace(event.LogicalModelID),
		"vendor_id":        strings.TrimSpace(event.VendorID),
		"upstream_model":   strings.TrimSpace(event.UpstreamModel),
		"status":           int32(status),
		"failure_category": strings.TrimSpace(event.FailureCategory),
		"failure_message":  strings.TrimSpace(event.FailureMessage),
		"latency_ms":       event.LatencyMs,
		"total_tokens":     event.TotalTokens,
		"started_at":       startedAt,
		"completed_at":     completedAt,
		"metadata":         event.Metadata,
	}

	if _, err := m.backend.collection(requestTracesCollection).UpdateOne(
		ctx,
		bson.M{"_id": doc["_id"]},
		bson.M{"$set": doc},
		options.UpdateOne().SetUpsert(true),
	); err != nil {
		return fmt.Errorf("upsert request trace: %w", err)
	}
	return nil
}

func (m *Manager) AdmitInferenceRequest(ctx context.Context, tenantID, projectID, gatewayKeyID string) error {
	if !m.Enabled() || m.repo == nil {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	projectID = strings.TrimSpace(projectID)
	gatewayKeyID = strings.TrimSpace(gatewayKeyID)
	if tenantID == "" {
		return nil
	}

	quotaPolicy, err := m.resolveQuotaPolicy(ctx, tenantID, projectID, gatewayKeyID)
	if err != nil {
		return err
	}
	if quotaPolicy == nil {
		return nil
	}
	if quotaPolicy.GetRequestLimit() <= 0 {
		return nil
	}

	return m.incrementRequestQuota(ctx, quotaPolicy)
}

func (m *Manager) resolveRoutingPolicy(ctx context.Context, tenantID, projectID, logicalModelID string) (*llmv1.RoutingPolicy, error) {
	if projectID != "" {
		projectPolicies, err := m.repo.ListRoutingPolicies(ctx, &llmv1.ListRoutingPoliciesRequest{
			TenantId:       tenantID,
			ProjectId:      projectID,
			LogicalModelId: logicalModelID,
			Status:         llmv1.ResourceStatus_RESOURCE_STATUS_ACTIVE,
		})
		if err != nil {
			return nil, fmt.Errorf("list project routing policies: %w", err)
		}
		if policy := selectPolicy(projectPolicies); policy != nil {
			return policy, nil
		}
	}

	tenantPolicies, err := m.repo.ListRoutingPolicies(ctx, &llmv1.ListRoutingPoliciesRequest{
		TenantId:       tenantID,
		LogicalModelId: logicalModelID,
		Status:         llmv1.ResourceStatus_RESOURCE_STATUS_ACTIVE,
	})
	if err != nil {
		return nil, fmt.Errorf("list tenant routing policies: %w", err)
	}
	tenantPolicies = filterPolicies(tenantPolicies, func(policy *llmv1.RoutingPolicy) bool {
		return strings.TrimSpace(policy.GetProjectId()) == ""
	})
	if policy := selectPolicy(tenantPolicies); policy != nil {
		return policy, nil
	}

	globalPolicies, err := m.repo.ListRoutingPolicies(ctx, &llmv1.ListRoutingPoliciesRequest{
		LogicalModelId: logicalModelID,
		Status:         llmv1.ResourceStatus_RESOURCE_STATUS_ACTIVE,
	})
	if err != nil {
		return nil, fmt.Errorf("list global routing policies: %w", err)
	}
	globalPolicies = filterPolicies(globalPolicies, func(policy *llmv1.RoutingPolicy) bool {
		return strings.TrimSpace(policy.GetTenantId()) == "" && strings.TrimSpace(policy.GetProjectId()) == ""
	})

	return selectPolicy(globalPolicies), nil
}

func selectPolicy(policies []*llmv1.RoutingPolicy) *llmv1.RoutingPolicy {
	if len(policies) == 0 {
		return nil
	}

	sort.SliceStable(policies, func(i, j int) bool {
		return policies[i].GetId() < policies[j].GetId()
	})
	return policies[0]
}

func filterPolicies(policies []*llmv1.RoutingPolicy, predicate func(policy *llmv1.RoutingPolicy) bool) []*llmv1.RoutingPolicy {
	result := make([]*llmv1.RoutingPolicy, 0, len(policies))
	for _, policy := range policies {
		if policy == nil {
			continue
		}
		if predicate(policy) {
			result = append(result, policy)
		}
	}
	return result
}

func matchLogicalModel(models []*llmv1.LogicalModel, requestedModel string) *llmv1.LogicalModel {
	for _, model := range models {
		if model == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(model.GetName()), requestedModel) || strings.EqualFold(strings.TrimSpace(model.GetId()), requestedModel) {
			return model
		}
	}
	return nil
}

func (m *Manager) resolveQuotaPolicy(ctx context.Context, tenantID, projectID, gatewayKeyID string) (*llmv1.QuotaPolicy, error) {
	if gatewayKeyID != "" {
		policies, err := m.repo.ListQuotaPolicies(ctx, &llmv1.ListQuotaPoliciesRequest{
			TenantId:     tenantID,
			ProjectId:    projectID,
			GatewayKeyId: gatewayKeyID,
			Status:       llmv1.ResourceStatus_RESOURCE_STATUS_ACTIVE,
		})
		if err != nil {
			return nil, fmt.Errorf("list key quota policies: %w", err)
		}
		if policy := selectQuotaPolicy(policies); policy != nil {
			return policy, nil
		}
	}

	if projectID != "" {
		policies, err := m.repo.ListQuotaPolicies(ctx, &llmv1.ListQuotaPoliciesRequest{
			TenantId:  tenantID,
			ProjectId: projectID,
			Status:    llmv1.ResourceStatus_RESOURCE_STATUS_ACTIVE,
		})
		if err != nil {
			return nil, fmt.Errorf("list project quota policies: %w", err)
		}
		policies = filterQuotaPolicies(policies, func(policy *llmv1.QuotaPolicy) bool {
			return strings.TrimSpace(policy.GetGatewayKeyId()) == ""
		})
		if policy := selectQuotaPolicy(policies); policy != nil {
			return policy, nil
		}
	}

	policies, err := m.repo.ListQuotaPolicies(ctx, &llmv1.ListQuotaPoliciesRequest{
		TenantId: tenantID,
		Status:   llmv1.ResourceStatus_RESOURCE_STATUS_ACTIVE,
	})
	if err != nil {
		return nil, fmt.Errorf("list tenant quota policies: %w", err)
	}
	policies = filterQuotaPolicies(policies, func(policy *llmv1.QuotaPolicy) bool {
		return strings.TrimSpace(policy.GetProjectId()) == "" && strings.TrimSpace(policy.GetGatewayKeyId()) == ""
	})

	return selectQuotaPolicy(policies), nil
}

func selectQuotaPolicy(policies []*llmv1.QuotaPolicy) *llmv1.QuotaPolicy {
	if len(policies) == 0 {
		return nil
	}
	sort.SliceStable(policies, func(i, j int) bool {
		return policies[i].GetId() < policies[j].GetId()
	})
	return policies[0]
}

func filterQuotaPolicies(policies []*llmv1.QuotaPolicy, predicate func(policy *llmv1.QuotaPolicy) bool) []*llmv1.QuotaPolicy {
	result := make([]*llmv1.QuotaPolicy, 0, len(policies))
	for _, policy := range policies {
		if policy == nil {
			continue
		}
		if predicate(policy) {
			result = append(result, policy)
		}
	}
	return result
}

func (m *Manager) incrementRequestQuota(ctx context.Context, quotaPolicy *llmv1.QuotaPolicy) error {
	if m.backend == nil || m.backend.redisClient == nil {
		return nil
	}

	windowStart, windowEnd := quotaWindow(quotaPolicy.GetPeriod(), time.Now().UTC())
	counterKey := m.backend.cacheKey("quota_request", quotaPolicy.GetId(), strconv.FormatInt(windowStart.Unix(), 10))

	count, err := m.backend.redisClient.Incr(ctx, counterKey).Result()
	if err != nil {
		return fmt.Errorf("increment request quota counter: %w", err)
	}
	if count == 1 {
		ttl := windowEnd.Sub(time.Now().UTC()) + 5*time.Second
		if ttl < time.Second {
			ttl = time.Second
		}
		if expireErr := m.backend.redisClient.Expire(ctx, counterKey, ttl).Err(); expireErr != nil {
			return fmt.Errorf("set request quota counter ttl: %w", expireErr)
		}
	}

	if count > quotaPolicy.GetRequestLimit() {
		return ErrQuotaExceeded
	}
	return nil
}

func quotaWindow(period llmv1.QuotaPeriod, now time.Time) (time.Time, time.Time) {
	now = now.UTC()
	switch period {
	case llmv1.QuotaPeriod_QUOTA_PERIOD_MINUTE:
		start := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), 0, 0, time.UTC)
		return start, start.Add(time.Minute)
	case llmv1.QuotaPeriod_QUOTA_PERIOD_HOUR:
		start := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, time.UTC)
		return start, start.Add(time.Hour)
	case llmv1.QuotaPeriod_QUOTA_PERIOD_DAY:
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		return start, start.Add(24 * time.Hour)
	case llmv1.QuotaPeriod_QUOTA_PERIOD_MONTH:
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 1, 0)
	default:
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 1, 0)
	}
}

func selectRoutingTarget(policy *llmv1.RoutingPolicy, logicalModel *llmv1.LogicalModel) *llmv1.VendorTarget {
	targets := make([]*llmv1.VendorTarget, 0)
	if policy != nil && len(policy.GetTargets()) > 0 {
		targets = append(targets, policy.GetTargets()...)
	} else if logicalModel != nil {
		targets = append(targets, logicalModel.GetTargets()...)
	}

	enabledTargets := make([]*llmv1.VendorTarget, 0, len(targets))
	for _, target := range targets {
		if target == nil {
			continue
		}
		if !target.GetEnabled() {
			continue
		}
		if strings.TrimSpace(target.GetVendorId()) == "" {
			continue
		}
		enabledTargets = append(enabledTargets, target)
	}
	if len(enabledTargets) == 0 {
		return nil
	}

	sort.SliceStable(enabledTargets, func(i, j int) bool {
		if enabledTargets[i].GetPriority() == enabledTargets[j].GetPriority() {
			if enabledTargets[i].GetWeight() == enabledTargets[j].GetWeight() {
				return enabledTargets[i].GetVendorId() < enabledTargets[j].GetVendorId()
			}
			return enabledTargets[i].GetWeight() > enabledTargets[j].GetWeight()
		}
		return enabledTargets[i].GetPriority() < enabledTargets[j].GetPriority()
	})
	return enabledTargets[0]
}
