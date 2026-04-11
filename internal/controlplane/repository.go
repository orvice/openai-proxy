package controlplane

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	llmv1 "github.com/orvice/aiproxy/pkg/proto/llm/v1"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var errNotFound = errors.New("resource not found")

type tenantDocument struct {
	ID               string            `bson:"_id"`
	Name             string            `bson:"name"`
	DisplayName      string            `bson:"display_name"`
	Status           int32             `bson:"status"`
	BillingMode      int32             `bson:"billing_mode"`
	BillingProfileID string            `bson:"billing_profile_id"`
	Labels           map[string]string `bson:"labels,omitempty"`
	Annotations      map[string]string `bson:"annotations,omitempty"`
	CreatedBy        string            `bson:"created_by,omitempty"`
	CreatedAt        time.Time         `bson:"created_at"`
	UpdatedBy        string            `bson:"updated_by,omitempty"`
	UpdatedAt        time.Time         `bson:"updated_at"`
}

type projectDocument struct {
	ID          string            `bson:"_id"`
	TenantID    string            `bson:"tenant_id"`
	Name        string            `bson:"name"`
	DisplayName string            `bson:"display_name"`
	Description string            `bson:"description"`
	Status      int32             `bson:"status"`
	Environment string            `bson:"environment"`
	Labels      map[string]string `bson:"labels,omitempty"`
	Annotations map[string]string `bson:"annotations,omitempty"`
	CreatedBy   string            `bson:"created_by,omitempty"`
	CreatedAt   time.Time         `bson:"created_at"`
	UpdatedBy   string            `bson:"updated_by,omitempty"`
	UpdatedAt   time.Time         `bson:"updated_at"`
}

type membershipDocument struct {
	ID            string    `bson:"_id"`
	TenantID      string    `bson:"tenant_id"`
	ProjectID     string    `bson:"project_id,omitempty"`
	PrincipalID   string    `bson:"principal_id"`
	PrincipalType string    `bson:"principal_type"`
	Roles         []int32   `bson:"roles"`
	Status        int32     `bson:"status"`
	ExpiresAt     time.Time `bson:"expires_at,omitempty"`
	CreatedBy     string    `bson:"created_by,omitempty"`
	CreatedAt     time.Time `bson:"created_at"`
	UpdatedBy     string    `bson:"updated_by,omitempty"`
	UpdatedAt     time.Time `bson:"updated_at"`
}

type gatewayKeyDocument struct {
	ID            string            `bson:"_id"`
	TenantID      string            `bson:"tenant_id"`
	ProjectID     string            `bson:"project_id,omitempty"`
	DisplayName   string            `bson:"display_name"`
	SecretHash    string            `bson:"secret_hash"`
	Status        int32             `bson:"status"`
	AllowedModels []string          `bson:"allowed_models,omitempty"`
	Tags          []string          `bson:"tags,omitempty"`
	ExpiresAt     time.Time         `bson:"expires_at,omitempty"`
	QuotaPolicyID string            `bson:"quota_policy_id,omitempty"`
	SpendPolicyID string            `bson:"spend_policy_id,omitempty"`
	LastUsedAt    time.Time         `bson:"last_used_at,omitempty"`
	Labels        map[string]string `bson:"labels,omitempty"`
	Annotations   map[string]string `bson:"annotations,omitempty"`
	CreatedBy     string            `bson:"created_by,omitempty"`
	CreatedAt     time.Time         `bson:"created_at"`
	UpdatedBy     string            `bson:"updated_by,omitempty"`
	UpdatedAt     time.Time         `bson:"updated_at"`
}

type upstreamCredentialDocument struct {
	ID            string            `bson:"_id"`
	VendorType    int32             `bson:"vendor_type"`
	DisplayName   string            `bson:"display_name"`
	Status        int32             `bson:"status"`
	SecretRef     string            `bson:"secret_ref"`
	Scopes        []string          `bson:"scopes,omitempty"`
	LastRotatedAt time.Time         `bson:"last_rotated_at,omitempty"`
	ExpiresAt     time.Time         `bson:"expires_at,omitempty"`
	Labels        map[string]string `bson:"labels,omitempty"`
	Annotations   map[string]string `bson:"annotations,omitempty"`
	CreatedBy     string            `bson:"created_by,omitempty"`
	CreatedAt     time.Time         `bson:"created_at"`
	UpdatedBy     string            `bson:"updated_by,omitempty"`
	UpdatedAt     time.Time         `bson:"updated_at"`
}

type vendorTargetDocument struct {
	VendorID      string `bson:"vendor_id"`
	UpstreamModel string `bson:"upstream_model"`
	Priority      int32  `bson:"priority"`
	Weight        int32  `bson:"weight"`
	Enabled       bool   `bson:"enabled"`
}

type vendorDocument struct {
	ID                    string            `bson:"_id"`
	Name                  string            `bson:"name"`
	DisplayName           string            `bson:"display_name"`
	VendorType            int32             `bson:"vendor_type"`
	Endpoint              string            `bson:"endpoint"`
	UpstreamCredentialID  string            `bson:"upstream_credential_id,omitempty"`
	Status                int32             `bson:"status"`
	SupportedCapabilities []string          `bson:"supported_capabilities,omitempty"`
	TimeoutSeconds        int64             `bson:"timeout_seconds,omitempty"`
	Labels                map[string]string `bson:"labels,omitempty"`
	Annotations           map[string]string `bson:"annotations,omitempty"`
	CreatedBy             string            `bson:"created_by,omitempty"`
	CreatedAt             time.Time         `bson:"created_at"`
	UpdatedBy             string            `bson:"updated_by,omitempty"`
	UpdatedAt             time.Time         `bson:"updated_at"`
}

type logicalModelDocument struct {
	ID                       string                 `bson:"_id"`
	Name                     string                 `bson:"name"`
	DisplayName              string                 `bson:"display_name"`
	Description              string                 `bson:"description"`
	Visibility               int32                  `bson:"visibility"`
	Status                   int32                  `bson:"status"`
	Capabilities             []string               `bson:"capabilities,omitempty"`
	Targets                  []vendorTargetDocument `bson:"targets,omitempty"`
	DefaultPricingSnapshotID string                 `bson:"default_pricing_snapshot_id,omitempty"`
	AllowedTenantIDs         []string               `bson:"allowed_tenant_ids,omitempty"`
	Labels                   map[string]string      `bson:"labels,omitempty"`
	Annotations              map[string]string      `bson:"annotations,omitempty"`
	CreatedBy                string                 `bson:"created_by,omitempty"`
	CreatedAt                time.Time              `bson:"created_at"`
	UpdatedBy                string                 `bson:"updated_by,omitempty"`
	UpdatedAt                time.Time              `bson:"updated_at"`
}

type routingPolicyDocument struct {
	ID                  string                 `bson:"_id"`
	Name                string                 `bson:"name"`
	TenantID            string                 `bson:"tenant_id,omitempty"`
	ProjectID           string                 `bson:"project_id,omitempty"`
	LogicalModelID      string                 `bson:"logical_model_id"`
	Strategy            int32                  `bson:"strategy"`
	Targets             []vendorTargetDocument `bson:"targets,omitempty"`
	Status              int32                  `bson:"status"`
	FailoverEnabled     bool                   `bson:"failover_enabled"`
	HealthCheckRequired bool                   `bson:"health_check_required"`
	Labels              map[string]string      `bson:"labels,omitempty"`
	Annotations         map[string]string      `bson:"annotations,omitempty"`
	CreatedBy           string                 `bson:"created_by,omitempty"`
	CreatedAt           time.Time              `bson:"created_at"`
	UpdatedBy           string                 `bson:"updated_by,omitempty"`
	UpdatedAt           time.Time              `bson:"updated_at"`
}

type quotaPolicyDocument struct {
	ID               string            `bson:"_id"`
	Name             string            `bson:"name"`
	TenantID         string            `bson:"tenant_id"`
	ProjectID        string            `bson:"project_id,omitempty"`
	GatewayKeyID     string            `bson:"gateway_key_id,omitempty"`
	Period           int32             `bson:"period"`
	RequestLimit     int64             `bson:"request_limit"`
	TokenLimit       int64             `bson:"token_limit"`
	ConcurrencyLimit int64             `bson:"concurrency_limit"`
	Status           int32             `bson:"status"`
	Labels           map[string]string `bson:"labels,omitempty"`
	Annotations      map[string]string `bson:"annotations,omitempty"`
	CreatedBy        string            `bson:"created_by,omitempty"`
	CreatedAt        time.Time         `bson:"created_at"`
	UpdatedBy        string            `bson:"updated_by,omitempty"`
	UpdatedAt        time.Time         `bson:"updated_at"`
}

type spendPolicyDocument struct {
	ID           string            `bson:"_id"`
	Name         string            `bson:"name"`
	TenantID     string            `bson:"tenant_id"`
	ProjectID    string            `bson:"project_id,omitempty"`
	GatewayKeyID string            `bson:"gateway_key_id,omitempty"`
	SpendUnits   string            `bson:"spend_units"`
	CurrencyCode string            `bson:"currency_code"`
	Period       int32             `bson:"period"`
	Status       int32             `bson:"status"`
	Labels       map[string]string `bson:"labels,omitempty"`
	Annotations  map[string]string `bson:"annotations,omitempty"`
	CreatedBy    string            `bson:"created_by,omitempty"`
	CreatedAt    time.Time         `bson:"created_at"`
	UpdatedBy    string            `bson:"updated_by,omitempty"`
	UpdatedAt    time.Time         `bson:"updated_at"`
}

type repository struct {
	backend *backend
}

func newRepository(backend *backend) *repository {
	return &repository{backend: backend}
}

func (r *repository) CreateTenant(ctx context.Context, tenant *llmv1.Tenant) (*llmv1.Tenant, error) {
	tenant = ensureTenantDefaults(tenant)
	now := time.Now().UTC()
	doc := tenantDocument{
		ID:               firstNonEmpty(tenant.GetId(), uuid.NewString()),
		Name:             tenant.GetName(),
		DisplayName:      tenant.GetDisplayName(),
		Status:           normalizeResourceStatus(tenant.GetStatus()),
		BillingMode:      normalizeBillingMode(tenant.GetBillingMode()),
		BillingProfileID: tenant.GetBillingProfileId(),
		Labels:           copyMap(tenant.GetMetadata().GetLabels()),
		Annotations:      copyMap(tenant.GetMetadata().GetAnnotations()),
		CreatedBy:        tenant.GetAuditInfo().GetCreatedBy(),
		CreatedAt:        now,
		UpdatedBy:        tenant.GetAuditInfo().GetUpdatedBy(),
		UpdatedAt:        now,
	}

	if _, err := r.backend.collection(tenantsCollection).InsertOne(ctx, doc); err != nil {
		return nil, fmt.Errorf("insert tenant: %w", err)
	}

	result := tenantFromDocument(doc)
	if err := r.backend.cacheSet(ctx, r.backend.cacheKey("tenant", result.GetId()), result); err != nil {
		return nil, fmt.Errorf("cache tenant: %w", err)
	}

	return result, nil
}

func (r *repository) GetTenant(ctx context.Context, tenantID string) (*llmv1.Tenant, error) {
	cacheKey := r.backend.cacheKey("tenant", tenantID)
	cached := &llmv1.Tenant{}
	found, err := r.backend.cacheGet(ctx, cacheKey, cached)
	if err != nil {
		return nil, fmt.Errorf("cache get tenant: %w", err)
	}
	if found {
		return cached, nil
	}

	var doc tenantDocument
	if err := r.backend.collection(tenantsCollection).FindOne(ctx, bson.M{"_id": tenantID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("find tenant: %w", err)
	}

	result := tenantFromDocument(doc)
	if err := r.backend.cacheSet(ctx, cacheKey, result); err != nil {
		return nil, fmt.Errorf("cache tenant: %w", err)
	}

	return result, nil
}

func (r *repository) ListTenants(ctx context.Context, req *llmv1.ListTenantsRequest) ([]*llmv1.Tenant, error) {
	filter := bson.M{}
	if req.GetStatus() != llmv1.ResourceStatus_RESOURCE_STATUS_UNSPECIFIED {
		filter["status"] = int32(req.GetStatus())
	}

	findOptions := options.Find().SetLimit(int64(normalizePageSize(req.GetPage())))
	cursor, err := r.backend.collection(tenantsCollection).Find(ctx, filter, findOptions)
	if err != nil {
		return nil, fmt.Errorf("find tenants: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []tenantDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode tenants: %w", err)
	}

	result := make([]*llmv1.Tenant, 0, len(docs))
	for _, doc := range docs {
		result = append(result, tenantFromDocument(doc))
	}

	return result, nil
}

func (r *repository) UpdateTenant(ctx context.Context, tenant *llmv1.Tenant, paths []string) (*llmv1.Tenant, error) {
	tenant = ensureTenantDefaults(tenant)
	existing, err := r.GetTenant(ctx, tenant.GetId())
	if err != nil {
		return nil, err
	}

	updated := cloneTenant(existing)
	applyTenantUpdateMask(updated, tenant, paths)
	updated = ensureTenantDefaults(updated)
	updated.AuditInfo.UpdatedAt = timestamppb.Now()

	doc := tenantToDocument(updated)
	doc.CreatedAt = existing.GetAuditInfo().GetCreatedAt().AsTime()
	doc.CreatedBy = existing.GetAuditInfo().GetCreatedBy()

	_, err = r.backend.collection(tenantsCollection).UpdateByID(ctx, updated.GetId(), bson.M{"$set": doc})
	if err != nil {
		return nil, fmt.Errorf("update tenant: %w", err)
	}

	if err := r.backend.cacheDelete(ctx, r.backend.cacheKey("tenant", updated.GetId())); err != nil {
		return nil, fmt.Errorf("invalidate tenant cache: %w", err)
	}

	return updated, nil
}

func (r *repository) UpdateTenantStatus(ctx context.Context, tenantID string, status llmv1.ResourceStatus) (*llmv1.Tenant, error) {
	tenant, err := r.GetTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	tenant = ensureTenantDefaults(tenant)
	tenant.Status = status
	tenant.AuditInfo.UpdatedAt = timestamppb.Now()

	doc := tenantToDocument(tenant)
	doc.CreatedAt = tenant.GetAuditInfo().GetCreatedAt().AsTime()
	doc.CreatedBy = tenant.GetAuditInfo().GetCreatedBy()

	_, err = r.backend.collection(tenantsCollection).UpdateByID(ctx, tenantID, bson.M{"$set": doc})
	if err != nil {
		return nil, fmt.Errorf("update tenant status: %w", err)
	}

	if err := r.backend.cacheDelete(ctx, r.backend.cacheKey("tenant", tenantID)); err != nil {
		return nil, fmt.Errorf("invalidate tenant cache: %w", err)
	}

	return tenant, nil
}

func (r *repository) CreateProject(ctx context.Context, project *llmv1.Project) (*llmv1.Project, error) {
	project = ensureProjectDefaults(project)
	now := time.Now().UTC()
	doc := projectDocument{
		ID:          firstNonEmpty(project.GetId(), uuid.NewString()),
		TenantID:    project.GetTenantId(),
		Name:        project.GetName(),
		DisplayName: project.GetDisplayName(),
		Description: project.GetDescription(),
		Status:      normalizeResourceStatus(project.GetStatus()),
		Environment: project.GetEnvironment(),
		Labels:      copyMap(project.GetMetadata().GetLabels()),
		Annotations: copyMap(project.GetMetadata().GetAnnotations()),
		CreatedBy:   project.GetAuditInfo().GetCreatedBy(),
		CreatedAt:   now,
		UpdatedBy:   project.GetAuditInfo().GetUpdatedBy(),
		UpdatedAt:   now,
	}

	if _, err := r.backend.collection(projectsCollection).InsertOne(ctx, doc); err != nil {
		return nil, fmt.Errorf("insert project: %w", err)
	}

	result := projectFromDocument(doc)
	if err := r.backend.cacheSet(ctx, r.backend.cacheKey("project", result.GetId()), result); err != nil {
		return nil, fmt.Errorf("cache project: %w", err)
	}

	return result, nil
}

func (r *repository) GetProject(ctx context.Context, projectID string) (*llmv1.Project, error) {
	cacheKey := r.backend.cacheKey("project", projectID)
	cached := &llmv1.Project{}
	found, err := r.backend.cacheGet(ctx, cacheKey, cached)
	if err != nil {
		return nil, fmt.Errorf("cache get project: %w", err)
	}
	if found {
		return cached, nil
	}

	var doc projectDocument
	if err := r.backend.collection(projectsCollection).FindOne(ctx, bson.M{"_id": projectID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("find project: %w", err)
	}

	result := projectFromDocument(doc)
	if err := r.backend.cacheSet(ctx, cacheKey, result); err != nil {
		return nil, fmt.Errorf("cache project: %w", err)
	}

	return result, nil
}

func (r *repository) ListProjects(ctx context.Context, req *llmv1.ListProjectsRequest) ([]*llmv1.Project, error) {
	filter := bson.M{"tenant_id": req.GetTenantId()}
	if req.GetStatus() != llmv1.ResourceStatus_RESOURCE_STATUS_UNSPECIFIED {
		filter["status"] = int32(req.GetStatus())
	}

	findOptions := options.Find().SetLimit(int64(normalizePageSize(req.GetPage())))
	cursor, err := r.backend.collection(projectsCollection).Find(ctx, filter, findOptions)
	if err != nil {
		return nil, fmt.Errorf("find projects: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []projectDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode projects: %w", err)
	}

	result := make([]*llmv1.Project, 0, len(docs))
	for _, doc := range docs {
		result = append(result, projectFromDocument(doc))
	}

	return result, nil
}

func (r *repository) UpdateProject(ctx context.Context, project *llmv1.Project, paths []string) (*llmv1.Project, error) {
	project = ensureProjectDefaults(project)
	existing, err := r.GetProject(ctx, project.GetId())
	if err != nil {
		return nil, err
	}

	updated := cloneProject(existing)
	applyProjectUpdateMask(updated, project, paths)
	updated = ensureProjectDefaults(updated)
	updated.AuditInfo.UpdatedAt = timestamppb.Now()

	doc := projectToDocument(updated)
	doc.CreatedAt = existing.GetAuditInfo().GetCreatedAt().AsTime()
	doc.CreatedBy = existing.GetAuditInfo().GetCreatedBy()

	_, err = r.backend.collection(projectsCollection).UpdateByID(ctx, updated.GetId(), bson.M{"$set": doc})
	if err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}

	if err := r.backend.cacheDelete(ctx, r.backend.cacheKey("project", updated.GetId())); err != nil {
		return nil, fmt.Errorf("invalidate project cache: %w", err)
	}

	return updated, nil
}

func (r *repository) UpdateProjectStatus(ctx context.Context, projectID string, status llmv1.ResourceStatus) (*llmv1.Project, error) {
	project, err := r.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}

	project = ensureProjectDefaults(project)
	project.Status = status
	project.AuditInfo.UpdatedAt = timestamppb.Now()

	doc := projectToDocument(project)
	doc.CreatedAt = project.GetAuditInfo().GetCreatedAt().AsTime()
	doc.CreatedBy = project.GetAuditInfo().GetCreatedBy()

	_, err = r.backend.collection(projectsCollection).UpdateByID(ctx, projectID, bson.M{"$set": doc})
	if err != nil {
		return nil, fmt.Errorf("update project status: %w", err)
	}

	if err := r.backend.cacheDelete(ctx, r.backend.cacheKey("project", projectID)); err != nil {
		return nil, fmt.Errorf("invalidate project cache: %w", err)
	}

	return project, nil
}

func (r *repository) CreateMembership(ctx context.Context, membership *llmv1.Membership) (*llmv1.Membership, error) {
	membership = ensureMembershipDefaults(membership)
	now := time.Now().UTC()
	doc := membershipDocument{
		ID:            firstNonEmpty(membership.GetId(), uuid.NewString()),
		TenantID:      membership.GetTenantId(),
		ProjectID:     membership.GetProjectId(),
		PrincipalID:   membership.GetPrincipalId(),
		PrincipalType: membership.GetPrincipalType(),
		Roles:         toRoleInts(membership.GetRoles()),
		Status:        normalizeResourceStatus(membership.GetStatus()),
		ExpiresAt:     membership.GetExpiresAt().AsTime(),
		CreatedBy:     membership.GetAuditInfo().GetCreatedBy(),
		CreatedAt:     now,
		UpdatedBy:     membership.GetAuditInfo().GetUpdatedBy(),
		UpdatedAt:     now,
	}

	if _, err := r.backend.collection(membershipsCollection).InsertOne(ctx, doc); err != nil {
		return nil, fmt.Errorf("insert membership: %w", err)
	}

	return membershipFromDocument(doc), nil
}

func (r *repository) ListMemberships(ctx context.Context, req *llmv1.ListMembershipsRequest) ([]*llmv1.Membership, error) {
	filter := bson.M{}
	if req.GetTenantId() != "" {
		filter["tenant_id"] = req.GetTenantId()
	}
	if req.GetProjectId() != "" {
		filter["project_id"] = req.GetProjectId()
	}
	if req.GetPrincipalId() != "" {
		filter["principal_id"] = req.GetPrincipalId()
	}

	findOptions := options.Find().SetLimit(int64(normalizePageSize(req.GetPage())))
	cursor, err := r.backend.collection(membershipsCollection).Find(ctx, filter, findOptions)
	if err != nil {
		return nil, fmt.Errorf("find memberships: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []membershipDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode memberships: %w", err)
	}

	result := make([]*llmv1.Membership, 0, len(docs))
	for _, doc := range docs {
		result = append(result, membershipFromDocument(doc))
	}

	return result, nil
}

func (r *repository) DeleteMembership(ctx context.Context, membershipID string) error {
	result, err := r.backend.collection(membershipsCollection).DeleteOne(ctx, bson.M{"_id": membershipID})
	if err != nil {
		return fmt.Errorf("delete membership: %w", err)
	}
	if result.DeletedCount == 0 {
		return errNotFound
	}

	return nil
}

func (r *repository) CreateGatewayKey(ctx context.Context, gatewayKey *llmv1.GatewayKey) (*llmv1.GatewayKey, *llmv1.GatewayKeySecret, error) {
	gatewayKey = ensureGatewayKeyDefaults(gatewayKey)

	secretValue, secretHash, err := generateSecretMaterial()
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC()
	doc := gatewayKeyDocument{
		ID:            firstNonEmpty(gatewayKey.GetId(), uuid.NewString()),
		TenantID:      gatewayKey.GetTenantId(),
		ProjectID:     gatewayKey.GetProjectId(),
		DisplayName:   gatewayKey.GetDisplayName(),
		SecretHash:    secretHash,
		Status:        normalizeKeyStatus(gatewayKey.GetStatus()),
		AllowedModels: append([]string(nil), gatewayKey.GetAllowedModels()...),
		Tags:          append([]string(nil), gatewayKey.GetTags()...),
		ExpiresAt:     gatewayKey.GetExpiresAt().AsTime(),
		QuotaPolicyID: gatewayKey.GetQuotaPolicyId(),
		SpendPolicyID: gatewayKey.GetSpendPolicyId(),
		LastUsedAt:    gatewayKey.GetLastUsedAt().AsTime(),
		Labels:        copyMap(gatewayKey.GetMetadata().GetLabels()),
		Annotations:   copyMap(gatewayKey.GetMetadata().GetAnnotations()),
		CreatedBy:     gatewayKey.GetAuditInfo().GetCreatedBy(),
		CreatedAt:     now,
		UpdatedBy:     gatewayKey.GetAuditInfo().GetUpdatedBy(),
		UpdatedAt:     now,
	}

	if _, err := r.backend.collection(gatewayKeysCollection).InsertOne(ctx, doc); err != nil {
		return nil, nil, fmt.Errorf("insert gateway key: %w", err)
	}

	result := gatewayKeyFromDocument(doc)
	if err := r.backend.cacheSet(ctx, r.backend.cacheKey("gateway_key", result.GetId()), result); err != nil {
		return nil, nil, fmt.Errorf("cache gateway key: %w", err)
	}
	if err := r.backend.cacheSet(ctx, r.backend.cacheKey("gateway_key_auth", secretHash), result); err != nil {
		return nil, nil, fmt.Errorf("cache gateway key auth lookup: %w", err)
	}

	return result, &llmv1.GatewayKeySecret{
		Id:              result.GetId(),
		PlaintextSecret: secretValue,
		RevealExpiresAt: timestamppb.New(now.Add(10 * time.Minute)),
	}, nil
}

func (r *repository) GetGatewayKey(ctx context.Context, gatewayKeyID string) (*llmv1.GatewayKey, error) {
	cacheKey := r.backend.cacheKey("gateway_key", gatewayKeyID)
	cached := &llmv1.GatewayKey{}
	found, err := r.backend.cacheGet(ctx, cacheKey, cached)
	if err != nil {
		return nil, fmt.Errorf("cache get gateway key: %w", err)
	}
	if found {
		return cached, nil
	}

	var doc gatewayKeyDocument
	if err := r.backend.collection(gatewayKeysCollection).FindOne(ctx, bson.M{"_id": gatewayKeyID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("find gateway key: %w", err)
	}

	result := gatewayKeyFromDocument(doc)
	if err := r.backend.cacheSet(ctx, cacheKey, result); err != nil {
		return nil, fmt.Errorf("cache gateway key: %w", err)
	}

	return result, nil
}

func (r *repository) GetGatewayKeyBySecret(ctx context.Context, plaintextSecret string) (*llmv1.GatewayKey, error) {
	secretHash := hashGatewaySecret(plaintextSecret)
	if secretHash == "" {
		return nil, errNotFound
	}

	cacheKey := r.backend.cacheKey("gateway_key_auth", secretHash)
	cached := &llmv1.GatewayKey{}
	found, err := r.backend.cacheGet(ctx, cacheKey, cached)
	if err != nil {
		return nil, fmt.Errorf("cache get gateway key by secret: %w", err)
	}
	if found {
		return cached, nil
	}

	var doc gatewayKeyDocument
	if err := r.backend.collection(gatewayKeysCollection).FindOne(ctx, bson.M{"secret_hash": secretHash}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("find gateway key by secret: %w", err)
	}

	result := gatewayKeyFromDocument(doc)
	if err := r.backend.cacheSet(ctx, cacheKey, result); err != nil {
		return nil, fmt.Errorf("cache gateway key by secret: %w", err)
	}
	if err := r.backend.cacheSet(ctx, r.backend.cacheKey("gateway_key", result.GetId()), result); err != nil {
		return nil, fmt.Errorf("cache gateway key by id: %w", err)
	}

	return result, nil
}

func (r *repository) ListGatewayKeys(ctx context.Context, req *llmv1.ListGatewayKeysRequest) ([]*llmv1.GatewayKey, error) {
	filter := bson.M{}
	if req.GetTenantId() != "" {
		filter["tenant_id"] = req.GetTenantId()
	}
	if req.GetProjectId() != "" {
		filter["project_id"] = req.GetProjectId()
	}
	if req.GetStatus() != llmv1.KeyStatus_KEY_STATUS_UNSPECIFIED {
		filter["status"] = int32(req.GetStatus())
	}

	findOptions := options.Find().SetLimit(int64(normalizePageSize(req.GetPage())))
	cursor, err := r.backend.collection(gatewayKeysCollection).Find(ctx, filter, findOptions)
	if err != nil {
		return nil, fmt.Errorf("find gateway keys: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []gatewayKeyDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode gateway keys: %w", err)
	}

	result := make([]*llmv1.GatewayKey, 0, len(docs))
	for _, doc := range docs {
		result = append(result, gatewayKeyFromDocument(doc))
	}

	return result, nil
}

func (r *repository) UpdateGatewayKey(ctx context.Context, gatewayKey *llmv1.GatewayKey, paths []string) (*llmv1.GatewayKey, error) {
	gatewayKey = ensureGatewayKeyDefaults(gatewayKey)
	existing, err := r.GetGatewayKey(ctx, gatewayKey.GetId())
	if err != nil {
		return nil, err
	}

	updated := cloneGatewayKey(existing)
	applyGatewayKeyUpdateMask(updated, gatewayKey, paths)
	updated = ensureGatewayKeyDefaults(updated)
	updated.AuditInfo.UpdatedAt = timestamppb.Now()

	doc := gatewayKeyToDocument(updated)
	doc.CreatedAt = existing.GetAuditInfo().GetCreatedAt().AsTime()
	doc.CreatedBy = existing.GetAuditInfo().GetCreatedBy()
	doc.SecretHash = existing.GetSecretHash()

	_, err = r.backend.collection(gatewayKeysCollection).UpdateByID(ctx, updated.GetId(), bson.M{"$set": doc})
	if err != nil {
		return nil, fmt.Errorf("update gateway key: %w", err)
	}

	if err := r.backend.cacheDelete(ctx, r.backend.cacheKey("gateway_key", updated.GetId())); err != nil {
		return nil, fmt.Errorf("invalidate gateway key cache: %w", err)
	}
	if err := r.backend.cacheDelete(ctx, r.backend.cacheKey("gateway_key_auth", existing.GetSecretHash())); err != nil {
		return nil, fmt.Errorf("invalidate gateway key auth cache: %w", err)
	}

	return updated, nil
}

func (r *repository) RotateGatewayKey(ctx context.Context, gatewayKeyID, actorID string) (*llmv1.GatewayKey, *llmv1.GatewayKeySecret, error) {
	existing, err := r.GetGatewayKey(ctx, gatewayKeyID)
	if err != nil {
		return nil, nil, err
	}
	oldSecretHash := existing.GetSecretHash()

	secretValue, secretHash, err := generateSecretMaterial()
	if err != nil {
		return nil, nil, err
	}

	existing = ensureGatewayKeyDefaults(existing)
	existing.SecretHash = secretHash
	existing.Status = llmv1.KeyStatus_KEY_STATUS_ACTIVE
	existing.AuditInfo.UpdatedBy = actorID
	existing.AuditInfo.UpdatedAt = timestamppb.Now()

	doc := gatewayKeyToDocument(existing)
	doc.CreatedAt = existing.GetAuditInfo().GetCreatedAt().AsTime()
	doc.CreatedBy = existing.GetAuditInfo().GetCreatedBy()

	_, err = r.backend.collection(gatewayKeysCollection).UpdateByID(ctx, gatewayKeyID, bson.M{"$set": doc})
	if err != nil {
		return nil, nil, fmt.Errorf("rotate gateway key: %w", err)
	}

	if err := r.backend.cacheDelete(ctx, r.backend.cacheKey("gateway_key", gatewayKeyID)); err != nil {
		return nil, nil, fmt.Errorf("invalidate gateway key cache: %w", err)
	}
	if err := r.backend.cacheDelete(ctx, r.backend.cacheKey("gateway_key_auth", oldSecretHash)); err != nil {
		return nil, nil, fmt.Errorf("invalidate old gateway key auth cache: %w", err)
	}

	return existing, &llmv1.GatewayKeySecret{
		Id:              existing.GetId(),
		PlaintextSecret: secretValue,
		RevealExpiresAt: timestamppb.New(time.Now().UTC().Add(10 * time.Minute)),
	}, nil
}

func (r *repository) RevokeGatewayKey(ctx context.Context, gatewayKeyID, actorID string) (*llmv1.GatewayKey, error) {
	existing, err := r.GetGatewayKey(ctx, gatewayKeyID)
	if err != nil {
		return nil, err
	}

	existing = ensureGatewayKeyDefaults(existing)
	existing.Status = llmv1.KeyStatus_KEY_STATUS_REVOKED
	existing.AuditInfo.UpdatedBy = actorID
	existing.AuditInfo.UpdatedAt = timestamppb.Now()

	doc := gatewayKeyToDocument(existing)
	doc.CreatedAt = existing.GetAuditInfo().GetCreatedAt().AsTime()
	doc.CreatedBy = existing.GetAuditInfo().GetCreatedBy()

	_, err = r.backend.collection(gatewayKeysCollection).UpdateByID(ctx, gatewayKeyID, bson.M{"$set": doc})
	if err != nil {
		return nil, fmt.Errorf("revoke gateway key: %w", err)
	}

	if err := r.backend.cacheDelete(ctx, r.backend.cacheKey("gateway_key", gatewayKeyID)); err != nil {
		return nil, fmt.Errorf("invalidate gateway key cache: %w", err)
	}
	if err := r.backend.cacheDelete(ctx, r.backend.cacheKey("gateway_key_auth", existing.GetSecretHash())); err != nil {
		return nil, fmt.Errorf("invalidate gateway key auth cache: %w", err)
	}

	return existing, nil
}

func (r *repository) CreateUpstreamCredential(ctx context.Context, credential *llmv1.UpstreamCredential) (*llmv1.UpstreamCredential, error) {
	credential = ensureUpstreamCredentialDefaults(credential)
	now := time.Now().UTC()
	doc := upstreamCredentialDocument{
		ID:            firstNonEmpty(credential.GetId(), uuid.NewString()),
		VendorType:    normalizeVendorType(credential.GetVendorType()),
		DisplayName:   credential.GetDisplayName(),
		Status:        normalizeCredentialStatus(credential.GetStatus()),
		SecretRef:     firstNonEmpty(credential.GetSecretRef(), "secret://generated/"+uuid.NewString()),
		Scopes:        append([]string(nil), credential.GetScopes()...),
		LastRotatedAt: credential.GetLastRotatedAt().AsTime(),
		ExpiresAt:     credential.GetExpiresAt().AsTime(),
		Labels:        copyMap(credential.GetMetadata().GetLabels()),
		Annotations:   copyMap(credential.GetMetadata().GetAnnotations()),
		CreatedBy:     credential.GetAuditInfo().GetCreatedBy(),
		CreatedAt:     now,
		UpdatedBy:     credential.GetAuditInfo().GetUpdatedBy(),
		UpdatedAt:     now,
	}

	if doc.LastRotatedAt.IsZero() {
		doc.LastRotatedAt = now
	}

	if _, err := r.backend.collection(upstreamCredsCollection).InsertOne(ctx, doc); err != nil {
		return nil, fmt.Errorf("insert upstream credential: %w", err)
	}

	result := upstreamCredentialFromDocument(doc)
	if err := r.backend.cacheSet(ctx, r.backend.cacheKey("upstream_credential", result.GetId()), result); err != nil {
		return nil, fmt.Errorf("cache upstream credential: %w", err)
	}

	return result, nil
}

func (r *repository) GetUpstreamCredential(ctx context.Context, credentialID string) (*llmv1.UpstreamCredential, error) {
	cacheKey := r.backend.cacheKey("upstream_credential", credentialID)
	cached := &llmv1.UpstreamCredential{}
	found, err := r.backend.cacheGet(ctx, cacheKey, cached)
	if err != nil {
		return nil, fmt.Errorf("cache get upstream credential: %w", err)
	}
	if found {
		return cached, nil
	}

	var doc upstreamCredentialDocument
	if err := r.backend.collection(upstreamCredsCollection).FindOne(ctx, bson.M{"_id": credentialID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("find upstream credential: %w", err)
	}

	result := upstreamCredentialFromDocument(doc)
	if err := r.backend.cacheSet(ctx, cacheKey, result); err != nil {
		return nil, fmt.Errorf("cache upstream credential: %w", err)
	}

	return result, nil
}

func (r *repository) ListUpstreamCredentials(ctx context.Context, req *llmv1.ListUpstreamCredentialsRequest) ([]*llmv1.UpstreamCredential, error) {
	filter := bson.M{}
	if req.GetVendorType() != llmv1.VendorType_VENDOR_TYPE_UNSPECIFIED {
		filter["vendor_type"] = int32(req.GetVendorType())
	}
	if req.GetStatus() != llmv1.CredentialStatus_CREDENTIAL_STATUS_UNSPECIFIED {
		filter["status"] = int32(req.GetStatus())
	}

	findOptions := options.Find().SetLimit(int64(normalizePageSize(req.GetPage())))
	cursor, err := r.backend.collection(upstreamCredsCollection).Find(ctx, filter, findOptions)
	if err != nil {
		return nil, fmt.Errorf("find upstream credentials: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []upstreamCredentialDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode upstream credentials: %w", err)
	}

	result := make([]*llmv1.UpstreamCredential, 0, len(docs))
	for _, doc := range docs {
		result = append(result, upstreamCredentialFromDocument(doc))
	}

	return result, nil
}

func (r *repository) UpdateUpstreamCredential(ctx context.Context, credential *llmv1.UpstreamCredential, paths []string) (*llmv1.UpstreamCredential, error) {
	credential = ensureUpstreamCredentialDefaults(credential)
	existing, err := r.GetUpstreamCredential(ctx, credential.GetId())
	if err != nil {
		return nil, err
	}

	updated := cloneUpstreamCredential(existing)
	applyUpstreamCredentialUpdateMask(updated, credential, paths)
	updated = ensureUpstreamCredentialDefaults(updated)
	updated.AuditInfo.UpdatedAt = timestamppb.Now()

	doc := upstreamCredentialToDocument(updated)
	doc.CreatedAt = existing.GetAuditInfo().GetCreatedAt().AsTime()
	doc.CreatedBy = existing.GetAuditInfo().GetCreatedBy()

	_, err = r.backend.collection(upstreamCredsCollection).UpdateByID(ctx, updated.GetId(), bson.M{"$set": doc})
	if err != nil {
		return nil, fmt.Errorf("update upstream credential: %w", err)
	}

	if err := r.backend.cacheDelete(ctx, r.backend.cacheKey("upstream_credential", updated.GetId())); err != nil {
		return nil, fmt.Errorf("invalidate upstream credential cache: %w", err)
	}

	return updated, nil
}

func (r *repository) RotateUpstreamCredential(ctx context.Context, credentialID, actorID string) (*llmv1.UpstreamCredential, error) {
	existing, err := r.GetUpstreamCredential(ctx, credentialID)
	if err != nil {
		return nil, err
	}

	existing = ensureUpstreamCredentialDefaults(existing)
	existing.Status = llmv1.CredentialStatus_CREDENTIAL_STATUS_ACTIVE
	existing.SecretRef = "secret://rotated/" + uuid.NewString()
	existing.LastRotatedAt = timestamppb.Now()
	existing.AuditInfo.UpdatedBy = actorID
	existing.AuditInfo.UpdatedAt = timestamppb.Now()

	doc := upstreamCredentialToDocument(existing)
	doc.CreatedAt = existing.GetAuditInfo().GetCreatedAt().AsTime()
	doc.CreatedBy = existing.GetAuditInfo().GetCreatedBy()

	_, err = r.backend.collection(upstreamCredsCollection).UpdateByID(ctx, credentialID, bson.M{"$set": doc})
	if err != nil {
		return nil, fmt.Errorf("rotate upstream credential: %w", err)
	}

	if err := r.backend.cacheDelete(ctx, r.backend.cacheKey("upstream_credential", credentialID)); err != nil {
		return nil, fmt.Errorf("invalidate upstream credential cache: %w", err)
	}

	return existing, nil
}

func (r *repository) CreateVendor(ctx context.Context, vendor *llmv1.Vendor) (*llmv1.Vendor, error) {
	vendor = ensureVendorDefaults(vendor)
	now := time.Now().UTC()
	doc := vendorDocument{
		ID:                    firstNonEmpty(vendor.GetId(), uuid.NewString()),
		Name:                  vendor.GetName(),
		DisplayName:           vendor.GetDisplayName(),
		VendorType:            normalizeVendorType(vendor.GetVendorType()),
		Endpoint:              vendor.GetEndpoint(),
		UpstreamCredentialID:  vendor.GetUpstreamCredentialId(),
		Status:                normalizeResourceStatus(vendor.GetStatus()),
		SupportedCapabilities: append([]string(nil), vendor.GetSupportedCapabilities()...),
		TimeoutSeconds:        int64(vendor.GetTimeout().AsDuration() / time.Second),
		Labels:                copyMap(vendor.GetMetadata().GetLabels()),
		Annotations:           copyMap(vendor.GetMetadata().GetAnnotations()),
		CreatedBy:             vendor.GetAuditInfo().GetCreatedBy(),
		CreatedAt:             now,
		UpdatedBy:             vendor.GetAuditInfo().GetUpdatedBy(),
		UpdatedAt:             now,
	}

	if _, err := r.backend.collection(vendorsCollection).InsertOne(ctx, doc); err != nil {
		return nil, fmt.Errorf("insert vendor: %w", err)
	}

	result := vendorFromDocument(doc)
	if err := r.backend.cacheSet(ctx, r.backend.cacheKey("vendor", result.GetId()), result); err != nil {
		return nil, fmt.Errorf("cache vendor: %w", err)
	}

	return result, nil
}

func (r *repository) GetVendor(ctx context.Context, vendorID string) (*llmv1.Vendor, error) {
	cacheKey := r.backend.cacheKey("vendor", vendorID)
	cached := &llmv1.Vendor{}
	found, err := r.backend.cacheGet(ctx, cacheKey, cached)
	if err != nil {
		return nil, fmt.Errorf("cache get vendor: %w", err)
	}
	if found {
		return cached, nil
	}

	var doc vendorDocument
	if err := r.backend.collection(vendorsCollection).FindOne(ctx, bson.M{"_id": vendorID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("find vendor: %w", err)
	}

	result := vendorFromDocument(doc)
	if err := r.backend.cacheSet(ctx, cacheKey, result); err != nil {
		return nil, fmt.Errorf("cache vendor: %w", err)
	}

	return result, nil
}

func (r *repository) ListVendors(ctx context.Context, req *llmv1.ListVendorsRequest) ([]*llmv1.Vendor, error) {
	filter := bson.M{}
	if req.GetVendorType() != llmv1.VendorType_VENDOR_TYPE_UNSPECIFIED {
		filter["vendor_type"] = int32(req.GetVendorType())
	}
	if req.GetStatus() != llmv1.ResourceStatus_RESOURCE_STATUS_UNSPECIFIED {
		filter["status"] = int32(req.GetStatus())
	}

	findOptions := options.Find().SetLimit(int64(normalizePageSize(req.GetPage())))
	cursor, err := r.backend.collection(vendorsCollection).Find(ctx, filter, findOptions)
	if err != nil {
		return nil, fmt.Errorf("find vendors: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []vendorDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode vendors: %w", err)
	}

	result := make([]*llmv1.Vendor, 0, len(docs))
	for _, doc := range docs {
		result = append(result, vendorFromDocument(doc))
	}

	return result, nil
}

func (r *repository) UpdateVendor(ctx context.Context, vendor *llmv1.Vendor, paths []string) (*llmv1.Vendor, error) {
	vendor = ensureVendorDefaults(vendor)
	existing, err := r.GetVendor(ctx, vendor.GetId())
	if err != nil {
		return nil, err
	}

	updated := cloneVendor(existing)
	applyVendorUpdateMask(updated, vendor, paths)
	updated = ensureVendorDefaults(updated)
	updated.AuditInfo.UpdatedAt = timestamppb.Now()

	doc := vendorToDocument(updated)
	doc.CreatedAt = existing.GetAuditInfo().GetCreatedAt().AsTime()
	doc.CreatedBy = existing.GetAuditInfo().GetCreatedBy()

	_, err = r.backend.collection(vendorsCollection).UpdateByID(ctx, updated.GetId(), bson.M{"$set": doc})
	if err != nil {
		return nil, fmt.Errorf("update vendor: %w", err)
	}

	if err := r.backend.cacheDelete(ctx, r.backend.cacheKey("vendor", updated.GetId())); err != nil {
		return nil, fmt.Errorf("invalidate vendor cache: %w", err)
	}

	return updated, nil
}

func (r *repository) CreateLogicalModel(ctx context.Context, model *llmv1.LogicalModel) (*llmv1.LogicalModel, error) {
	model = ensureLogicalModelDefaults(model)
	now := time.Now().UTC()
	doc := logicalModelDocument{
		ID:                       firstNonEmpty(model.GetId(), uuid.NewString()),
		Name:                     model.GetName(),
		DisplayName:              model.GetDisplayName(),
		Description:              model.GetDescription(),
		Visibility:               int32(normalizeModelVisibility(model.GetVisibility())),
		Status:                   normalizeResourceStatus(model.GetStatus()),
		Capabilities:             append([]string(nil), model.GetCapabilities()...),
		Targets:                  vendorTargetsToDocuments(model.GetTargets()),
		DefaultPricingSnapshotID: model.GetDefaultPricingSnapshotId(),
		AllowedTenantIDs:         append([]string(nil), model.GetAllowedTenantIds()...),
		Labels:                   copyMap(model.GetMetadata().GetLabels()),
		Annotations:              copyMap(model.GetMetadata().GetAnnotations()),
		CreatedBy:                model.GetAuditInfo().GetCreatedBy(),
		CreatedAt:                now,
		UpdatedBy:                model.GetAuditInfo().GetUpdatedBy(),
		UpdatedAt:                now,
	}

	if _, err := r.backend.collection(logicalModelsCollection).InsertOne(ctx, doc); err != nil {
		return nil, fmt.Errorf("insert logical model: %w", err)
	}

	result := logicalModelFromDocument(doc)
	if err := r.backend.cacheSet(ctx, r.backend.cacheKey("logical_model", result.GetId()), result); err != nil {
		return nil, fmt.Errorf("cache logical model: %w", err)
	}

	return result, nil
}

func (r *repository) GetLogicalModel(ctx context.Context, logicalModelID string) (*llmv1.LogicalModel, error) {
	cacheKey := r.backend.cacheKey("logical_model", logicalModelID)
	cached := &llmv1.LogicalModel{}
	found, err := r.backend.cacheGet(ctx, cacheKey, cached)
	if err != nil {
		return nil, fmt.Errorf("cache get logical model: %w", err)
	}
	if found {
		return cached, nil
	}

	var doc logicalModelDocument
	if err := r.backend.collection(logicalModelsCollection).FindOne(ctx, bson.M{"_id": logicalModelID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("find logical model: %w", err)
	}

	result := logicalModelFromDocument(doc)
	if err := r.backend.cacheSet(ctx, cacheKey, result); err != nil {
		return nil, fmt.Errorf("cache logical model: %w", err)
	}

	return result, nil
}

func (r *repository) ListLogicalModels(ctx context.Context, req *llmv1.ListLogicalModelsRequest) ([]*llmv1.LogicalModel, error) {
	filter := bson.M{}
	if req.GetVisibility() != llmv1.ModelVisibility_MODEL_VISIBILITY_UNSPECIFIED {
		filter["visibility"] = int32(req.GetVisibility())
	}
	if req.GetStatus() != llmv1.ResourceStatus_RESOURCE_STATUS_UNSPECIFIED {
		filter["status"] = int32(req.GetStatus())
	}
	if req.GetTenantId() != "" {
		filter["$or"] = []bson.M{
			{"visibility": int32(llmv1.ModelVisibility_MODEL_VISIBILITY_PUBLIC)},
			{"allowed_tenant_ids": req.GetTenantId()},
		}
	}

	findOptions := options.Find().SetLimit(int64(normalizePageSize(req.GetPage())))
	cursor, err := r.backend.collection(logicalModelsCollection).Find(ctx, filter, findOptions)
	if err != nil {
		return nil, fmt.Errorf("find logical models: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []logicalModelDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode logical models: %w", err)
	}

	result := make([]*llmv1.LogicalModel, 0, len(docs))
	for _, doc := range docs {
		result = append(result, logicalModelFromDocument(doc))
	}

	return result, nil
}

func (r *repository) UpdateLogicalModel(ctx context.Context, model *llmv1.LogicalModel, paths []string) (*llmv1.LogicalModel, error) {
	model = ensureLogicalModelDefaults(model)
	existing, err := r.GetLogicalModel(ctx, model.GetId())
	if err != nil {
		return nil, err
	}

	updated := cloneLogicalModel(existing)
	applyLogicalModelUpdateMask(updated, model, paths)
	updated = ensureLogicalModelDefaults(updated)
	updated.AuditInfo.UpdatedAt = timestamppb.Now()

	doc := logicalModelToDocument(updated)
	doc.CreatedAt = existing.GetAuditInfo().GetCreatedAt().AsTime()
	doc.CreatedBy = existing.GetAuditInfo().GetCreatedBy()

	_, err = r.backend.collection(logicalModelsCollection).UpdateByID(ctx, updated.GetId(), bson.M{"$set": doc})
	if err != nil {
		return nil, fmt.Errorf("update logical model: %w", err)
	}

	if err := r.backend.cacheDelete(ctx, r.backend.cacheKey("logical_model", updated.GetId())); err != nil {
		return nil, fmt.Errorf("invalidate logical model cache: %w", err)
	}

	return updated, nil
}

func (r *repository) CreateRoutingPolicy(ctx context.Context, policy *llmv1.RoutingPolicy) (*llmv1.RoutingPolicy, error) {
	policy = ensureRoutingPolicyDefaults(policy)
	now := time.Now().UTC()
	doc := routingPolicyDocument{
		ID:                  firstNonEmpty(policy.GetId(), uuid.NewString()),
		Name:                policy.GetName(),
		TenantID:            policy.GetTenantId(),
		ProjectID:           policy.GetProjectId(),
		LogicalModelID:      policy.GetLogicalModelId(),
		Strategy:            int32(normalizeRoutingStrategy(policy.GetStrategy())),
		Targets:             vendorTargetsToDocuments(policy.GetTargets()),
		Status:              normalizeResourceStatus(policy.GetStatus()),
		FailoverEnabled:     policy.GetFailoverEnabled(),
		HealthCheckRequired: policy.GetHealthCheckRequired(),
		Labels:              copyMap(policy.GetMetadata().GetLabels()),
		Annotations:         copyMap(policy.GetMetadata().GetAnnotations()),
		CreatedBy:           policy.GetAuditInfo().GetCreatedBy(),
		CreatedAt:           now,
		UpdatedBy:           policy.GetAuditInfo().GetUpdatedBy(),
		UpdatedAt:           now,
	}

	if _, err := r.backend.collection(routingPoliciesCollection).InsertOne(ctx, doc); err != nil {
		return nil, fmt.Errorf("insert routing policy: %w", err)
	}

	result := routingPolicyFromDocument(doc)
	if err := r.backend.cacheSet(ctx, r.backend.cacheKey("routing_policy", result.GetId()), result); err != nil {
		return nil, fmt.Errorf("cache routing policy: %w", err)
	}

	return result, nil
}

func (r *repository) GetRoutingPolicy(ctx context.Context, routingPolicyID string) (*llmv1.RoutingPolicy, error) {
	cacheKey := r.backend.cacheKey("routing_policy", routingPolicyID)
	cached := &llmv1.RoutingPolicy{}
	found, err := r.backend.cacheGet(ctx, cacheKey, cached)
	if err != nil {
		return nil, fmt.Errorf("cache get routing policy: %w", err)
	}
	if found {
		return cached, nil
	}

	var doc routingPolicyDocument
	if err := r.backend.collection(routingPoliciesCollection).FindOne(ctx, bson.M{"_id": routingPolicyID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("find routing policy: %w", err)
	}

	result := routingPolicyFromDocument(doc)
	if err := r.backend.cacheSet(ctx, cacheKey, result); err != nil {
		return nil, fmt.Errorf("cache routing policy: %w", err)
	}

	return result, nil
}

func (r *repository) ListRoutingPolicies(ctx context.Context, req *llmv1.ListRoutingPoliciesRequest) ([]*llmv1.RoutingPolicy, error) {
	filter := bson.M{}
	if req.GetTenantId() != "" {
		filter["tenant_id"] = req.GetTenantId()
	}
	if req.GetProjectId() != "" {
		filter["project_id"] = req.GetProjectId()
	}
	if req.GetLogicalModelId() != "" {
		filter["logical_model_id"] = req.GetLogicalModelId()
	}
	if req.GetStatus() != llmv1.ResourceStatus_RESOURCE_STATUS_UNSPECIFIED {
		filter["status"] = int32(req.GetStatus())
	}

	findOptions := options.Find().SetLimit(int64(normalizePageSize(req.GetPage())))
	cursor, err := r.backend.collection(routingPoliciesCollection).Find(ctx, filter, findOptions)
	if err != nil {
		return nil, fmt.Errorf("find routing policies: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []routingPolicyDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode routing policies: %w", err)
	}

	result := make([]*llmv1.RoutingPolicy, 0, len(docs))
	for _, doc := range docs {
		result = append(result, routingPolicyFromDocument(doc))
	}

	return result, nil
}

func (r *repository) UpdateRoutingPolicy(ctx context.Context, policy *llmv1.RoutingPolicy, paths []string) (*llmv1.RoutingPolicy, error) {
	policy = ensureRoutingPolicyDefaults(policy)
	existing, err := r.GetRoutingPolicy(ctx, policy.GetId())
	if err != nil {
		return nil, err
	}

	updated := cloneRoutingPolicy(existing)
	applyRoutingPolicyUpdateMask(updated, policy, paths)
	updated = ensureRoutingPolicyDefaults(updated)
	updated.AuditInfo.UpdatedAt = timestamppb.Now()

	doc := routingPolicyToDocument(updated)
	doc.CreatedAt = existing.GetAuditInfo().GetCreatedAt().AsTime()
	doc.CreatedBy = existing.GetAuditInfo().GetCreatedBy()

	_, err = r.backend.collection(routingPoliciesCollection).UpdateByID(ctx, updated.GetId(), bson.M{"$set": doc})
	if err != nil {
		return nil, fmt.Errorf("update routing policy: %w", err)
	}

	if err := r.backend.cacheDelete(ctx, r.backend.cacheKey("routing_policy", updated.GetId())); err != nil {
		return nil, fmt.Errorf("invalidate routing policy cache: %w", err)
	}

	return updated, nil
}

func (r *repository) CreateQuotaPolicy(ctx context.Context, policy *llmv1.QuotaPolicy) (*llmv1.QuotaPolicy, error) {
	policy = ensureQuotaPolicyDefaults(policy)
	now := time.Now().UTC()
	doc := quotaPolicyDocument{
		ID:               firstNonEmpty(policy.GetId(), uuid.NewString()),
		Name:             policy.GetName(),
		TenantID:         policy.GetTenantId(),
		ProjectID:        policy.GetProjectId(),
		GatewayKeyID:     policy.GetGatewayKeyId(),
		Period:           int32(normalizeQuotaPeriod(policy.GetPeriod())),
		RequestLimit:     policy.GetRequestLimit(),
		TokenLimit:       policy.GetTokenLimit(),
		ConcurrencyLimit: policy.GetConcurrencyLimit(),
		Status:           normalizeResourceStatus(policy.GetStatus()),
		Labels:           copyMap(policy.GetMetadata().GetLabels()),
		Annotations:      copyMap(policy.GetMetadata().GetAnnotations()),
		CreatedBy:        policy.GetAuditInfo().GetCreatedBy(),
		CreatedAt:        now,
		UpdatedBy:        policy.GetAuditInfo().GetUpdatedBy(),
		UpdatedAt:        now,
	}

	if _, err := r.backend.collection(quotaPoliciesCollection).InsertOne(ctx, doc); err != nil {
		return nil, fmt.Errorf("insert quota policy: %w", err)
	}

	result := quotaPolicyFromDocument(doc)
	if err := r.backend.cacheSet(ctx, r.backend.cacheKey("quota_policy", result.GetId()), result); err != nil {
		return nil, fmt.Errorf("cache quota policy: %w", err)
	}

	return result, nil
}

func (r *repository) GetQuotaPolicy(ctx context.Context, quotaPolicyID string) (*llmv1.QuotaPolicy, error) {
	cacheKey := r.backend.cacheKey("quota_policy", quotaPolicyID)
	cached := &llmv1.QuotaPolicy{}
	found, err := r.backend.cacheGet(ctx, cacheKey, cached)
	if err != nil {
		return nil, fmt.Errorf("cache get quota policy: %w", err)
	}
	if found {
		return cached, nil
	}

	var doc quotaPolicyDocument
	if err := r.backend.collection(quotaPoliciesCollection).FindOne(ctx, bson.M{"_id": quotaPolicyID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("find quota policy: %w", err)
	}

	result := quotaPolicyFromDocument(doc)
	if err := r.backend.cacheSet(ctx, cacheKey, result); err != nil {
		return nil, fmt.Errorf("cache quota policy: %w", err)
	}

	return result, nil
}

func (r *repository) ListQuotaPolicies(ctx context.Context, req *llmv1.ListQuotaPoliciesRequest) ([]*llmv1.QuotaPolicy, error) {
	filter := bson.M{}
	if req.GetTenantId() != "" {
		filter["tenant_id"] = req.GetTenantId()
	}
	if req.GetProjectId() != "" {
		filter["project_id"] = req.GetProjectId()
	}
	if req.GetGatewayKeyId() != "" {
		filter["gateway_key_id"] = req.GetGatewayKeyId()
	}
	if req.GetStatus() != llmv1.ResourceStatus_RESOURCE_STATUS_UNSPECIFIED {
		filter["status"] = int32(req.GetStatus())
	}

	findOptions := options.Find().SetLimit(int64(normalizePageSize(req.GetPage())))
	cursor, err := r.backend.collection(quotaPoliciesCollection).Find(ctx, filter, findOptions)
	if err != nil {
		return nil, fmt.Errorf("find quota policies: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []quotaPolicyDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode quota policies: %w", err)
	}

	result := make([]*llmv1.QuotaPolicy, 0, len(docs))
	for _, doc := range docs {
		result = append(result, quotaPolicyFromDocument(doc))
	}

	return result, nil
}

func (r *repository) UpdateQuotaPolicy(ctx context.Context, policy *llmv1.QuotaPolicy, paths []string) (*llmv1.QuotaPolicy, error) {
	policy = ensureQuotaPolicyDefaults(policy)
	existing, err := r.GetQuotaPolicy(ctx, policy.GetId())
	if err != nil {
		return nil, err
	}

	updated := cloneQuotaPolicy(existing)
	applyQuotaPolicyUpdateMask(updated, policy, paths)
	updated = ensureQuotaPolicyDefaults(updated)
	updated.AuditInfo.UpdatedAt = timestamppb.Now()

	doc := quotaPolicyToDocument(updated)
	doc.CreatedAt = existing.GetAuditInfo().GetCreatedAt().AsTime()
	doc.CreatedBy = existing.GetAuditInfo().GetCreatedBy()

	_, err = r.backend.collection(quotaPoliciesCollection).UpdateByID(ctx, updated.GetId(), bson.M{"$set": doc})
	if err != nil {
		return nil, fmt.Errorf("update quota policy: %w", err)
	}

	if err := r.backend.cacheDelete(ctx, r.backend.cacheKey("quota_policy", updated.GetId())); err != nil {
		return nil, fmt.Errorf("invalidate quota policy cache: %w", err)
	}

	return updated, nil
}

func (r *repository) CreateSpendPolicy(ctx context.Context, policy *llmv1.SpendPolicy) (*llmv1.SpendPolicy, error) {
	policy = ensureSpendPolicyDefaults(policy)
	now := time.Now().UTC()
	doc := spendPolicyDocument{
		ID:           firstNonEmpty(policy.GetId(), uuid.NewString()),
		Name:         policy.GetName(),
		TenantID:     policy.GetTenantId(),
		ProjectID:    policy.GetProjectId(),
		GatewayKeyID: policy.GetGatewayKeyId(),
		SpendUnits:   policy.GetSpendCap().GetUnits(),
		CurrencyCode: firstNonEmpty(policy.GetSpendCap().GetCurrencyCode(), "USD"),
		Period:       int32(normalizeQuotaPeriod(policy.GetPeriod())),
		Status:       normalizeResourceStatus(policy.GetStatus()),
		Labels:       copyMap(policy.GetMetadata().GetLabels()),
		Annotations:  copyMap(policy.GetMetadata().GetAnnotations()),
		CreatedBy:    policy.GetAuditInfo().GetCreatedBy(),
		CreatedAt:    now,
		UpdatedBy:    policy.GetAuditInfo().GetUpdatedBy(),
		UpdatedAt:    now,
	}

	if _, err := r.backend.collection(spendPoliciesCollection).InsertOne(ctx, doc); err != nil {
		return nil, fmt.Errorf("insert spend policy: %w", err)
	}

	result := spendPolicyFromDocument(doc)
	if err := r.backend.cacheSet(ctx, r.backend.cacheKey("spend_policy", result.GetId()), result); err != nil {
		return nil, fmt.Errorf("cache spend policy: %w", err)
	}

	return result, nil
}

func (r *repository) GetSpendPolicy(ctx context.Context, spendPolicyID string) (*llmv1.SpendPolicy, error) {
	cacheKey := r.backend.cacheKey("spend_policy", spendPolicyID)
	cached := &llmv1.SpendPolicy{}
	found, err := r.backend.cacheGet(ctx, cacheKey, cached)
	if err != nil {
		return nil, fmt.Errorf("cache get spend policy: %w", err)
	}
	if found {
		return cached, nil
	}

	var doc spendPolicyDocument
	if err := r.backend.collection(spendPoliciesCollection).FindOne(ctx, bson.M{"_id": spendPolicyID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("find spend policy: %w", err)
	}

	result := spendPolicyFromDocument(doc)
	if err := r.backend.cacheSet(ctx, cacheKey, result); err != nil {
		return nil, fmt.Errorf("cache spend policy: %w", err)
	}

	return result, nil
}

func (r *repository) ListSpendPolicies(ctx context.Context, req *llmv1.ListSpendPoliciesRequest) ([]*llmv1.SpendPolicy, error) {
	filter := bson.M{}
	if req.GetTenantId() != "" {
		filter["tenant_id"] = req.GetTenantId()
	}
	if req.GetProjectId() != "" {
		filter["project_id"] = req.GetProjectId()
	}
	if req.GetGatewayKeyId() != "" {
		filter["gateway_key_id"] = req.GetGatewayKeyId()
	}
	if req.GetStatus() != llmv1.ResourceStatus_RESOURCE_STATUS_UNSPECIFIED {
		filter["status"] = int32(req.GetStatus())
	}

	findOptions := options.Find().SetLimit(int64(normalizePageSize(req.GetPage())))
	cursor, err := r.backend.collection(spendPoliciesCollection).Find(ctx, filter, findOptions)
	if err != nil {
		return nil, fmt.Errorf("find spend policies: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []spendPolicyDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode spend policies: %w", err)
	}

	result := make([]*llmv1.SpendPolicy, 0, len(docs))
	for _, doc := range docs {
		result = append(result, spendPolicyFromDocument(doc))
	}

	return result, nil
}

func (r *repository) UpdateSpendPolicy(ctx context.Context, policy *llmv1.SpendPolicy, paths []string) (*llmv1.SpendPolicy, error) {
	policy = ensureSpendPolicyDefaults(policy)
	existing, err := r.GetSpendPolicy(ctx, policy.GetId())
	if err != nil {
		return nil, err
	}

	updated := cloneSpendPolicy(existing)
	applySpendPolicyUpdateMask(updated, policy, paths)
	updated = ensureSpendPolicyDefaults(updated)
	updated.AuditInfo.UpdatedAt = timestamppb.Now()

	doc := spendPolicyToDocument(updated)
	doc.CreatedAt = existing.GetAuditInfo().GetCreatedAt().AsTime()
	doc.CreatedBy = existing.GetAuditInfo().GetCreatedBy()

	_, err = r.backend.collection(spendPoliciesCollection).UpdateByID(ctx, updated.GetId(), bson.M{"$set": doc})
	if err != nil {
		return nil, fmt.Errorf("update spend policy: %w", err)
	}

	if err := r.backend.cacheDelete(ctx, r.backend.cacheKey("spend_policy", updated.GetId())); err != nil {
		return nil, fmt.Errorf("invalidate spend policy cache: %w", err)
	}

	return updated, nil
}

func tenantFromDocument(doc tenantDocument) *llmv1.Tenant {
	return &llmv1.Tenant{
		Id:               doc.ID,
		Name:             doc.Name,
		DisplayName:      doc.DisplayName,
		Status:           llmv1.ResourceStatus(doc.Status),
		BillingMode:      llmv1.BillingMode(doc.BillingMode),
		BillingProfileId: doc.BillingProfileID,
		Metadata: &llmv1.Metadata{
			Labels:      copyMap(doc.Labels),
			Annotations: copyMap(doc.Annotations),
		},
		AuditInfo: &llmv1.AuditInfo{
			CreatedBy: doc.CreatedBy,
			CreatedAt: timestamppb.New(doc.CreatedAt),
			UpdatedBy: doc.UpdatedBy,
			UpdatedAt: timestamppb.New(doc.UpdatedAt),
		},
	}
}

func tenantToDocument(tenant *llmv1.Tenant) tenantDocument {
	tenant = ensureTenantDefaults(tenant)
	return tenantDocument{
		ID:               tenant.GetId(),
		Name:             tenant.GetName(),
		DisplayName:      tenant.GetDisplayName(),
		Status:           int32(tenant.GetStatus()),
		BillingMode:      int32(tenant.GetBillingMode()),
		BillingProfileID: tenant.GetBillingProfileId(),
		Labels:           copyMap(tenant.GetMetadata().GetLabels()),
		Annotations:      copyMap(tenant.GetMetadata().GetAnnotations()),
		CreatedBy:        tenant.GetAuditInfo().GetCreatedBy(),
		CreatedAt:        tenant.GetAuditInfo().GetCreatedAt().AsTime(),
		UpdatedBy:        tenant.GetAuditInfo().GetUpdatedBy(),
		UpdatedAt:        tenant.GetAuditInfo().GetUpdatedAt().AsTime(),
	}
}

func projectFromDocument(doc projectDocument) *llmv1.Project {
	return &llmv1.Project{
		Id:          doc.ID,
		TenantId:    doc.TenantID,
		Name:        doc.Name,
		DisplayName: doc.DisplayName,
		Description: doc.Description,
		Status:      llmv1.ResourceStatus(doc.Status),
		Environment: doc.Environment,
		Metadata: &llmv1.Metadata{
			Labels:      copyMap(doc.Labels),
			Annotations: copyMap(doc.Annotations),
		},
		AuditInfo: &llmv1.AuditInfo{
			CreatedBy: doc.CreatedBy,
			CreatedAt: timestamppb.New(doc.CreatedAt),
			UpdatedBy: doc.UpdatedBy,
			UpdatedAt: timestamppb.New(doc.UpdatedAt),
		},
	}
}

func projectToDocument(project *llmv1.Project) projectDocument {
	project = ensureProjectDefaults(project)
	return projectDocument{
		ID:          project.GetId(),
		TenantID:    project.GetTenantId(),
		Name:        project.GetName(),
		DisplayName: project.GetDisplayName(),
		Description: project.GetDescription(),
		Status:      int32(project.GetStatus()),
		Environment: project.GetEnvironment(),
		Labels:      copyMap(project.GetMetadata().GetLabels()),
		Annotations: copyMap(project.GetMetadata().GetAnnotations()),
		CreatedBy:   project.GetAuditInfo().GetCreatedBy(),
		CreatedAt:   project.GetAuditInfo().GetCreatedAt().AsTime(),
		UpdatedBy:   project.GetAuditInfo().GetUpdatedBy(),
		UpdatedAt:   project.GetAuditInfo().GetUpdatedAt().AsTime(),
	}
}

func membershipFromDocument(doc membershipDocument) *llmv1.Membership {
	var expiresAt *timestamppb.Timestamp
	if !doc.ExpiresAt.IsZero() {
		expiresAt = timestamppb.New(doc.ExpiresAt)
	}

	return &llmv1.Membership{
		Id:            doc.ID,
		TenantId:      doc.TenantID,
		ProjectId:     doc.ProjectID,
		PrincipalId:   doc.PrincipalID,
		PrincipalType: doc.PrincipalType,
		Roles:         fromRoleInts(doc.Roles),
		Status:        llmv1.ResourceStatus(doc.Status),
		ExpiresAt:     expiresAt,
		AuditInfo: &llmv1.AuditInfo{
			CreatedBy: doc.CreatedBy,
			CreatedAt: timestamppb.New(doc.CreatedAt),
			UpdatedBy: doc.UpdatedBy,
			UpdatedAt: timestamppb.New(doc.UpdatedAt),
		},
	}
}

func gatewayKeyFromDocument(doc gatewayKeyDocument) *llmv1.GatewayKey {
	var expiresAt *timestamppb.Timestamp
	if !doc.ExpiresAt.IsZero() {
		expiresAt = timestamppb.New(doc.ExpiresAt)
	}
	var lastUsedAt *timestamppb.Timestamp
	if !doc.LastUsedAt.IsZero() {
		lastUsedAt = timestamppb.New(doc.LastUsedAt)
	}

	return &llmv1.GatewayKey{
		Id:            doc.ID,
		TenantId:      doc.TenantID,
		ProjectId:     doc.ProjectID,
		DisplayName:   doc.DisplayName,
		SecretHash:    doc.SecretHash,
		Status:        llmv1.KeyStatus(doc.Status),
		AllowedModels: append([]string(nil), doc.AllowedModels...),
		Tags:          append([]string(nil), doc.Tags...),
		ExpiresAt:     expiresAt,
		QuotaPolicyId: doc.QuotaPolicyID,
		SpendPolicyId: doc.SpendPolicyID,
		LastUsedAt:    lastUsedAt,
		Metadata: &llmv1.Metadata{
			Labels:      copyMap(doc.Labels),
			Annotations: copyMap(doc.Annotations),
		},
		AuditInfo: &llmv1.AuditInfo{
			CreatedBy: doc.CreatedBy,
			CreatedAt: timestamppb.New(doc.CreatedAt),
			UpdatedBy: doc.UpdatedBy,
			UpdatedAt: timestamppb.New(doc.UpdatedAt),
		},
	}
}

func gatewayKeyToDocument(gatewayKey *llmv1.GatewayKey) gatewayKeyDocument {
	gatewayKey = ensureGatewayKeyDefaults(gatewayKey)
	return gatewayKeyDocument{
		ID:            gatewayKey.GetId(),
		TenantID:      gatewayKey.GetTenantId(),
		ProjectID:     gatewayKey.GetProjectId(),
		DisplayName:   gatewayKey.GetDisplayName(),
		SecretHash:    gatewayKey.GetSecretHash(),
		Status:        int32(gatewayKey.GetStatus()),
		AllowedModels: append([]string(nil), gatewayKey.GetAllowedModels()...),
		Tags:          append([]string(nil), gatewayKey.GetTags()...),
		ExpiresAt:     gatewayKey.GetExpiresAt().AsTime(),
		QuotaPolicyID: gatewayKey.GetQuotaPolicyId(),
		SpendPolicyID: gatewayKey.GetSpendPolicyId(),
		LastUsedAt:    gatewayKey.GetLastUsedAt().AsTime(),
		Labels:        copyMap(gatewayKey.GetMetadata().GetLabels()),
		Annotations:   copyMap(gatewayKey.GetMetadata().GetAnnotations()),
		CreatedBy:     gatewayKey.GetAuditInfo().GetCreatedBy(),
		CreatedAt:     gatewayKey.GetAuditInfo().GetCreatedAt().AsTime(),
		UpdatedBy:     gatewayKey.GetAuditInfo().GetUpdatedBy(),
		UpdatedAt:     gatewayKey.GetAuditInfo().GetUpdatedAt().AsTime(),
	}
}

func upstreamCredentialFromDocument(doc upstreamCredentialDocument) *llmv1.UpstreamCredential {
	var lastRotatedAt *timestamppb.Timestamp
	if !doc.LastRotatedAt.IsZero() {
		lastRotatedAt = timestamppb.New(doc.LastRotatedAt)
	}
	var expiresAt *timestamppb.Timestamp
	if !doc.ExpiresAt.IsZero() {
		expiresAt = timestamppb.New(doc.ExpiresAt)
	}

	return &llmv1.UpstreamCredential{
		Id:            doc.ID,
		VendorType:    llmv1.VendorType(doc.VendorType),
		DisplayName:   doc.DisplayName,
		Status:        llmv1.CredentialStatus(doc.Status),
		SecretRef:     doc.SecretRef,
		Scopes:        append([]string(nil), doc.Scopes...),
		LastRotatedAt: lastRotatedAt,
		ExpiresAt:     expiresAt,
		Metadata: &llmv1.Metadata{
			Labels:      copyMap(doc.Labels),
			Annotations: copyMap(doc.Annotations),
		},
		AuditInfo: &llmv1.AuditInfo{
			CreatedBy: doc.CreatedBy,
			CreatedAt: timestamppb.New(doc.CreatedAt),
			UpdatedBy: doc.UpdatedBy,
			UpdatedAt: timestamppb.New(doc.UpdatedAt),
		},
	}
}

func upstreamCredentialToDocument(credential *llmv1.UpstreamCredential) upstreamCredentialDocument {
	credential = ensureUpstreamCredentialDefaults(credential)
	return upstreamCredentialDocument{
		ID:            credential.GetId(),
		VendorType:    int32(credential.GetVendorType()),
		DisplayName:   credential.GetDisplayName(),
		Status:        int32(credential.GetStatus()),
		SecretRef:     credential.GetSecretRef(),
		Scopes:        append([]string(nil), credential.GetScopes()...),
		LastRotatedAt: credential.GetLastRotatedAt().AsTime(),
		ExpiresAt:     credential.GetExpiresAt().AsTime(),
		Labels:        copyMap(credential.GetMetadata().GetLabels()),
		Annotations:   copyMap(credential.GetMetadata().GetAnnotations()),
		CreatedBy:     credential.GetAuditInfo().GetCreatedBy(),
		CreatedAt:     credential.GetAuditInfo().GetCreatedAt().AsTime(),
		UpdatedBy:     credential.GetAuditInfo().GetUpdatedBy(),
		UpdatedAt:     credential.GetAuditInfo().GetUpdatedAt().AsTime(),
	}
}

func vendorFromDocument(doc vendorDocument) *llmv1.Vendor {
	var timeout *durationpb.Duration
	if doc.TimeoutSeconds > 0 {
		timeout = durationpb.New(time.Duration(doc.TimeoutSeconds) * time.Second)
	}

	return &llmv1.Vendor{
		Id:                   doc.ID,
		Name:                 doc.Name,
		DisplayName:          doc.DisplayName,
		VendorType:           llmv1.VendorType(doc.VendorType),
		Endpoint:             doc.Endpoint,
		UpstreamCredentialId: doc.UpstreamCredentialID,
		Status:               llmv1.ResourceStatus(doc.Status),
		SupportedCapabilities: append([]string(nil),
			doc.SupportedCapabilities...,
		),
		Timeout: timeout,
		Metadata: &llmv1.Metadata{
			Labels:      copyMap(doc.Labels),
			Annotations: copyMap(doc.Annotations),
		},
		AuditInfo: &llmv1.AuditInfo{
			CreatedBy: doc.CreatedBy,
			CreatedAt: timestamppb.New(doc.CreatedAt),
			UpdatedBy: doc.UpdatedBy,
			UpdatedAt: timestamppb.New(doc.UpdatedAt),
		},
	}
}

func vendorToDocument(vendor *llmv1.Vendor) vendorDocument {
	vendor = ensureVendorDefaults(vendor)
	var timeoutSeconds int64
	if vendor.GetTimeout() != nil {
		timeoutSeconds = int64(vendor.GetTimeout().AsDuration() / time.Second)
	}

	return vendorDocument{
		ID:                    vendor.GetId(),
		Name:                  vendor.GetName(),
		DisplayName:           vendor.GetDisplayName(),
		VendorType:            int32(vendor.GetVendorType()),
		Endpoint:              vendor.GetEndpoint(),
		UpstreamCredentialID:  vendor.GetUpstreamCredentialId(),
		Status:                int32(vendor.GetStatus()),
		SupportedCapabilities: append([]string(nil), vendor.GetSupportedCapabilities()...),
		TimeoutSeconds:        timeoutSeconds,
		Labels:                copyMap(vendor.GetMetadata().GetLabels()),
		Annotations:           copyMap(vendor.GetMetadata().GetAnnotations()),
		CreatedBy:             vendor.GetAuditInfo().GetCreatedBy(),
		CreatedAt:             vendor.GetAuditInfo().GetCreatedAt().AsTime(),
		UpdatedBy:             vendor.GetAuditInfo().GetUpdatedBy(),
		UpdatedAt:             vendor.GetAuditInfo().GetUpdatedAt().AsTime(),
	}
}

func logicalModelFromDocument(doc logicalModelDocument) *llmv1.LogicalModel {
	return &llmv1.LogicalModel{
		Id:                       doc.ID,
		Name:                     doc.Name,
		DisplayName:              doc.DisplayName,
		Description:              doc.Description,
		Visibility:               llmv1.ModelVisibility(doc.Visibility),
		Status:                   llmv1.ResourceStatus(doc.Status),
		Capabilities:             append([]string(nil), doc.Capabilities...),
		Targets:                  vendorTargetsFromDocuments(doc.Targets),
		DefaultPricingSnapshotId: doc.DefaultPricingSnapshotID,
		AllowedTenantIds:         append([]string(nil), doc.AllowedTenantIDs...),
		Metadata: &llmv1.Metadata{
			Labels:      copyMap(doc.Labels),
			Annotations: copyMap(doc.Annotations),
		},
		AuditInfo: &llmv1.AuditInfo{
			CreatedBy: doc.CreatedBy,
			CreatedAt: timestamppb.New(doc.CreatedAt),
			UpdatedBy: doc.UpdatedBy,
			UpdatedAt: timestamppb.New(doc.UpdatedAt),
		},
	}
}

func logicalModelToDocument(model *llmv1.LogicalModel) logicalModelDocument {
	model = ensureLogicalModelDefaults(model)
	return logicalModelDocument{
		ID:                       model.GetId(),
		Name:                     model.GetName(),
		DisplayName:              model.GetDisplayName(),
		Description:              model.GetDescription(),
		Visibility:               int32(model.GetVisibility()),
		Status:                   int32(model.GetStatus()),
		Capabilities:             append([]string(nil), model.GetCapabilities()...),
		Targets:                  vendorTargetsToDocuments(model.GetTargets()),
		DefaultPricingSnapshotID: model.GetDefaultPricingSnapshotId(),
		AllowedTenantIDs:         append([]string(nil), model.GetAllowedTenantIds()...),
		Labels:                   copyMap(model.GetMetadata().GetLabels()),
		Annotations:              copyMap(model.GetMetadata().GetAnnotations()),
		CreatedBy:                model.GetAuditInfo().GetCreatedBy(),
		CreatedAt:                model.GetAuditInfo().GetCreatedAt().AsTime(),
		UpdatedBy:                model.GetAuditInfo().GetUpdatedBy(),
		UpdatedAt:                model.GetAuditInfo().GetUpdatedAt().AsTime(),
	}
}

func routingPolicyFromDocument(doc routingPolicyDocument) *llmv1.RoutingPolicy {
	return &llmv1.RoutingPolicy{
		Id:                  doc.ID,
		Name:                doc.Name,
		TenantId:            doc.TenantID,
		ProjectId:           doc.ProjectID,
		LogicalModelId:      doc.LogicalModelID,
		Strategy:            llmv1.RoutingStrategy(doc.Strategy),
		Targets:             vendorTargetsFromDocuments(doc.Targets),
		Status:              llmv1.ResourceStatus(doc.Status),
		FailoverEnabled:     doc.FailoverEnabled,
		HealthCheckRequired: doc.HealthCheckRequired,
		Metadata: &llmv1.Metadata{
			Labels:      copyMap(doc.Labels),
			Annotations: copyMap(doc.Annotations),
		},
		AuditInfo: &llmv1.AuditInfo{
			CreatedBy: doc.CreatedBy,
			CreatedAt: timestamppb.New(doc.CreatedAt),
			UpdatedBy: doc.UpdatedBy,
			UpdatedAt: timestamppb.New(doc.UpdatedAt),
		},
	}
}

func routingPolicyToDocument(policy *llmv1.RoutingPolicy) routingPolicyDocument {
	policy = ensureRoutingPolicyDefaults(policy)
	return routingPolicyDocument{
		ID:                  policy.GetId(),
		Name:                policy.GetName(),
		TenantID:            policy.GetTenantId(),
		ProjectID:           policy.GetProjectId(),
		LogicalModelID:      policy.GetLogicalModelId(),
		Strategy:            int32(policy.GetStrategy()),
		Targets:             vendorTargetsToDocuments(policy.GetTargets()),
		Status:              int32(policy.GetStatus()),
		FailoverEnabled:     policy.GetFailoverEnabled(),
		HealthCheckRequired: policy.GetHealthCheckRequired(),
		Labels:              copyMap(policy.GetMetadata().GetLabels()),
		Annotations:         copyMap(policy.GetMetadata().GetAnnotations()),
		CreatedBy:           policy.GetAuditInfo().GetCreatedBy(),
		CreatedAt:           policy.GetAuditInfo().GetCreatedAt().AsTime(),
		UpdatedBy:           policy.GetAuditInfo().GetUpdatedBy(),
		UpdatedAt:           policy.GetAuditInfo().GetUpdatedAt().AsTime(),
	}
}

func quotaPolicyFromDocument(doc quotaPolicyDocument) *llmv1.QuotaPolicy {
	return &llmv1.QuotaPolicy{
		Id:               doc.ID,
		Name:             doc.Name,
		TenantId:         doc.TenantID,
		ProjectId:        doc.ProjectID,
		GatewayKeyId:     doc.GatewayKeyID,
		Period:           llmv1.QuotaPeriod(doc.Period),
		RequestLimit:     doc.RequestLimit,
		TokenLimit:       doc.TokenLimit,
		ConcurrencyLimit: doc.ConcurrencyLimit,
		Status:           llmv1.ResourceStatus(doc.Status),
		Metadata: &llmv1.Metadata{
			Labels:      copyMap(doc.Labels),
			Annotations: copyMap(doc.Annotations),
		},
		AuditInfo: &llmv1.AuditInfo{
			CreatedBy: doc.CreatedBy,
			CreatedAt: timestamppb.New(doc.CreatedAt),
			UpdatedBy: doc.UpdatedBy,
			UpdatedAt: timestamppb.New(doc.UpdatedAt),
		},
	}
}

func quotaPolicyToDocument(policy *llmv1.QuotaPolicy) quotaPolicyDocument {
	policy = ensureQuotaPolicyDefaults(policy)
	return quotaPolicyDocument{
		ID:               policy.GetId(),
		Name:             policy.GetName(),
		TenantID:         policy.GetTenantId(),
		ProjectID:        policy.GetProjectId(),
		GatewayKeyID:     policy.GetGatewayKeyId(),
		Period:           int32(policy.GetPeriod()),
		RequestLimit:     policy.GetRequestLimit(),
		TokenLimit:       policy.GetTokenLimit(),
		ConcurrencyLimit: policy.GetConcurrencyLimit(),
		Status:           int32(policy.GetStatus()),
		Labels:           copyMap(policy.GetMetadata().GetLabels()),
		Annotations:      copyMap(policy.GetMetadata().GetAnnotations()),
		CreatedBy:        policy.GetAuditInfo().GetCreatedBy(),
		CreatedAt:        policy.GetAuditInfo().GetCreatedAt().AsTime(),
		UpdatedBy:        policy.GetAuditInfo().GetUpdatedBy(),
		UpdatedAt:        policy.GetAuditInfo().GetUpdatedAt().AsTime(),
	}
}

func spendPolicyFromDocument(doc spendPolicyDocument) *llmv1.SpendPolicy {
	return &llmv1.SpendPolicy{
		Id:           doc.ID,
		Name:         doc.Name,
		TenantId:     doc.TenantID,
		ProjectId:    doc.ProjectID,
		GatewayKeyId: doc.GatewayKeyID,
		SpendCap: &llmv1.Money{
			CurrencyCode: doc.CurrencyCode,
			Units:        doc.SpendUnits,
		},
		Period: llmv1.QuotaPeriod(doc.Period),
		Status: llmv1.ResourceStatus(doc.Status),
		Metadata: &llmv1.Metadata{
			Labels:      copyMap(doc.Labels),
			Annotations: copyMap(doc.Annotations),
		},
		AuditInfo: &llmv1.AuditInfo{
			CreatedBy: doc.CreatedBy,
			CreatedAt: timestamppb.New(doc.CreatedAt),
			UpdatedBy: doc.UpdatedBy,
			UpdatedAt: timestamppb.New(doc.UpdatedAt),
		},
	}
}

func spendPolicyToDocument(policy *llmv1.SpendPolicy) spendPolicyDocument {
	policy = ensureSpendPolicyDefaults(policy)
	return spendPolicyDocument{
		ID:           policy.GetId(),
		Name:         policy.GetName(),
		TenantID:     policy.GetTenantId(),
		ProjectID:    policy.GetProjectId(),
		GatewayKeyID: policy.GetGatewayKeyId(),
		SpendUnits:   policy.GetSpendCap().GetUnits(),
		CurrencyCode: policy.GetSpendCap().GetCurrencyCode(),
		Period:       int32(policy.GetPeriod()),
		Status:       int32(policy.GetStatus()),
		Labels:       copyMap(policy.GetMetadata().GetLabels()),
		Annotations:  copyMap(policy.GetMetadata().GetAnnotations()),
		CreatedBy:    policy.GetAuditInfo().GetCreatedBy(),
		CreatedAt:    policy.GetAuditInfo().GetCreatedAt().AsTime(),
		UpdatedBy:    policy.GetAuditInfo().GetUpdatedBy(),
		UpdatedAt:    policy.GetAuditInfo().GetUpdatedAt().AsTime(),
	}
}

func normalizePageSize(page *llmv1.PageRequest) int32 {
	if page == nil || page.GetPageSize() <= 0 {
		return 50
	}
	if page.GetPageSize() > 200 {
		return 200
	}

	return page.GetPageSize()
}

func normalizeResourceStatus(status llmv1.ResourceStatus) int32 {
	if status == llmv1.ResourceStatus_RESOURCE_STATUS_UNSPECIFIED {
		return int32(llmv1.ResourceStatus_RESOURCE_STATUS_ACTIVE)
	}

	return int32(status)
}

func normalizeBillingMode(mode llmv1.BillingMode) int32 {
	if mode == llmv1.BillingMode_BILLING_MODE_UNSPECIFIED {
		return int32(llmv1.BillingMode_BILLING_MODE_PREPAID)
	}

	return int32(mode)
}

func normalizeKeyStatus(status llmv1.KeyStatus) int32 {
	if status == llmv1.KeyStatus_KEY_STATUS_UNSPECIFIED {
		return int32(llmv1.KeyStatus_KEY_STATUS_ACTIVE)
	}
	return int32(status)
}

func normalizeCredentialStatus(status llmv1.CredentialStatus) int32 {
	if status == llmv1.CredentialStatus_CREDENTIAL_STATUS_UNSPECIFIED {
		return int32(llmv1.CredentialStatus_CREDENTIAL_STATUS_ACTIVE)
	}
	return int32(status)
}

func normalizeVendorType(vendorType llmv1.VendorType) int32 {
	return int32(vendorType)
}

func normalizeModelVisibility(visibility llmv1.ModelVisibility) llmv1.ModelVisibility {
	if visibility == llmv1.ModelVisibility_MODEL_VISIBILITY_UNSPECIFIED {
		return llmv1.ModelVisibility_MODEL_VISIBILITY_PRIVATE
	}
	return visibility
}

func normalizeRoutingStrategy(strategy llmv1.RoutingStrategy) llmv1.RoutingStrategy {
	if strategy == llmv1.RoutingStrategy_ROUTING_STRATEGY_UNSPECIFIED {
		return llmv1.RoutingStrategy_ROUTING_STRATEGY_PRIORITY
	}
	return strategy
}

func normalizeQuotaPeriod(period llmv1.QuotaPeriod) llmv1.QuotaPeriod {
	if period == llmv1.QuotaPeriod_QUOTA_PERIOD_UNSPECIFIED {
		return llmv1.QuotaPeriod_QUOTA_PERIOD_MONTH
	}
	return period
}

func copyMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}

	target := make(map[string]string, len(source))
	for key, value := range source {
		target[key] = value
	}

	return target
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}

func applyTenantUpdateMask(target *llmv1.Tenant, source *llmv1.Tenant, paths []string) {
	if len(paths) == 0 {
		paths = []string{"display_name", "status", "billing_mode", "billing_profile_id", "metadata"}
	}

	for _, path := range paths {
		switch strings.ToLower(path) {
		case "name":
			target.Name = source.GetName()
		case "display_name":
			target.DisplayName = source.GetDisplayName()
		case "status":
			target.Status = source.GetStatus()
		case "billing_mode":
			target.BillingMode = source.GetBillingMode()
		case "billing_profile_id":
			target.BillingProfileId = source.GetBillingProfileId()
		case "metadata":
			target.Metadata = source.GetMetadata()
		}
	}
}

func applyProjectUpdateMask(target *llmv1.Project, source *llmv1.Project, paths []string) {
	if len(paths) == 0 {
		paths = []string{"display_name", "description", "status", "environment", "metadata"}
	}

	for _, path := range paths {
		switch strings.ToLower(path) {
		case "name":
			target.Name = source.GetName()
		case "display_name":
			target.DisplayName = source.GetDisplayName()
		case "description":
			target.Description = source.GetDescription()
		case "status":
			target.Status = source.GetStatus()
		case "environment":
			target.Environment = source.GetEnvironment()
		case "metadata":
			target.Metadata = source.GetMetadata()
		}
	}
}

func cloneTenant(source *llmv1.Tenant) *llmv1.Tenant {
	source = ensureTenantDefaults(source)
	return &llmv1.Tenant{
		Id:               source.GetId(),
		Name:             source.GetName(),
		DisplayName:      source.GetDisplayName(),
		Status:           source.GetStatus(),
		BillingMode:      source.GetBillingMode(),
		BillingProfileId: source.GetBillingProfileId(),
		Metadata:         source.GetMetadata(),
		AuditInfo:        source.GetAuditInfo(),
		Conditions:       source.GetConditions(),
	}
}

func cloneProject(source *llmv1.Project) *llmv1.Project {
	source = ensureProjectDefaults(source)
	return &llmv1.Project{
		Id:          source.GetId(),
		TenantId:    source.GetTenantId(),
		Name:        source.GetName(),
		DisplayName: source.GetDisplayName(),
		Description: source.GetDescription(),
		Status:      source.GetStatus(),
		Environment: source.GetEnvironment(),
		Metadata:    source.GetMetadata(),
		AuditInfo:   source.GetAuditInfo(),
	}
}

func cloneGatewayKey(source *llmv1.GatewayKey) *llmv1.GatewayKey {
	source = ensureGatewayKeyDefaults(source)
	return &llmv1.GatewayKey{
		Id:            source.GetId(),
		TenantId:      source.GetTenantId(),
		ProjectId:     source.GetProjectId(),
		DisplayName:   source.GetDisplayName(),
		SecretHash:    source.GetSecretHash(),
		Status:        source.GetStatus(),
		AllowedModels: append([]string(nil), source.GetAllowedModels()...),
		Tags:          append([]string(nil), source.GetTags()...),
		ExpiresAt:     source.GetExpiresAt(),
		QuotaPolicyId: source.GetQuotaPolicyId(),
		SpendPolicyId: source.GetSpendPolicyId(),
		LastUsedAt:    source.GetLastUsedAt(),
		Metadata:      source.GetMetadata(),
		AuditInfo:     source.GetAuditInfo(),
	}
}

func cloneUpstreamCredential(source *llmv1.UpstreamCredential) *llmv1.UpstreamCredential {
	source = ensureUpstreamCredentialDefaults(source)
	return &llmv1.UpstreamCredential{
		Id:            source.GetId(),
		VendorType:    source.GetVendorType(),
		DisplayName:   source.GetDisplayName(),
		Status:        source.GetStatus(),
		SecretRef:     source.GetSecretRef(),
		Scopes:        append([]string(nil), source.GetScopes()...),
		LastRotatedAt: source.GetLastRotatedAt(),
		ExpiresAt:     source.GetExpiresAt(),
		Metadata:      source.GetMetadata(),
		AuditInfo:     source.GetAuditInfo(),
	}
}

func cloneVendor(source *llmv1.Vendor) *llmv1.Vendor {
	source = ensureVendorDefaults(source)
	return &llmv1.Vendor{
		Id:                    source.GetId(),
		Name:                  source.GetName(),
		DisplayName:           source.GetDisplayName(),
		VendorType:            source.GetVendorType(),
		Endpoint:              source.GetEndpoint(),
		UpstreamCredentialId:  source.GetUpstreamCredentialId(),
		Status:                source.GetStatus(),
		SupportedCapabilities: append([]string(nil), source.GetSupportedCapabilities()...),
		Timeout:               source.GetTimeout(),
		Metadata:              source.GetMetadata(),
		AuditInfo:             source.GetAuditInfo(),
		Conditions:            source.GetConditions(),
	}
}

func cloneLogicalModel(source *llmv1.LogicalModel) *llmv1.LogicalModel {
	source = ensureLogicalModelDefaults(source)
	return &llmv1.LogicalModel{
		Id:                       source.GetId(),
		Name:                     source.GetName(),
		DisplayName:              source.GetDisplayName(),
		Description:              source.GetDescription(),
		Visibility:               source.GetVisibility(),
		Status:                   source.GetStatus(),
		Capabilities:             append([]string(nil), source.GetCapabilities()...),
		Targets:                  source.GetTargets(),
		DefaultPricingSnapshotId: source.GetDefaultPricingSnapshotId(),
		AllowedTenantIds:         append([]string(nil), source.GetAllowedTenantIds()...),
		Metadata:                 source.GetMetadata(),
		AuditInfo:                source.GetAuditInfo(),
	}
}

func cloneRoutingPolicy(source *llmv1.RoutingPolicy) *llmv1.RoutingPolicy {
	source = ensureRoutingPolicyDefaults(source)
	return &llmv1.RoutingPolicy{
		Id:                  source.GetId(),
		Name:                source.GetName(),
		TenantId:            source.GetTenantId(),
		ProjectId:           source.GetProjectId(),
		LogicalModelId:      source.GetLogicalModelId(),
		Strategy:            source.GetStrategy(),
		Targets:             source.GetTargets(),
		Status:              source.GetStatus(),
		FailoverEnabled:     source.GetFailoverEnabled(),
		HealthCheckRequired: source.GetHealthCheckRequired(),
		Metadata:            source.GetMetadata(),
		AuditInfo:           source.GetAuditInfo(),
	}
}

func cloneQuotaPolicy(source *llmv1.QuotaPolicy) *llmv1.QuotaPolicy {
	source = ensureQuotaPolicyDefaults(source)
	return &llmv1.QuotaPolicy{
		Id:               source.GetId(),
		Name:             source.GetName(),
		TenantId:         source.GetTenantId(),
		ProjectId:        source.GetProjectId(),
		GatewayKeyId:     source.GetGatewayKeyId(),
		Period:           source.GetPeriod(),
		RequestLimit:     source.GetRequestLimit(),
		TokenLimit:       source.GetTokenLimit(),
		ConcurrencyLimit: source.GetConcurrencyLimit(),
		Status:           source.GetStatus(),
		Metadata:         source.GetMetadata(),
		AuditInfo:        source.GetAuditInfo(),
	}
}

func cloneSpendPolicy(source *llmv1.SpendPolicy) *llmv1.SpendPolicy {
	source = ensureSpendPolicyDefaults(source)
	return &llmv1.SpendPolicy{
		Id:           source.GetId(),
		Name:         source.GetName(),
		TenantId:     source.GetTenantId(),
		ProjectId:    source.GetProjectId(),
		GatewayKeyId: source.GetGatewayKeyId(),
		SpendCap:     source.GetSpendCap(),
		Period:       source.GetPeriod(),
		Status:       source.GetStatus(),
		Metadata:     source.GetMetadata(),
		AuditInfo:    source.GetAuditInfo(),
	}
}

func toRoleInts(roles []llmv1.MembershipRole) []int32 {
	result := make([]int32, 0, len(roles))
	for _, role := range roles {
		result = append(result, int32(role))
	}

	return result
}

func fromRoleInts(roles []int32) []llmv1.MembershipRole {
	result := make([]llmv1.MembershipRole, 0, len(roles))
	for _, role := range roles {
		result = append(result, llmv1.MembershipRole(role))
	}

	return result
}

func ensureTenantDefaults(tenant *llmv1.Tenant) *llmv1.Tenant {
	if tenant == nil {
		tenant = &llmv1.Tenant{}
	}
	if tenant.Metadata == nil {
		tenant.Metadata = &llmv1.Metadata{}
	}
	if tenant.AuditInfo == nil {
		tenant.AuditInfo = &llmv1.AuditInfo{}
	}

	return tenant
}

func ensureProjectDefaults(project *llmv1.Project) *llmv1.Project {
	if project == nil {
		project = &llmv1.Project{}
	}
	if project.Metadata == nil {
		project.Metadata = &llmv1.Metadata{}
	}
	if project.AuditInfo == nil {
		project.AuditInfo = &llmv1.AuditInfo{}
	}

	return project
}

func ensureMembershipDefaults(membership *llmv1.Membership) *llmv1.Membership {
	if membership == nil {
		membership = &llmv1.Membership{}
	}
	if membership.AuditInfo == nil {
		membership.AuditInfo = &llmv1.AuditInfo{}
	}

	return membership
}

func ensureGatewayKeyDefaults(gatewayKey *llmv1.GatewayKey) *llmv1.GatewayKey {
	if gatewayKey == nil {
		gatewayKey = &llmv1.GatewayKey{}
	}
	if gatewayKey.Metadata == nil {
		gatewayKey.Metadata = &llmv1.Metadata{}
	}
	if gatewayKey.AuditInfo == nil {
		gatewayKey.AuditInfo = &llmv1.AuditInfo{}
	}
	return gatewayKey
}

func ensureUpstreamCredentialDefaults(credential *llmv1.UpstreamCredential) *llmv1.UpstreamCredential {
	if credential == nil {
		credential = &llmv1.UpstreamCredential{}
	}
	if credential.Metadata == nil {
		credential.Metadata = &llmv1.Metadata{}
	}
	if credential.AuditInfo == nil {
		credential.AuditInfo = &llmv1.AuditInfo{}
	}
	return credential
}

func ensureVendorDefaults(vendor *llmv1.Vendor) *llmv1.Vendor {
	if vendor == nil {
		vendor = &llmv1.Vendor{}
	}
	if vendor.Metadata == nil {
		vendor.Metadata = &llmv1.Metadata{}
	}
	if vendor.AuditInfo == nil {
		vendor.AuditInfo = &llmv1.AuditInfo{}
	}
	return vendor
}

func ensureLogicalModelDefaults(model *llmv1.LogicalModel) *llmv1.LogicalModel {
	if model == nil {
		model = &llmv1.LogicalModel{}
	}
	if model.Metadata == nil {
		model.Metadata = &llmv1.Metadata{}
	}
	if model.AuditInfo == nil {
		model.AuditInfo = &llmv1.AuditInfo{}
	}
	return model
}

func ensureRoutingPolicyDefaults(policy *llmv1.RoutingPolicy) *llmv1.RoutingPolicy {
	if policy == nil {
		policy = &llmv1.RoutingPolicy{}
	}
	if policy.Metadata == nil {
		policy.Metadata = &llmv1.Metadata{}
	}
	if policy.AuditInfo == nil {
		policy.AuditInfo = &llmv1.AuditInfo{}
	}
	return policy
}

func ensureQuotaPolicyDefaults(policy *llmv1.QuotaPolicy) *llmv1.QuotaPolicy {
	if policy == nil {
		policy = &llmv1.QuotaPolicy{}
	}
	if policy.Metadata == nil {
		policy.Metadata = &llmv1.Metadata{}
	}
	if policy.AuditInfo == nil {
		policy.AuditInfo = &llmv1.AuditInfo{}
	}
	return policy
}

func ensureSpendPolicyDefaults(policy *llmv1.SpendPolicy) *llmv1.SpendPolicy {
	if policy == nil {
		policy = &llmv1.SpendPolicy{}
	}
	if policy.Metadata == nil {
		policy.Metadata = &llmv1.Metadata{}
	}
	if policy.AuditInfo == nil {
		policy.AuditInfo = &llmv1.AuditInfo{}
	}
	if policy.SpendCap == nil {
		policy.SpendCap = &llmv1.Money{
			CurrencyCode: "USD",
			Units:        "0",
		}
	}
	return policy
}

func applyGatewayKeyUpdateMask(target *llmv1.GatewayKey, source *llmv1.GatewayKey, paths []string) {
	if len(paths) == 0 {
		paths = []string{"display_name", "status", "allowed_models", "tags", "expires_at", "quota_policy_id", "spend_policy_id", "metadata"}
	}

	for _, path := range paths {
		switch strings.ToLower(path) {
		case "display_name":
			target.DisplayName = source.GetDisplayName()
		case "status":
			target.Status = source.GetStatus()
		case "allowed_models":
			target.AllowedModels = append([]string(nil), source.GetAllowedModels()...)
		case "tags":
			target.Tags = append([]string(nil), source.GetTags()...)
		case "expires_at":
			target.ExpiresAt = source.GetExpiresAt()
		case "quota_policy_id":
			target.QuotaPolicyId = source.GetQuotaPolicyId()
		case "spend_policy_id":
			target.SpendPolicyId = source.GetSpendPolicyId()
		case "metadata":
			target.Metadata = source.GetMetadata()
		}
	}
}

func applyUpstreamCredentialUpdateMask(target *llmv1.UpstreamCredential, source *llmv1.UpstreamCredential, paths []string) {
	if len(paths) == 0 {
		paths = []string{"display_name", "status", "secret_ref", "scopes", "expires_at", "metadata"}
	}

	for _, path := range paths {
		switch strings.ToLower(path) {
		case "vendor_type":
			target.VendorType = source.GetVendorType()
		case "display_name":
			target.DisplayName = source.GetDisplayName()
		case "status":
			target.Status = source.GetStatus()
		case "secret_ref":
			target.SecretRef = source.GetSecretRef()
		case "scopes":
			target.Scopes = append([]string(nil), source.GetScopes()...)
		case "expires_at":
			target.ExpiresAt = source.GetExpiresAt()
		case "metadata":
			target.Metadata = source.GetMetadata()
		}
	}
}

func applyVendorUpdateMask(target *llmv1.Vendor, source *llmv1.Vendor, paths []string) {
	if len(paths) == 0 {
		paths = []string{"display_name", "vendor_type", "endpoint", "upstream_credential_id", "status", "supported_capabilities", "timeout", "metadata"}
	}

	for _, path := range paths {
		switch strings.ToLower(path) {
		case "name":
			target.Name = source.GetName()
		case "display_name":
			target.DisplayName = source.GetDisplayName()
		case "vendor_type":
			target.VendorType = source.GetVendorType()
		case "endpoint":
			target.Endpoint = source.GetEndpoint()
		case "upstream_credential_id":
			target.UpstreamCredentialId = source.GetUpstreamCredentialId()
		case "status":
			target.Status = source.GetStatus()
		case "supported_capabilities":
			target.SupportedCapabilities = append([]string(nil), source.GetSupportedCapabilities()...)
		case "timeout":
			target.Timeout = source.GetTimeout()
		case "metadata":
			target.Metadata = source.GetMetadata()
		}
	}
}

func applyLogicalModelUpdateMask(target *llmv1.LogicalModel, source *llmv1.LogicalModel, paths []string) {
	if len(paths) == 0 {
		paths = []string{"display_name", "description", "visibility", "status", "capabilities", "targets", "default_pricing_snapshot_id", "allowed_tenant_ids", "metadata"}
	}

	for _, path := range paths {
		switch strings.ToLower(path) {
		case "name":
			target.Name = source.GetName()
		case "display_name":
			target.DisplayName = source.GetDisplayName()
		case "description":
			target.Description = source.GetDescription()
		case "visibility":
			target.Visibility = source.GetVisibility()
		case "status":
			target.Status = source.GetStatus()
		case "capabilities":
			target.Capabilities = append([]string(nil), source.GetCapabilities()...)
		case "targets":
			target.Targets = source.GetTargets()
		case "default_pricing_snapshot_id":
			target.DefaultPricingSnapshotId = source.GetDefaultPricingSnapshotId()
		case "allowed_tenant_ids":
			target.AllowedTenantIds = append([]string(nil), source.GetAllowedTenantIds()...)
		case "metadata":
			target.Metadata = source.GetMetadata()
		}
	}
}

func applyRoutingPolicyUpdateMask(target *llmv1.RoutingPolicy, source *llmv1.RoutingPolicy, paths []string) {
	if len(paths) == 0 {
		paths = []string{"name", "strategy", "targets", "status", "failover_enabled", "health_check_required", "metadata"}
	}

	for _, path := range paths {
		switch strings.ToLower(path) {
		case "name":
			target.Name = source.GetName()
		case "strategy":
			target.Strategy = source.GetStrategy()
		case "targets":
			target.Targets = source.GetTargets()
		case "status":
			target.Status = source.GetStatus()
		case "failover_enabled":
			target.FailoverEnabled = source.GetFailoverEnabled()
		case "health_check_required":
			target.HealthCheckRequired = source.GetHealthCheckRequired()
		case "metadata":
			target.Metadata = source.GetMetadata()
		}
	}
}

func applyQuotaPolicyUpdateMask(target *llmv1.QuotaPolicy, source *llmv1.QuotaPolicy, paths []string) {
	if len(paths) == 0 {
		paths = []string{"name", "period", "request_limit", "token_limit", "concurrency_limit", "status", "metadata"}
	}

	for _, path := range paths {
		switch strings.ToLower(path) {
		case "name":
			target.Name = source.GetName()
		case "project_id":
			target.ProjectId = source.GetProjectId()
		case "gateway_key_id":
			target.GatewayKeyId = source.GetGatewayKeyId()
		case "period":
			target.Period = source.GetPeriod()
		case "request_limit":
			target.RequestLimit = source.GetRequestLimit()
		case "token_limit":
			target.TokenLimit = source.GetTokenLimit()
		case "concurrency_limit":
			target.ConcurrencyLimit = source.GetConcurrencyLimit()
		case "status":
			target.Status = source.GetStatus()
		case "metadata":
			target.Metadata = source.GetMetadata()
		}
	}
}

func applySpendPolicyUpdateMask(target *llmv1.SpendPolicy, source *llmv1.SpendPolicy, paths []string) {
	if len(paths) == 0 {
		paths = []string{"name", "period", "spend_cap", "status", "metadata"}
	}

	for _, path := range paths {
		switch strings.ToLower(path) {
		case "name":
			target.Name = source.GetName()
		case "project_id":
			target.ProjectId = source.GetProjectId()
		case "gateway_key_id":
			target.GatewayKeyId = source.GetGatewayKeyId()
		case "period":
			target.Period = source.GetPeriod()
		case "spend_cap":
			target.SpendCap = source.GetSpendCap()
		case "status":
			target.Status = source.GetStatus()
		case "metadata":
			target.Metadata = source.GetMetadata()
		}
	}
}

func vendorTargetsToDocuments(targets []*llmv1.VendorTarget) []vendorTargetDocument {
	result := make([]vendorTargetDocument, 0, len(targets))
	for _, target := range targets {
		if target == nil {
			continue
		}
		result = append(result, vendorTargetDocument{
			VendorID:      target.GetVendorId(),
			UpstreamModel: target.GetUpstreamModel(),
			Priority:      target.GetPriority(),
			Weight:        target.GetWeight(),
			Enabled:       target.GetEnabled(),
		})
	}
	return result
}

func vendorTargetsFromDocuments(targets []vendorTargetDocument) []*llmv1.VendorTarget {
	result := make([]*llmv1.VendorTarget, 0, len(targets))
	for _, target := range targets {
		result = append(result, &llmv1.VendorTarget{
			VendorId:      target.VendorID,
			UpstreamModel: target.UpstreamModel,
			Priority:      target.Priority,
			Weight:        target.Weight,
			Enabled:       target.Enabled,
		})
	}
	return result
}

func generateSecretMaterial() (string, string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", "", fmt.Errorf("generate secret: %w", err)
	}

	plaintext := "sk-gw-" + base64.RawURLEncoding.EncodeToString(random)
	return plaintext, hashGatewaySecret(plaintext), nil
}

func hashGatewaySecret(plaintext string) string {
	trimmed := strings.TrimSpace(plaintext)
	if trimmed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(trimmed))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
