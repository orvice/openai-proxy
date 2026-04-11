package controlplane

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/twitchtv/twirp"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/orvice/aiproxy/internal/config"
	llmv1 "github.com/orvice/aiproxy/pkg/proto/llm/v1"
)

type contextKey string

const actorContextKey contextKey = "controlplane.actor"

type actor struct {
	ID           string
	Type         string
	TenantID     string
	ProjectID    string
	SystemAdmin  bool
	TenantRoles  map[llmv1.MembershipRole]struct{}
	ProjectRoles map[llmv1.MembershipRole]struct{}
}

type Manager struct {
	conf          *config.Config
	backend       *backend
	repo          *repository
	httpMux       *http.ServeMux
	enabled       bool
	tenantSvc     *tenantService
	projectSvc    *projectService
	memberSvc     *membershipService
	gatewayKeySvc *gatewayKeyService
	upstreamSvc   *upstreamCredentialService
	vendorSvc     *vendorService
	modelSvc      *modelCatalogService
	routingSvc    *routingPolicyService
	quotaSvc      *quotaPolicyService
	spendSvc      *spendPolicyService
	usageSvc      *usageService
	billingSvc    *billingService
	auditSvc      *auditService
}

func NewManager(conf *config.Config) *Manager {
	return &Manager{conf: conf}
}

func (m *Manager) Initialize() error {
	if m == nil || m.conf == nil || !m.conf.ControlPlane.IsEnabled() {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	backend, err := newBackend(ctx, m.conf.ControlPlane)
	if err != nil {
		return err
	}

	m.backend = backend
	m.repo = newRepository(backend)
	m.tenantSvc = &tenantService{repo: m.repo}
	m.projectSvc = &projectService{repo: m.repo}
	m.memberSvc = &membershipService{repo: m.repo}
	m.gatewayKeySvc = &gatewayKeyService{repo: m.repo}
	m.upstreamSvc = &upstreamCredentialService{repo: m.repo}
	m.vendorSvc = &vendorService{repo: m.repo}
	m.modelSvc = &modelCatalogService{repo: m.repo}
	m.routingSvc = &routingPolicyService{repo: m.repo}
	m.quotaSvc = &quotaPolicyService{repo: m.repo}
	m.spendSvc = &spendPolicyService{repo: m.repo}
	m.usageSvc = &usageService{repo: m.repo}
	m.billingSvc = &billingService{repo: m.repo}
	m.auditSvc = &auditService{repo: m.repo}
	m.httpMux = http.NewServeMux()

	tenantHandler := wrapTwirpHandler(llmv1.NewTenantServiceServer(m.tenantSvc), m)
	projectHandler := wrapTwirpHandler(llmv1.NewProjectServiceServer(m.projectSvc), m)
	memberHandler := wrapTwirpHandler(llmv1.NewMembershipServiceServer(m.memberSvc), m)
	gatewayKeyHandler := wrapTwirpHandler(llmv1.NewGatewayKeyServiceServer(m.gatewayKeySvc), m)
	upstreamCredentialHandler := wrapTwirpHandler(llmv1.NewUpstreamCredentialServiceServer(m.upstreamSvc), m)
	vendorHandler := wrapTwirpHandler(llmv1.NewVendorServiceServer(m.vendorSvc), m)
	modelHandler := wrapTwirpHandler(llmv1.NewModelCatalogServiceServer(m.modelSvc), m)
	routingHandler := wrapTwirpHandler(llmv1.NewRoutingPolicyServiceServer(m.routingSvc), m)
	quotaHandler := wrapTwirpHandler(llmv1.NewQuotaPolicyServiceServer(m.quotaSvc), m)
	spendHandler := wrapTwirpHandler(llmv1.NewSpendPolicyServiceServer(m.spendSvc), m)
	usageHandler := wrapTwirpHandler(llmv1.NewUsageServiceServer(m.usageSvc), m)
	billingHandler := wrapTwirpHandler(llmv1.NewBillingServiceServer(m.billingSvc), m)
	auditHandler := wrapTwirpHandler(llmv1.NewAuditServiceServer(m.auditSvc), m)

	m.httpMux.Handle(tenantHandler.PathPrefix(), tenantHandler)
	m.httpMux.Handle(projectHandler.PathPrefix(), projectHandler)
	m.httpMux.Handle(memberHandler.PathPrefix(), memberHandler)
	m.httpMux.Handle(gatewayKeyHandler.PathPrefix(), gatewayKeyHandler)
	m.httpMux.Handle(upstreamCredentialHandler.PathPrefix(), upstreamCredentialHandler)
	m.httpMux.Handle(vendorHandler.PathPrefix(), vendorHandler)
	m.httpMux.Handle(modelHandler.PathPrefix(), modelHandler)
	m.httpMux.Handle(routingHandler.PathPrefix(), routingHandler)
	m.httpMux.Handle(quotaHandler.PathPrefix(), quotaHandler)
	m.httpMux.Handle(spendHandler.PathPrefix(), spendHandler)
	m.httpMux.Handle(usageHandler.PathPrefix(), usageHandler)
	m.httpMux.Handle(billingHandler.PathPrefix(), billingHandler)
	m.httpMux.Handle(auditHandler.PathPrefix(), auditHandler)

	m.enabled = true
	return nil
}

func (m *Manager) Enabled() bool {
	return m != nil && m.enabled
}

func (m *Manager) Mount(r *gin.Engine) {
	if !m.Enabled() {
		return
	}

	r.Any("/twirp/*path", gin.WrapH(m.httpMux))
}

type tenantService struct {
	repo *repository
}

func (s *tenantService) CreateTenant(ctx context.Context, req *llmv1.CreateTenantRequest) (*llmv1.CreateTenantResponse, error) {
	act, err := requireSystemAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if req.GetTenant() == nil {
		return nil, twirp.RequiredArgumentError("tenant")
	}
	if req.GetTenant().GetName() == "" {
		return nil, twirp.InvalidArgumentError("tenant.name", "tenant name is required")
	}

	req.Tenant.AuditInfo = ensureAuditInfo(req.GetTenant().GetAuditInfo(), act.ID, true)
	tenant, err := s.repo.CreateTenant(ctx, req.GetTenant())
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.CreateTenantResponse{Tenant: tenant}, nil
}

func (s *tenantService) GetTenant(ctx context.Context, req *llmv1.GetTenantRequest) (*llmv1.GetTenantResponse, error) {
	if err := requireTenantRead(ctx, req.GetTenantId()); err != nil {
		return nil, err
	}
	tenant, err := s.repo.GetTenant(ctx, req.GetTenantId())
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("tenant %s not found", req.GetTenantId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.GetTenantResponse{Tenant: tenant}, nil
}

func (s *tenantService) ListTenants(ctx context.Context, req *llmv1.ListTenantsRequest) (*llmv1.ListTenantsResponse, error) {
	act, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}

	if !act.SystemAdmin {
		if act.TenantID == "" || !hasAnyRole(act.TenantRoles) {
			return nil, twirp.PermissionDenied.Error("tenant membership required")
		}

		tenant, getErr := s.repo.GetTenant(ctx, act.TenantID)
		if getErr != nil {
			if errors.Is(getErr, errNotFound) {
				return nil, twirp.NotFoundError(fmt.Sprintf("tenant %s not found", act.TenantID))
			}
			return nil, twirp.InternalErrorWith(getErr)
		}

		return &llmv1.ListTenantsResponse{
			Tenants: []*llmv1.Tenant{tenant},
			Page: &llmv1.PageResponse{
				TotalSize: 1,
			},
		}, nil
	}

	tenants, err := s.repo.ListTenants(ctx, req)
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.ListTenantsResponse{
		Tenants: tenants,
		Page: &llmv1.PageResponse{
			TotalSize: int64(len(tenants)),
		},
	}, nil
}

func (s *tenantService) UpdateTenant(ctx context.Context, req *llmv1.UpdateTenantRequest) (*llmv1.UpdateTenantResponse, error) {
	if req.GetTenant() == nil || req.GetTenant().GetId() == "" {
		return nil, twirp.InvalidArgumentError("tenant.id", "tenant id is required")
	}
	act, err := requireTenantWrite(ctx, req.GetTenant().GetId())
	if err != nil {
		return nil, err
	}

	req.Tenant.AuditInfo = ensureAuditInfo(req.GetTenant().GetAuditInfo(), act.ID, false)
	tenant, err := s.repo.UpdateTenant(ctx, req.GetTenant(), updateMaskPaths(req.GetUpdateMask()))
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("tenant %s not found", req.GetTenant().GetId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.UpdateTenantResponse{Tenant: tenant}, nil
}

func (s *tenantService) SuspendTenant(ctx context.Context, req *llmv1.SuspendTenantRequest) (*llmv1.SuspendTenantResponse, error) {
	if _, err := requireTenantWrite(ctx, req.GetTenantId()); err != nil {
		return nil, err
	}
	tenant, err := s.repo.UpdateTenantStatus(ctx, req.GetTenantId(), llmv1.ResourceStatus_RESOURCE_STATUS_SUSPENDED)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("tenant %s not found", req.GetTenantId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.SuspendTenantResponse{Tenant: tenant}, nil
}

func (s *tenantService) ArchiveTenant(ctx context.Context, req *llmv1.ArchiveTenantRequest) (*llmv1.ArchiveTenantResponse, error) {
	if _, err := requireTenantWrite(ctx, req.GetTenantId()); err != nil {
		return nil, err
	}
	tenant, err := s.repo.UpdateTenantStatus(ctx, req.GetTenantId(), llmv1.ResourceStatus_RESOURCE_STATUS_ARCHIVED)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("tenant %s not found", req.GetTenantId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.ArchiveTenantResponse{Tenant: tenant}, nil
}

type projectService struct {
	repo *repository
}

func (s *projectService) CreateProject(ctx context.Context, req *llmv1.CreateProjectRequest) (*llmv1.CreateProjectResponse, error) {
	if req.GetProject() == nil {
		return nil, twirp.RequiredArgumentError("project")
	}
	if req.GetTenantId() == "" {
		return nil, twirp.InvalidArgumentError("tenant_id", "tenant id is required")
	}
	act, err := requireTenantWrite(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}

	project := req.GetProject()
	project.TenantId = req.GetTenantId()
	project.AuditInfo = ensureAuditInfo(project.GetAuditInfo(), act.ID, true)

	created, err := s.repo.CreateProject(ctx, project)
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.CreateProjectResponse{Project: created}, nil
}

func (s *projectService) GetProject(ctx context.Context, req *llmv1.GetProjectRequest) (*llmv1.GetProjectResponse, error) {
	project, err := s.repo.GetProject(ctx, req.GetProjectId())
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("project %s not found", req.GetProjectId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}
	if err := requireProjectRead(ctx, project); err != nil {
		return nil, err
	}

	return &llmv1.GetProjectResponse{Project: project}, nil
}

func (s *projectService) ListProjects(ctx context.Context, req *llmv1.ListProjectsRequest) (*llmv1.ListProjectsResponse, error) {
	if err := requireTenantRead(ctx, req.GetTenantId()); err != nil {
		return nil, err
	}
	projects, err := s.repo.ListProjects(ctx, req)
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.ListProjectsResponse{
		Projects: projects,
		Page: &llmv1.PageResponse{
			TotalSize: int64(len(projects)),
		},
	}, nil
}

func (s *projectService) UpdateProject(ctx context.Context, req *llmv1.UpdateProjectRequest) (*llmv1.UpdateProjectResponse, error) {
	if req.GetProject() == nil || req.GetProject().GetId() == "" {
		return nil, twirp.InvalidArgumentError("project.id", "project id is required")
	}
	existing, err := s.repo.GetProject(ctx, req.GetProject().GetId())
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("project %s not found", req.GetProject().GetId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}
	act, err := requireProjectWrite(ctx, existing)
	if err != nil {
		return nil, err
	}

	req.Project.TenantId = existing.GetTenantId()
	req.Project.AuditInfo = ensureAuditInfo(req.GetProject().GetAuditInfo(), act.ID, false)
	project, err := s.repo.UpdateProject(ctx, req.GetProject(), updateMaskPaths(req.GetUpdateMask()))
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("project %s not found", req.GetProject().GetId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.UpdateProjectResponse{Project: project}, nil
}

func (s *projectService) ArchiveProject(ctx context.Context, req *llmv1.ArchiveProjectRequest) (*llmv1.ArchiveProjectResponse, error) {
	existing, err := s.repo.GetProject(ctx, req.GetProjectId())
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("project %s not found", req.GetProjectId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}
	if _, err := requireProjectWrite(ctx, existing); err != nil {
		return nil, err
	}

	project, err := s.repo.UpdateProjectStatus(ctx, req.GetProjectId(), llmv1.ResourceStatus_RESOURCE_STATUS_ARCHIVED)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("project %s not found", req.GetProjectId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.ArchiveProjectResponse{Project: project}, nil
}

type membershipService struct {
	repo *repository
}

func (s *membershipService) CreateMembership(ctx context.Context, req *llmv1.CreateMembershipRequest) (*llmv1.CreateMembershipResponse, error) {
	if req.GetMembership() == nil {
		return nil, twirp.RequiredArgumentError("membership")
	}
	if req.GetMembership().GetTenantId() == "" {
		return nil, twirp.InvalidArgumentError("membership.tenant_id", "tenant id is required")
	}
	if req.GetMembership().GetPrincipalId() == "" {
		return nil, twirp.InvalidArgumentError("membership.principal_id", "principal id is required")
	}
	act, err := requireMembershipWrite(ctx, req.GetMembership().GetTenantId(), req.GetMembership().GetProjectId())
	if err != nil {
		return nil, err
	}

	req.Membership.AuditInfo = ensureAuditInfo(req.GetMembership().GetAuditInfo(), act.ID, true)
	membership, err := s.repo.CreateMembership(ctx, req.GetMembership())
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.CreateMembershipResponse{Membership: membership}, nil
}

func (s *membershipService) ListMemberships(ctx context.Context, req *llmv1.ListMembershipsRequest) (*llmv1.ListMembershipsResponse, error) {
	if err := requireMembershipRead(ctx, req.GetTenantId(), req.GetProjectId()); err != nil {
		return nil, err
	}
	memberships, err := s.repo.ListMemberships(ctx, req)
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.ListMembershipsResponse{
		Memberships: memberships,
		Page: &llmv1.PageResponse{
			TotalSize: int64(len(memberships)),
		},
	}, nil
}

func (s *membershipService) DeleteMembership(ctx context.Context, req *llmv1.DeleteMembershipRequest) (*llmv1.DeleteMembershipResponse, error) {
	// Delete is privileged; without a lookup-by-id API yet, require system admin or tenant admin scope.
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	act, _ := actorFromContext(ctx)
	if !act.SystemAdmin && !hasAdminRole(act.TenantRoles) {
		return nil, twirp.PermissionDenied.Error("admin role required to delete membership")
	}
	if err := s.repo.DeleteMembership(ctx, req.GetMembershipId()); err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("membership %s not found", req.GetMembershipId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.DeleteMembershipResponse{MembershipId: req.GetMembershipId()}, nil
}

type gatewayKeyService struct {
	repo *repository
}

func (s *gatewayKeyService) CreateGatewayKey(ctx context.Context, req *llmv1.CreateGatewayKeyRequest) (*llmv1.CreateGatewayKeyResponse, error) {
	if req.GetGatewayKey() == nil {
		return nil, twirp.RequiredArgumentError("gateway_key")
	}
	if req.GetGatewayKey().GetTenantId() == "" {
		return nil, twirp.InvalidArgumentError("gateway_key.tenant_id", "tenant id is required")
	}

	act, err := requireTenantWrite(ctx, req.GetGatewayKey().GetTenantId())
	if err != nil {
		return nil, err
	}

	if req.GetGatewayKey().GetProjectId() != "" {
		project := &llmv1.Project{
			Id:       req.GetGatewayKey().GetProjectId(),
			TenantId: req.GetGatewayKey().GetTenantId(),
		}
		if _, err := requireProjectWrite(ctx, project); err != nil {
			return nil, err
		}
	}

	req.GatewayKey.AuditInfo = ensureAuditInfo(req.GetGatewayKey().GetAuditInfo(), act.ID, true)
	key, secret, err := s.repo.CreateGatewayKey(ctx, req.GetGatewayKey())
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.CreateGatewayKeyResponse{
		GatewayKey: key,
		Secret:     secret,
	}, nil
}

func (s *gatewayKeyService) GetGatewayKey(ctx context.Context, req *llmv1.GetGatewayKeyRequest) (*llmv1.GetGatewayKeyResponse, error) {
	key, err := s.repo.GetGatewayKey(ctx, req.GetGatewayKeyId())
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("gateway key %s not found", req.GetGatewayKeyId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}
	if err := requireGatewayKeyRead(ctx, key); err != nil {
		return nil, err
	}

	return &llmv1.GetGatewayKeyResponse{GatewayKey: key}, nil
}

func (s *gatewayKeyService) ListGatewayKeys(ctx context.Context, req *llmv1.ListGatewayKeysRequest) (*llmv1.ListGatewayKeysResponse, error) {
	if err := requireTenantRead(ctx, req.GetTenantId()); err != nil {
		return nil, err
	}
	if req.GetProjectId() != "" {
		project := &llmv1.Project{Id: req.GetProjectId(), TenantId: req.GetTenantId()}
		if err := requireProjectRead(ctx, project); err != nil {
			return nil, err
		}
	}

	keys, err := s.repo.ListGatewayKeys(ctx, req)
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.ListGatewayKeysResponse{
		GatewayKeys: keys,
		Page: &llmv1.PageResponse{
			TotalSize: int64(len(keys)),
		},
	}, nil
}

func (s *gatewayKeyService) UpdateGatewayKey(ctx context.Context, req *llmv1.UpdateGatewayKeyRequest) (*llmv1.UpdateGatewayKeyResponse, error) {
	if req.GetGatewayKey() == nil || req.GetGatewayKey().GetId() == "" {
		return nil, twirp.InvalidArgumentError("gateway_key.id", "gateway key id is required")
	}

	existing, err := s.repo.GetGatewayKey(ctx, req.GetGatewayKey().GetId())
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("gateway key %s not found", req.GetGatewayKey().GetId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}

	act, err := requireGatewayKeyWrite(ctx, existing)
	if err != nil {
		return nil, err
	}

	req.GatewayKey.TenantId = existing.GetTenantId()
	req.GatewayKey.ProjectId = existing.GetProjectId()
	req.GatewayKey.SecretHash = existing.GetSecretHash()
	req.GatewayKey.AuditInfo = ensureAuditInfo(req.GetGatewayKey().GetAuditInfo(), act.ID, false)

	key, err := s.repo.UpdateGatewayKey(ctx, req.GetGatewayKey(), updateMaskPaths(req.GetUpdateMask()))
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("gateway key %s not found", req.GetGatewayKey().GetId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.UpdateGatewayKeyResponse{GatewayKey: key}, nil
}

func (s *gatewayKeyService) RotateGatewayKey(ctx context.Context, req *llmv1.RotateGatewayKeyRequest) (*llmv1.RotateGatewayKeyResponse, error) {
	existing, err := s.repo.GetGatewayKey(ctx, req.GetGatewayKeyId())
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("gateway key %s not found", req.GetGatewayKeyId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}

	act, err := requireGatewayKeyWrite(ctx, existing)
	if err != nil {
		return nil, err
	}

	key, secret, err := s.repo.RotateGatewayKey(ctx, req.GetGatewayKeyId(), act.ID)
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.RotateGatewayKeyResponse{
		GatewayKey: key,
		Secret:     secret,
	}, nil
}

func (s *gatewayKeyService) RevokeGatewayKey(ctx context.Context, req *llmv1.RevokeGatewayKeyRequest) (*llmv1.RevokeGatewayKeyResponse, error) {
	existing, err := s.repo.GetGatewayKey(ctx, req.GetGatewayKeyId())
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("gateway key %s not found", req.GetGatewayKeyId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}

	act, err := requireGatewayKeyWrite(ctx, existing)
	if err != nil {
		return nil, err
	}

	key, err := s.repo.RevokeGatewayKey(ctx, req.GetGatewayKeyId(), act.ID)
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.RevokeGatewayKeyResponse{GatewayKey: key}, nil
}

type upstreamCredentialService struct {
	repo *repository
}

func (s *upstreamCredentialService) CreateUpstreamCredential(ctx context.Context, req *llmv1.CreateUpstreamCredentialRequest) (*llmv1.CreateUpstreamCredentialResponse, error) {
	act, err := requireSystemAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if req.GetUpstreamCredential() == nil {
		return nil, twirp.RequiredArgumentError("upstream_credential")
	}

	req.UpstreamCredential.AuditInfo = ensureAuditInfo(req.GetUpstreamCredential().GetAuditInfo(), act.ID, true)
	credential, err := s.repo.CreateUpstreamCredential(ctx, req.GetUpstreamCredential())
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.CreateUpstreamCredentialResponse{UpstreamCredential: credential}, nil
}

func (s *upstreamCredentialService) GetUpstreamCredential(ctx context.Context, req *llmv1.GetUpstreamCredentialRequest) (*llmv1.GetUpstreamCredentialResponse, error) {
	if _, err := requireSystemAdmin(ctx); err != nil {
		return nil, err
	}

	credential, err := s.repo.GetUpstreamCredential(ctx, req.GetUpstreamCredentialId())
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("upstream credential %s not found", req.GetUpstreamCredentialId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.GetUpstreamCredentialResponse{UpstreamCredential: credential}, nil
}

func (s *upstreamCredentialService) ListUpstreamCredentials(ctx context.Context, req *llmv1.ListUpstreamCredentialsRequest) (*llmv1.ListUpstreamCredentialsResponse, error) {
	if _, err := requireSystemAdmin(ctx); err != nil {
		return nil, err
	}

	credentials, err := s.repo.ListUpstreamCredentials(ctx, req)
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.ListUpstreamCredentialsResponse{
		UpstreamCredentials: credentials,
		Page: &llmv1.PageResponse{
			TotalSize: int64(len(credentials)),
		},
	}, nil
}

func (s *upstreamCredentialService) UpdateUpstreamCredential(ctx context.Context, req *llmv1.UpdateUpstreamCredentialRequest) (*llmv1.UpdateUpstreamCredentialResponse, error) {
	act, err := requireSystemAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if req.GetUpstreamCredential() == nil || req.GetUpstreamCredential().GetId() == "" {
		return nil, twirp.InvalidArgumentError("upstream_credential.id", "upstream credential id is required")
	}

	existing, err := s.repo.GetUpstreamCredential(ctx, req.GetUpstreamCredential().GetId())
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("upstream credential %s not found", req.GetUpstreamCredential().GetId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}

	req.UpstreamCredential.AuditInfo = ensureAuditInfo(req.GetUpstreamCredential().GetAuditInfo(), act.ID, false)
	req.UpstreamCredential.SecretRef = firstNonEmpty(req.GetUpstreamCredential().GetSecretRef(), existing.GetSecretRef())

	credential, err := s.repo.UpdateUpstreamCredential(ctx, req.GetUpstreamCredential(), updateMaskPaths(req.GetUpdateMask()))
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.UpdateUpstreamCredentialResponse{UpstreamCredential: credential}, nil
}

func (s *upstreamCredentialService) RotateUpstreamCredential(ctx context.Context, req *llmv1.RotateUpstreamCredentialRequest) (*llmv1.RotateUpstreamCredentialResponse, error) {
	act, err := requireSystemAdmin(ctx)
	if err != nil {
		return nil, err
	}

	credential, err := s.repo.RotateUpstreamCredential(ctx, req.GetUpstreamCredentialId(), act.ID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("upstream credential %s not found", req.GetUpstreamCredentialId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.RotateUpstreamCredentialResponse{UpstreamCredential: credential}, nil
}

type vendorService struct {
	repo *repository
}

func (s *vendorService) CreateVendor(ctx context.Context, req *llmv1.CreateVendorRequest) (*llmv1.CreateVendorResponse, error) {
	act, err := requireSystemAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if req.GetVendor() == nil {
		return nil, twirp.RequiredArgumentError("vendor")
	}

	req.Vendor.AuditInfo = ensureAuditInfo(req.GetVendor().GetAuditInfo(), act.ID, true)
	vendor, err := s.repo.CreateVendor(ctx, req.GetVendor())
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.CreateVendorResponse{Vendor: vendor}, nil
}

func (s *vendorService) GetVendor(ctx context.Context, req *llmv1.GetVendorRequest) (*llmv1.GetVendorResponse, error) {
	if _, err := requireSystemAdmin(ctx); err != nil {
		return nil, err
	}

	vendor, err := s.repo.GetVendor(ctx, req.GetVendorId())
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("vendor %s not found", req.GetVendorId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.GetVendorResponse{Vendor: vendor}, nil
}

func (s *vendorService) ListVendors(ctx context.Context, req *llmv1.ListVendorsRequest) (*llmv1.ListVendorsResponse, error) {
	if _, err := requireSystemAdmin(ctx); err != nil {
		return nil, err
	}

	vendors, err := s.repo.ListVendors(ctx, req)
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.ListVendorsResponse{
		Vendors: vendors,
		Page: &llmv1.PageResponse{
			TotalSize: int64(len(vendors)),
		},
	}, nil
}

func (s *vendorService) UpdateVendor(ctx context.Context, req *llmv1.UpdateVendorRequest) (*llmv1.UpdateVendorResponse, error) {
	act, err := requireSystemAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if req.GetVendor() == nil || req.GetVendor().GetId() == "" {
		return nil, twirp.InvalidArgumentError("vendor.id", "vendor id is required")
	}

	req.Vendor.AuditInfo = ensureAuditInfo(req.GetVendor().GetAuditInfo(), act.ID, false)
	vendor, err := s.repo.UpdateVendor(ctx, req.GetVendor(), updateMaskPaths(req.GetUpdateMask()))
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("vendor %s not found", req.GetVendor().GetId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.UpdateVendorResponse{Vendor: vendor}, nil
}

type modelCatalogService struct {
	repo *repository
}

func (s *modelCatalogService) CreateLogicalModel(ctx context.Context, req *llmv1.CreateLogicalModelRequest) (*llmv1.CreateLogicalModelResponse, error) {
	act, err := requireSystemAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if req.GetLogicalModel() == nil {
		return nil, twirp.RequiredArgumentError("logical_model")
	}

	req.LogicalModel.AuditInfo = ensureAuditInfo(req.GetLogicalModel().GetAuditInfo(), act.ID, true)
	model, err := s.repo.CreateLogicalModel(ctx, req.GetLogicalModel())
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.CreateLogicalModelResponse{LogicalModel: model}, nil
}

func (s *modelCatalogService) GetLogicalModel(ctx context.Context, req *llmv1.GetLogicalModelRequest) (*llmv1.GetLogicalModelResponse, error) {
	model, err := s.repo.GetLogicalModel(ctx, req.GetLogicalModelId())
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("logical model %s not found", req.GetLogicalModelId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}
	if err := requireLogicalModelRead(ctx, model); err != nil {
		return nil, err
	}

	return &llmv1.GetLogicalModelResponse{LogicalModel: model}, nil
}

func (s *modelCatalogService) ListLogicalModels(ctx context.Context, req *llmv1.ListLogicalModelsRequest) (*llmv1.ListLogicalModelsResponse, error) {
	models, err := s.repo.ListLogicalModels(ctx, req)
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	filtered := make([]*llmv1.LogicalModel, 0, len(models))
	for _, model := range models {
		if err := requireLogicalModelRead(ctx, model); err == nil {
			filtered = append(filtered, model)
		}
	}

	return &llmv1.ListLogicalModelsResponse{
		LogicalModels: filtered,
		Page: &llmv1.PageResponse{
			TotalSize: int64(len(filtered)),
		},
	}, nil
}

func (s *modelCatalogService) UpdateLogicalModel(ctx context.Context, req *llmv1.UpdateLogicalModelRequest) (*llmv1.UpdateLogicalModelResponse, error) {
	act, err := requireSystemAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if req.GetLogicalModel() == nil || req.GetLogicalModel().GetId() == "" {
		return nil, twirp.InvalidArgumentError("logical_model.id", "logical model id is required")
	}

	req.LogicalModel.AuditInfo = ensureAuditInfo(req.GetLogicalModel().GetAuditInfo(), act.ID, false)
	model, err := s.repo.UpdateLogicalModel(ctx, req.GetLogicalModel(), updateMaskPaths(req.GetUpdateMask()))
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("logical model %s not found", req.GetLogicalModel().GetId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.UpdateLogicalModelResponse{LogicalModel: model}, nil
}

type routingPolicyService struct {
	repo *repository
}

func (s *routingPolicyService) CreateRoutingPolicy(ctx context.Context, req *llmv1.CreateRoutingPolicyRequest) (*llmv1.CreateRoutingPolicyResponse, error) {
	if req.GetRoutingPolicy() == nil {
		return nil, twirp.RequiredArgumentError("routing_policy")
	}

	act, err := requireRoutingPolicyWrite(ctx, req.GetRoutingPolicy().GetTenantId(), req.GetRoutingPolicy().GetProjectId())
	if err != nil {
		return nil, err
	}

	req.RoutingPolicy.AuditInfo = ensureAuditInfo(req.GetRoutingPolicy().GetAuditInfo(), act.ID, true)
	policy, err := s.repo.CreateRoutingPolicy(ctx, req.GetRoutingPolicy())
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.CreateRoutingPolicyResponse{RoutingPolicy: policy}, nil
}

func (s *routingPolicyService) GetRoutingPolicy(ctx context.Context, req *llmv1.GetRoutingPolicyRequest) (*llmv1.GetRoutingPolicyResponse, error) {
	policy, err := s.repo.GetRoutingPolicy(ctx, req.GetRoutingPolicyId())
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("routing policy %s not found", req.GetRoutingPolicyId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}
	if err := requireRoutingPolicyRead(ctx, policy); err != nil {
		return nil, err
	}

	return &llmv1.GetRoutingPolicyResponse{RoutingPolicy: policy}, nil
}

func (s *routingPolicyService) ListRoutingPolicies(ctx context.Context, req *llmv1.ListRoutingPoliciesRequest) (*llmv1.ListRoutingPoliciesResponse, error) {
	if err := requireRoutingPolicyList(ctx, req.GetTenantId(), req.GetProjectId()); err != nil {
		return nil, err
	}

	policies, err := s.repo.ListRoutingPolicies(ctx, req)
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.ListRoutingPoliciesResponse{
		RoutingPolicies: policies,
		Page: &llmv1.PageResponse{
			TotalSize: int64(len(policies)),
		},
	}, nil
}

func (s *routingPolicyService) UpdateRoutingPolicy(ctx context.Context, req *llmv1.UpdateRoutingPolicyRequest) (*llmv1.UpdateRoutingPolicyResponse, error) {
	if req.GetRoutingPolicy() == nil || req.GetRoutingPolicy().GetId() == "" {
		return nil, twirp.InvalidArgumentError("routing_policy.id", "routing policy id is required")
	}

	existing, err := s.repo.GetRoutingPolicy(ctx, req.GetRoutingPolicy().GetId())
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("routing policy %s not found", req.GetRoutingPolicy().GetId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}
	act, err := requireRoutingPolicyWrite(ctx, existing.GetTenantId(), existing.GetProjectId())
	if err != nil {
		return nil, err
	}

	req.RoutingPolicy.TenantId = existing.GetTenantId()
	req.RoutingPolicy.ProjectId = existing.GetProjectId()
	req.RoutingPolicy.AuditInfo = ensureAuditInfo(req.GetRoutingPolicy().GetAuditInfo(), act.ID, false)
	policy, err := s.repo.UpdateRoutingPolicy(ctx, req.GetRoutingPolicy(), updateMaskPaths(req.GetUpdateMask()))
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.UpdateRoutingPolicyResponse{RoutingPolicy: policy}, nil
}

type quotaPolicyService struct {
	repo *repository
}

func (s *quotaPolicyService) CreateQuotaPolicy(ctx context.Context, req *llmv1.CreateQuotaPolicyRequest) (*llmv1.CreateQuotaPolicyResponse, error) {
	if req.GetQuotaPolicy() == nil {
		return nil, twirp.RequiredArgumentError("quota_policy")
	}
	if req.GetQuotaPolicy().GetTenantId() == "" {
		return nil, twirp.InvalidArgumentError("quota_policy.tenant_id", "tenant id is required")
	}

	act, err := requirePolicyWrite(ctx, req.GetQuotaPolicy().GetTenantId(), req.GetQuotaPolicy().GetProjectId())
	if err != nil {
		return nil, err
	}

	req.QuotaPolicy.AuditInfo = ensureAuditInfo(req.GetQuotaPolicy().GetAuditInfo(), act.ID, true)
	policy, err := s.repo.CreateQuotaPolicy(ctx, req.GetQuotaPolicy())
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.CreateQuotaPolicyResponse{QuotaPolicy: policy}, nil
}

func (s *quotaPolicyService) GetQuotaPolicy(ctx context.Context, req *llmv1.GetQuotaPolicyRequest) (*llmv1.GetQuotaPolicyResponse, error) {
	policy, err := s.repo.GetQuotaPolicy(ctx, req.GetQuotaPolicyId())
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("quota policy %s not found", req.GetQuotaPolicyId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}
	if err := requirePolicyRead(ctx, policy.GetTenantId(), policy.GetProjectId()); err != nil {
		return nil, err
	}

	return &llmv1.GetQuotaPolicyResponse{QuotaPolicy: policy}, nil
}

func (s *quotaPolicyService) ListQuotaPolicies(ctx context.Context, req *llmv1.ListQuotaPoliciesRequest) (*llmv1.ListQuotaPoliciesResponse, error) {
	if err := requirePolicyList(ctx, req.GetTenantId(), req.GetProjectId()); err != nil {
		return nil, err
	}

	policies, err := s.repo.ListQuotaPolicies(ctx, req)
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.ListQuotaPoliciesResponse{
		QuotaPolicies: policies,
		Page: &llmv1.PageResponse{
			TotalSize: int64(len(policies)),
		},
	}, nil
}

func (s *quotaPolicyService) UpdateQuotaPolicy(ctx context.Context, req *llmv1.UpdateQuotaPolicyRequest) (*llmv1.UpdateQuotaPolicyResponse, error) {
	if req.GetQuotaPolicy() == nil || req.GetQuotaPolicy().GetId() == "" {
		return nil, twirp.InvalidArgumentError("quota_policy.id", "quota policy id is required")
	}

	existing, err := s.repo.GetQuotaPolicy(ctx, req.GetQuotaPolicy().GetId())
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("quota policy %s not found", req.GetQuotaPolicy().GetId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}

	act, err := requirePolicyWrite(ctx, existing.GetTenantId(), existing.GetProjectId())
	if err != nil {
		return nil, err
	}

	req.QuotaPolicy.TenantId = existing.GetTenantId()
	req.QuotaPolicy.ProjectId = existing.GetProjectId()
	req.QuotaPolicy.AuditInfo = ensureAuditInfo(req.GetQuotaPolicy().GetAuditInfo(), act.ID, false)
	policy, err := s.repo.UpdateQuotaPolicy(ctx, req.GetQuotaPolicy(), updateMaskPaths(req.GetUpdateMask()))
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.UpdateQuotaPolicyResponse{QuotaPolicy: policy}, nil
}

type spendPolicyService struct {
	repo *repository
}

func (s *spendPolicyService) CreateSpendPolicy(ctx context.Context, req *llmv1.CreateSpendPolicyRequest) (*llmv1.CreateSpendPolicyResponse, error) {
	if req.GetSpendPolicy() == nil {
		return nil, twirp.RequiredArgumentError("spend_policy")
	}
	if req.GetSpendPolicy().GetTenantId() == "" {
		return nil, twirp.InvalidArgumentError("spend_policy.tenant_id", "tenant id is required")
	}

	act, err := requirePolicyWrite(ctx, req.GetSpendPolicy().GetTenantId(), req.GetSpendPolicy().GetProjectId())
	if err != nil {
		return nil, err
	}

	req.SpendPolicy.AuditInfo = ensureAuditInfo(req.GetSpendPolicy().GetAuditInfo(), act.ID, true)
	policy, err := s.repo.CreateSpendPolicy(ctx, req.GetSpendPolicy())
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.CreateSpendPolicyResponse{SpendPolicy: policy}, nil
}

func (s *spendPolicyService) GetSpendPolicy(ctx context.Context, req *llmv1.GetSpendPolicyRequest) (*llmv1.GetSpendPolicyResponse, error) {
	policy, err := s.repo.GetSpendPolicy(ctx, req.GetSpendPolicyId())
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("spend policy %s not found", req.GetSpendPolicyId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}
	if err := requirePolicyRead(ctx, policy.GetTenantId(), policy.GetProjectId()); err != nil {
		return nil, err
	}

	return &llmv1.GetSpendPolicyResponse{SpendPolicy: policy}, nil
}

func (s *spendPolicyService) ListSpendPolicies(ctx context.Context, req *llmv1.ListSpendPoliciesRequest) (*llmv1.ListSpendPoliciesResponse, error) {
	if err := requirePolicyList(ctx, req.GetTenantId(), req.GetProjectId()); err != nil {
		return nil, err
	}

	policies, err := s.repo.ListSpendPolicies(ctx, req)
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.ListSpendPoliciesResponse{
		SpendPolicies: policies,
		Page: &llmv1.PageResponse{
			TotalSize: int64(len(policies)),
		},
	}, nil
}

func (s *spendPolicyService) UpdateSpendPolicy(ctx context.Context, req *llmv1.UpdateSpendPolicyRequest) (*llmv1.UpdateSpendPolicyResponse, error) {
	if req.GetSpendPolicy() == nil || req.GetSpendPolicy().GetId() == "" {
		return nil, twirp.InvalidArgumentError("spend_policy.id", "spend policy id is required")
	}

	existing, err := s.repo.GetSpendPolicy(ctx, req.GetSpendPolicy().GetId())
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, twirp.NotFoundError(fmt.Sprintf("spend policy %s not found", req.GetSpendPolicy().GetId()))
		}
		return nil, twirp.InternalErrorWith(err)
	}

	act, err := requirePolicyWrite(ctx, existing.GetTenantId(), existing.GetProjectId())
	if err != nil {
		return nil, err
	}

	req.SpendPolicy.TenantId = existing.GetTenantId()
	req.SpendPolicy.ProjectId = existing.GetProjectId()
	req.SpendPolicy.AuditInfo = ensureAuditInfo(req.GetSpendPolicy().GetAuditInfo(), act.ID, false)
	policy, err := s.repo.UpdateSpendPolicy(ctx, req.GetSpendPolicy(), updateMaskPaths(req.GetUpdateMask()))
	if err != nil {
		return nil, twirp.InternalErrorWith(err)
	}

	return &llmv1.UpdateSpendPolicyResponse{SpendPolicy: policy}, nil
}

type usageService struct {
	repo *repository
}

func (s *usageService) ListUsageRecords(ctx context.Context, req *llmv1.ListUsageRecordsRequest) (*llmv1.ListUsageRecordsResponse, error) {
	if err := requirePolicyList(ctx, req.GetTenantId(), req.GetProjectId()); err != nil {
		return nil, err
	}

	filter := usageFilter(req.GetTenantId(), req.GetProjectId(), req.GetGatewayKeyId(), req.GetLogicalModelId(), req.GetVendorId(), req.GetTimeRange())
	findOptions := options.Find().SetLimit(int64(normalizePageSize(req.GetPage())))
	cursor, err := s.repo.backend.collection(usageRecordsCollection).Find(ctx, filter, findOptions)
	if err != nil {
		return nil, twirp.InternalErrorWith(fmt.Errorf("find usage records: %w", err))
	}
	defer cursor.Close(ctx)

	var docs []bson.M
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, twirp.InternalErrorWith(fmt.Errorf("decode usage records: %w", err))
	}

	records := make([]*llmv1.UsageRecord, 0, len(docs))
	for _, doc := range docs {
		records = append(records, usageRecordFromDoc(doc))
	}

	return &llmv1.ListUsageRecordsResponse{
		UsageRecords: records,
		Page: &llmv1.PageResponse{
			TotalSize: int64(len(records)),
		},
	}, nil
}

func (s *usageService) GetUsageSummary(ctx context.Context, req *llmv1.GetUsageSummaryRequest) (*llmv1.GetUsageSummaryResponse, error) {
	if err := requirePolicyList(ctx, req.GetTenantId(), req.GetProjectId()); err != nil {
		return nil, err
	}

	filter := usageFilter(req.GetTenantId(), req.GetProjectId(), "", req.GetLogicalModelId(), req.GetVendorId(), req.GetTimeRange())
	cursor, err := s.repo.backend.collection(usageRecordsCollection).Find(ctx, filter)
	if err != nil {
		return nil, twirp.InternalErrorWith(fmt.Errorf("find usage summary records: %w", err))
	}
	defer cursor.Close(ctx)

	var docs []bson.M
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, twirp.InternalErrorWith(fmt.Errorf("decode usage summary records: %w", err))
	}

	summary := &llmv1.UsageSummary{
		TenantId:       req.GetTenantId(),
		ProjectId:      req.GetProjectId(),
		LogicalModelId: req.GetLogicalModelId(),
		VendorId:       req.GetVendorId(),
		TimeRange:      req.GetTimeRange(),
	}
	for _, doc := range docs {
		summary.RequestCount++
		summary.PromptTokens += toInt64(doc["prompt_tokens"])
		summary.CompletionTokens += toInt64(doc["completion_tokens"])
		summary.TotalTokens += toInt64(doc["total_tokens"])
		summary.BillableUnits += toInt64(doc["billable_units"])
	}

	return &llmv1.GetUsageSummaryResponse{
		Summaries: []*llmv1.UsageSummary{summary},
	}, nil
}

type billingService struct {
	repo *repository
}

func (s *billingService) ListPricingSnapshots(ctx context.Context, req *llmv1.ListPricingSnapshotsRequest) (*llmv1.ListPricingSnapshotsResponse, error) {
	// Pricing snapshots are not persisted yet in this phase.
	return &llmv1.ListPricingSnapshotsResponse{
		PricingSnapshots: []*llmv1.PricingSnapshot{},
		Page: &llmv1.PageResponse{
			TotalSize: 0,
		},
	}, nil
}

func (s *billingService) GetBalance(ctx context.Context, req *llmv1.GetBalanceRequest) (*llmv1.GetBalanceResponse, error) {
	if err := requireTenantRead(ctx, req.GetTenantId()); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	rangeStart := timestamppb.New(time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC))
	rangeEnd := timestamppb.New(now)
	filter := usageFilter(req.GetTenantId(), "", "", "", "", &llmv1.TimeRange{
		StartTime: rangeStart,
		EndTime:   rangeEnd,
	})

	cursor, err := s.repo.backend.collection(usageRecordsCollection).Find(ctx, filter)
	if err != nil {
		return nil, twirp.InternalErrorWith(fmt.Errorf("find tenant balance usage: %w", err))
	}
	defer cursor.Close(ctx)

	var docs []bson.M
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, twirp.InternalErrorWith(fmt.Errorf("decode tenant balance usage: %w", err))
	}

	var billedUnits int64
	for _, doc := range docs {
		billedUnits += toInt64(doc["billable_units"])
	}

	return &llmv1.GetBalanceResponse{
		Balance: &llmv1.Balance{
			TenantId: req.GetTenantId(),
			AsOfTime: timestamppb.Now(),
			CurrentPeriodSpend: &llmv1.Money{
				CurrencyCode: "USD",
				Units:        fmt.Sprintf("%d", billedUnits),
			},
			AvailableBalance: &llmv1.Money{
				CurrencyCode: "USD",
				Units:        "0",
			},
			OutstandingAmount: &llmv1.Money{
				CurrencyCode: "USD",
				Units:        fmt.Sprintf("%d", billedUnits),
			},
		},
	}, nil
}

func (s *billingService) ListInvoices(ctx context.Context, req *llmv1.ListInvoicesRequest) (*llmv1.ListInvoicesResponse, error) {
	if err := requireTenantRead(ctx, req.GetTenantId()); err != nil {
		return nil, err
	}

	return &llmv1.ListInvoicesResponse{
		Invoices: []*llmv1.Invoice{},
		Page: &llmv1.PageResponse{
			TotalSize: 0,
		},
	}, nil
}

func (s *billingService) GetInvoice(ctx context.Context, req *llmv1.GetInvoiceRequest) (*llmv1.GetInvoiceResponse, error) {
	return nil, twirp.NotFoundError(fmt.Sprintf("invoice %s not found", req.GetInvoiceId()))
}

type auditService struct {
	repo *repository
}

func (s *auditService) ListAuditEvents(ctx context.Context, req *llmv1.ListAuditEventsRequest) (*llmv1.ListAuditEventsResponse, error) {
	if req.GetTenantId() != "" {
		if err := requirePolicyList(ctx, req.GetTenantId(), req.GetProjectId()); err != nil {
			return nil, err
		}
	} else {
		if _, err := requireSystemAdmin(ctx); err != nil {
			return nil, err
		}
	}

	filter := bson.M{}
	if req.GetTenantId() != "" {
		filter["tenant_id"] = req.GetTenantId()
	}
	if req.GetProjectId() != "" {
		filter["project_id"] = req.GetProjectId()
	}
	if req.GetActorId() != "" {
		filter["actor_id"] = req.GetActorId()
	}
	if req.GetTargetResourceType() != "" {
		filter["target_resource_type"] = req.GetTargetResourceType()
	}
	if req.GetTargetResourceId() != "" {
		filter["target_resource_id"] = req.GetTargetResourceId()
	}
	if req.GetAction() != llmv1.AuditAction_AUDIT_ACTION_UNSPECIFIED {
		filter["action"] = int32(req.GetAction())
	}
	if req.GetOutcome() != llmv1.AuditOutcome_AUDIT_OUTCOME_UNSPECIFIED {
		filter["outcome"] = int32(req.GetOutcome())
	}
	if req.GetTimeRange() != nil {
		timeFilter := bson.M{}
		if req.GetTimeRange().GetStartTime() != nil && !req.GetTimeRange().GetStartTime().AsTime().IsZero() {
			timeFilter["$gte"] = req.GetTimeRange().GetStartTime().AsTime()
		}
		if req.GetTimeRange().GetEndTime() != nil && !req.GetTimeRange().GetEndTime().AsTime().IsZero() {
			timeFilter["$lte"] = req.GetTimeRange().GetEndTime().AsTime()
		}
		if len(timeFilter) > 0 {
			filter["occurred_at"] = timeFilter
		}
	}

	findOptions := options.Find().SetLimit(int64(normalizePageSize(req.GetPage())))
	cursor, err := s.repo.backend.collection(auditEventsCollection).Find(ctx, filter, findOptions)
	if err != nil {
		return nil, twirp.InternalErrorWith(fmt.Errorf("find audit events: %w", err))
	}
	defer cursor.Close(ctx)

	var docs []bson.M
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, twirp.InternalErrorWith(fmt.Errorf("decode audit events: %w", err))
	}

	events := make([]*llmv1.AuditEvent, 0, len(docs))
	for _, doc := range docs {
		events = append(events, &llmv1.AuditEvent{
			Id:                 toString(doc["_id"]),
			TenantId:           toString(doc["tenant_id"]),
			ProjectId:          toString(doc["project_id"]),
			ActorId:            toString(doc["actor_id"]),
			ActorType:          toString(doc["actor_type"]),
			Action:             llmv1.AuditAction(toInt32(doc["action"])),
			Outcome:            llmv1.AuditOutcome(toInt32(doc["outcome"])),
			TargetResourceType: toString(doc["target_resource_type"]),
			TargetResourceId:   toString(doc["target_resource_id"]),
			RequestId:          toString(doc["request_id"]),
			Message:            toString(doc["message"]),
			OccurredAt:         toTimestamp(doc["occurred_at"]),
		})
	}

	return &llmv1.ListAuditEventsResponse{
		AuditEvents: events,
		Page: &llmv1.PageResponse{
			TotalSize: int64(len(events)),
		},
	}, nil
}

func (s *auditService) GetRequestTrace(ctx context.Context, req *llmv1.GetRequestTraceRequest) (*llmv1.GetRequestTraceResponse, error) {
	var doc bson.M
	err := s.repo.backend.collection(requestTracesCollection).FindOne(ctx, bson.M{"_id": req.GetRequestId()}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, twirp.NotFoundError(fmt.Sprintf("request trace %s not found", req.GetRequestId()))
		}
		return nil, twirp.InternalErrorWith(fmt.Errorf("find request trace: %w", err))
	}

	requestTrace := &llmv1.RequestTrace{
		RequestId:       toString(doc["request_id"]),
		TenantId:        toString(doc["tenant_id"]),
		ProjectId:       toString(doc["project_id"]),
		GatewayKeyId:    toString(doc["gateway_key_id"]),
		LogicalModelId:  toString(doc["logical_model_id"]),
		VendorId:        toString(doc["vendor_id"]),
		UpstreamModel:   toString(doc["upstream_model"]),
		Status:          llmv1.ResourceStatus(toInt32(doc["status"])),
		FailureCategory: toString(doc["failure_category"]),
		FailureMessage:  toString(doc["failure_message"]),
		LatencyMs:       toInt64(doc["latency_ms"]),
		TotalTokens:     toInt64(doc["total_tokens"]),
		StartedAt:       toTimestamp(doc["started_at"]),
		CompletedAt:     toTimestamp(doc["completed_at"]),
	}

	if requestTrace.GetTenantId() != "" {
		if err := requirePolicyList(ctx, requestTrace.GetTenantId(), requestTrace.GetProjectId()); err != nil {
			return nil, err
		}
	}

	return &llmv1.GetRequestTraceResponse{RequestTrace: requestTrace}, nil
}

func (s *auditService) CreateExportJob(ctx context.Context, req *llmv1.CreateExportJobRequest) (*llmv1.CreateExportJobResponse, error) {
	if req.GetExportJob() == nil {
		return nil, twirp.RequiredArgumentError("export_job")
	}
	if req.GetExportJob().GetTenantId() == "" {
		return nil, twirp.InvalidArgumentError("export_job.tenant_id", "tenant id is required")
	}
	act, err := requirePolicyWrite(ctx, req.GetExportJob().GetTenantId(), req.GetExportJob().GetProjectId())
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	job := req.GetExportJob()
	exportJobID := firstNonEmpty(job.GetId(), uuid.NewString())
	doc := bson.M{
		"_id":          exportJobID,
		"tenant_id":    job.GetTenantId(),
		"project_id":   job.GetProjectId(),
		"dataset":      job.GetDataset(),
		"time_range":   job.GetTimeRange(),
		"status":       int32(llmv1.ExportJobStatus_EXPORT_JOB_STATUS_PENDING),
		"download_url": "",
		"format":       firstNonEmpty(job.GetFormat(), "jsonl"),
		"expires_at":   now.Add(24 * time.Hour),
		"created_by":   act.ID,
		"created_at":   now,
		"updated_by":   act.ID,
		"updated_at":   now,
	}
	if _, err := s.repo.backend.collection(exportJobsCollection).InsertOne(ctx, doc); err != nil {
		return nil, twirp.InternalErrorWith(fmt.Errorf("insert export job: %w", err))
	}

	created := exportJobFromDoc(doc)
	_ = s.repo.backend.collection(exportJobsCollection).FindOne(ctx, bson.M{"_id": exportJobID}).Decode(&doc)
	created = exportJobFromDoc(doc)

	_ = s.repo.backend.cacheDelete(ctx, s.repo.backend.cacheKey("export_job", exportJobID))
	return &llmv1.CreateExportJobResponse{ExportJob: created}, nil
}

func (s *auditService) GetExportJob(ctx context.Context, req *llmv1.GetExportJobRequest) (*llmv1.GetExportJobResponse, error) {
	var doc bson.M
	if err := s.repo.backend.collection(exportJobsCollection).FindOne(ctx, bson.M{"_id": req.GetExportJobId()}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, twirp.NotFoundError(fmt.Sprintf("export job %s not found", req.GetExportJobId()))
		}
		return nil, twirp.InternalErrorWith(fmt.Errorf("find export job: %w", err))
	}

	job := exportJobFromDoc(doc)
	if err := requirePolicyList(ctx, job.GetTenantId(), job.GetProjectId()); err != nil {
		return nil, err
	}

	return &llmv1.GetExportJobResponse{ExportJob: job}, nil
}

func (s *auditService) ListExportJobs(ctx context.Context, req *llmv1.ListExportJobsRequest) (*llmv1.ListExportJobsResponse, error) {
	if err := requirePolicyList(ctx, req.GetTenantId(), req.GetProjectId()); err != nil {
		return nil, err
	}

	filter := bson.M{}
	if req.GetTenantId() != "" {
		filter["tenant_id"] = req.GetTenantId()
	}
	if req.GetProjectId() != "" {
		filter["project_id"] = req.GetProjectId()
	}
	if req.GetDataset() != "" {
		filter["dataset"] = req.GetDataset()
	}
	if req.GetStatus() != llmv1.ExportJobStatus_EXPORT_JOB_STATUS_UNSPECIFIED {
		filter["status"] = int32(req.GetStatus())
	}

	findOptions := options.Find().SetLimit(int64(normalizePageSize(req.GetPage())))
	cursor, err := s.repo.backend.collection(exportJobsCollection).Find(ctx, filter, findOptions)
	if err != nil {
		return nil, twirp.InternalErrorWith(fmt.Errorf("find export jobs: %w", err))
	}
	defer cursor.Close(ctx)

	var docs []bson.M
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, twirp.InternalErrorWith(fmt.Errorf("decode export jobs: %w", err))
	}

	jobs := make([]*llmv1.ExportJob, 0, len(docs))
	for _, doc := range docs {
		jobs = append(jobs, exportJobFromDoc(doc))
	}

	return &llmv1.ListExportJobsResponse{
		ExportJobs: jobs,
		Page: &llmv1.PageResponse{
			TotalSize: int64(len(jobs)),
		},
	}, nil
}

func updateMaskPaths(mask *fieldmaskpb.FieldMask) []string {
	if mask == nil {
		return nil
	}

	return mask.GetPaths()
}

func exportJobFromDoc(doc bson.M) *llmv1.ExportJob {
	return &llmv1.ExportJob{
		Id:          toString(doc["_id"]),
		TenantId:    toString(doc["tenant_id"]),
		ProjectId:   toString(doc["project_id"]),
		Dataset:     toString(doc["dataset"]),
		Status:      llmv1.ExportJobStatus(toInt32(doc["status"])),
		DownloadUrl: toString(doc["download_url"]),
		Format:      toString(doc["format"]),
		ExpiresAt:   toTimestamp(doc["expires_at"]),
		AuditInfo: &llmv1.AuditInfo{
			CreatedBy: toString(doc["created_by"]),
			CreatedAt: toTimestamp(doc["created_at"]),
			UpdatedBy: toString(doc["updated_by"]),
			UpdatedAt: toTimestamp(doc["updated_at"]),
		},
	}
}

func usageFilter(tenantID, projectID, gatewayKeyID, logicalModelID, vendorID string, timeRange *llmv1.TimeRange) bson.M {
	filter := bson.M{}
	if tenantID != "" {
		filter["tenant_id"] = tenantID
	}
	if projectID != "" {
		filter["project_id"] = projectID
	}
	if gatewayKeyID != "" {
		filter["gateway_key_id"] = gatewayKeyID
	}
	if logicalModelID != "" {
		filter["logical_model_id"] = logicalModelID
	}
	if vendorID != "" {
		filter["vendor_id"] = vendorID
	}
	if timeRange != nil {
		timeFilter := bson.M{}
		if timeRange.GetStartTime() != nil && !timeRange.GetStartTime().AsTime().IsZero() {
			timeFilter["$gte"] = timeRange.GetStartTime().AsTime()
		}
		if timeRange.GetEndTime() != nil && !timeRange.GetEndTime().AsTime().IsZero() {
			timeFilter["$lte"] = timeRange.GetEndTime().AsTime()
		}
		if len(timeFilter) > 0 {
			filter["completed_at"] = timeFilter
		}
	}
	return filter
}

func usageRecordFromDoc(doc bson.M) *llmv1.UsageRecord {
	return &llmv1.UsageRecord{
		Id:                toString(doc["_id"]),
		TenantId:          toString(doc["tenant_id"]),
		ProjectId:         toString(doc["project_id"]),
		GatewayKeyId:      toString(doc["gateway_key_id"]),
		LogicalModelId:    toString(doc["logical_model_id"]),
		VendorId:          toString(doc["vendor_id"]),
		UpstreamModel:     toString(doc["upstream_model"]),
		RequestId:         toString(doc["request_id"]),
		PromptTokens:      toInt64(doc["prompt_tokens"]),
		CompletionTokens:  toInt64(doc["completion_tokens"]),
		TotalTokens:       toInt64(doc["total_tokens"]),
		BillableUnits:     toInt64(doc["billable_units"]),
		PricingSnapshotId: toString(doc["pricing_snapshot_id"]),
		StartedAt:         toTimestamp(doc["started_at"]),
		CompletedAt:       toTimestamp(doc["completed_at"]),
		Status:            llmv1.ResourceStatus(toInt32(doc["status"])),
	}
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	switch value := v.(type) {
	case string:
		return value
	default:
		return fmt.Sprint(value)
	}
}

func toInt32(v any) int32 {
	switch value := v.(type) {
	case int32:
		return value
	case int64:
		return int32(value)
	case int:
		return int32(value)
	case float64:
		return int32(value)
	default:
		return 0
	}
}

func toInt64(v any) int64 {
	switch value := v.(type) {
	case int64:
		return value
	case int32:
		return int64(value)
	case int:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func toTimestamp(v any) *timestamppb.Timestamp {
	switch value := v.(type) {
	case time.Time:
		if value.IsZero() {
			return nil
		}
		return timestamppb.New(value)
	case bson.DateTime:
		t := value.Time()
		if t.IsZero() {
			return nil
		}
		return timestamppb.New(t)
	default:
		return nil
	}
}

func wrapTwirpHandler(server llmv1.TwirpServer, manager *Manager) llmv1.TwirpServer {
	return &twirpHandler{
		TwirpServer: server,
		manager:     manager,
	}
}

type twirpHandler struct {
	llmv1.TwirpServer
	manager *Manager
}

func (h *twirpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, err := twirp.WithHTTPRequestHeaders(r.Context(), r.Header.Clone())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ctx = context.WithValue(ctx, actorContextKey, actorFromHeaders(r.Header))
	capture := &statusCaptureWriter{ResponseWriter: w, status: http.StatusOK}
	h.TwirpServer.ServeHTTP(capture, r.WithContext(ctx))

	if h.manager != nil && h.manager.Enabled() {
		act := actorFromHeaders(r.Header)
		action := auditActionFromTwirpRequest(r)
		outcome := llmv1.AuditOutcome_AUDIT_OUTCOME_SUCCEEDED
		if capture.status >= http.StatusBadRequest {
			outcome = llmv1.AuditOutcome_AUDIT_OUTCOME_FAILED
		}
		targetType, targetID := auditTargetFromTwirpPath(r.URL.Path)
		_ = h.manager.RecordAuditEvent(ctx, AuditEventInput{
			TenantID:           act.TenantID,
			ProjectID:          act.ProjectID,
			ActorID:            act.ID,
			ActorType:          act.Type,
			Action:             action,
			Outcome:            outcome,
			TargetResourceType: targetType,
			TargetResourceID:   targetID,
			RequestID:          firstNonEmpty(r.Header.Get("X-Request-Id"), r.Header.Get("x-request-id")),
			Message:            fmt.Sprintf("%s %s", r.Method, r.URL.Path),
			Metadata:           map[string]string{"http_status": fmt.Sprintf("%d", capture.status)},
		})
	}
}

type statusCaptureWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusCaptureWriter) WriteHeader(statusCode int) {
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func auditActionFromTwirpRequest(r *http.Request) llmv1.AuditAction {
	path := strings.ToLower(r.URL.Path)
	switch {
	case strings.Contains(path, "rotate"):
		return llmv1.AuditAction_AUDIT_ACTION_ROTATED
	case strings.Contains(path, "revoke"):
		return llmv1.AuditAction_AUDIT_ACTION_REVOKED
	case strings.Contains(path, "archive"):
		return llmv1.AuditAction_AUDIT_ACTION_ARCHIVED
	case strings.Contains(path, "suspend"):
		return llmv1.AuditAction_AUDIT_ACTION_SUSPENDED
	case strings.Contains(path, "create"), r.Method == http.MethodPost:
		return llmv1.AuditAction_AUDIT_ACTION_CREATED
	case strings.Contains(path, "update"), r.Method == http.MethodPatch:
		return llmv1.AuditAction_AUDIT_ACTION_UPDATED
	case strings.Contains(path, "delete"), r.Method == http.MethodDelete:
		return llmv1.AuditAction_AUDIT_ACTION_DELETED
	default:
		return llmv1.AuditAction_AUDIT_ACTION_UPDATED
	}
}

func auditTargetFromTwirpPath(path string) (string, string) {
	trimmed := strings.Trim(path, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 3 {
		return "twirp", trimmed
	}
	service := parts[len(parts)-2]
	method := parts[len(parts)-1]
	return service, method
}

func actorFromContext(ctx context.Context) (*actor, error) {
	value := ctx.Value(actorContextKey)
	act, ok := value.(*actor)
	if !ok || act == nil {
		return nil, twirp.Unauthenticated.Error("missing actor context")
	}
	if act.ID == "" && !act.SystemAdmin {
		return nil, twirp.Unauthenticated.Error("missing actor identity")
	}

	return act, nil
}

func actorFromHeaders(headers http.Header) *actor {
	return &actor{
		ID:           headers.Get("X-Actor-Id"),
		Type:         firstNonEmpty(headers.Get("X-Actor-Type"), "user"),
		TenantID:     headers.Get("X-Tenant-Id"),
		ProjectID:    headers.Get("X-Project-Id"),
		SystemAdmin:  strings.EqualFold(headers.Get("X-Actor-System-Admin"), "true"),
		TenantRoles:  parseRoles(headers.Get("X-Tenant-Roles")),
		ProjectRoles: parseRoles(headers.Get("X-Project-Roles")),
	}
}

func requireActor(ctx context.Context) (*actor, error) {
	return actorFromContext(ctx)
}

func requireSystemAdmin(ctx context.Context) (*actor, error) {
	act, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if !act.SystemAdmin {
		return nil, twirp.PermissionDenied.Error("system admin role required")
	}

	return act, nil
}

func requireTenantRead(ctx context.Context, tenantID string) error {
	act, err := actorFromContext(ctx)
	if err != nil {
		return err
	}
	if act.SystemAdmin {
		return nil
	}
	if tenantID == "" {
		return twirp.InvalidArgumentError("tenant_id", "tenant id is required")
	}
	if act.TenantID != tenantID {
		return twirp.PermissionDenied.Error("tenant scope mismatch")
	}
	if !hasAnyRole(act.TenantRoles) {
		return twirp.PermissionDenied.Error("tenant membership required")
	}

	return nil
}

func requireTenantWrite(ctx context.Context, tenantID string) (*actor, error) {
	act, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if act.SystemAdmin {
		return act, nil
	}
	if act.TenantID != tenantID {
		return nil, twirp.PermissionDenied.Error("tenant scope mismatch")
	}
	if !hasAdminRole(act.TenantRoles) {
		return nil, twirp.PermissionDenied.Error("tenant admin role required")
	}

	return act, nil
}

func requireProjectRead(ctx context.Context, project *llmv1.Project) error {
	act, err := actorFromContext(ctx)
	if err != nil {
		return err
	}
	if act.SystemAdmin {
		return nil
	}
	if act.TenantID == project.GetTenantId() && hasAnyRole(act.TenantRoles) {
		return nil
	}
	if act.ProjectID == project.GetId() && hasAnyRole(act.ProjectRoles) {
		return nil
	}

	return twirp.PermissionDenied.Error("project read access denied")
}

func requireProjectWrite(ctx context.Context, project *llmv1.Project) (*actor, error) {
	act, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if act.SystemAdmin {
		return act, nil
	}
	if act.TenantID == project.GetTenantId() && hasAdminRole(act.TenantRoles) {
		return act, nil
	}
	if act.ProjectID == project.GetId() && hasAdminRole(act.ProjectRoles) {
		return act, nil
	}

	return nil, twirp.PermissionDenied.Error("project admin role required")
}

func requireMembershipRead(ctx context.Context, tenantID, projectID string) error {
	if projectID != "" {
		project := &llmv1.Project{Id: projectID, TenantId: tenantID}
		return requireProjectRead(ctx, project)
	}

	return requireTenantRead(ctx, tenantID)
}

func requireMembershipWrite(ctx context.Context, tenantID, projectID string) (*actor, error) {
	act, err := actorFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if act.SystemAdmin {
		return act, nil
	}
	if act.TenantID == tenantID && hasAdminRole(act.TenantRoles) {
		return act, nil
	}
	if projectID != "" && act.ProjectID == projectID && hasAdminRole(act.ProjectRoles) {
		return act, nil
	}

	return nil, twirp.PermissionDenied.Error("membership admin role required")
}

func requireGatewayKeyRead(ctx context.Context, key *llmv1.GatewayKey) error {
	if key.GetProjectId() != "" {
		return requireProjectRead(ctx, &llmv1.Project{
			Id:       key.GetProjectId(),
			TenantId: key.GetTenantId(),
		})
	}

	return requireTenantRead(ctx, key.GetTenantId())
}

func requireGatewayKeyWrite(ctx context.Context, key *llmv1.GatewayKey) (*actor, error) {
	if key.GetProjectId() != "" {
		project := &llmv1.Project{
			Id:       key.GetProjectId(),
			TenantId: key.GetTenantId(),
		}
		return requireProjectWrite(ctx, project)
	}

	return requireTenantWrite(ctx, key.GetTenantId())
}

func requireLogicalModelRead(ctx context.Context, model *llmv1.LogicalModel) error {
	act, err := actorFromContext(ctx)
	if err != nil {
		return err
	}
	if act.SystemAdmin {
		return nil
	}
	if model.GetVisibility() == llmv1.ModelVisibility_MODEL_VISIBILITY_PUBLIC {
		return nil
	}
	if act.TenantID == "" {
		return twirp.PermissionDenied.Error("tenant scope required")
	}
	if model.GetVisibility() == llmv1.ModelVisibility_MODEL_VISIBILITY_TENANT && hasString(model.GetAllowedTenantIds(), act.TenantID) {
		return nil
	}
	if model.GetVisibility() == llmv1.ModelVisibility_MODEL_VISIBILITY_PRIVATE && hasString(model.GetAllowedTenantIds(), act.TenantID) {
		return nil
	}

	return twirp.PermissionDenied.Error("logical model access denied")
}

func requireRoutingPolicyRead(ctx context.Context, policy *llmv1.RoutingPolicy) error {
	if policy.GetTenantId() == "" {
		_, err := requireSystemAdmin(ctx)
		return err
	}
	if policy.GetProjectId() != "" {
		return requireProjectRead(ctx, &llmv1.Project{
			Id:       policy.GetProjectId(),
			TenantId: policy.GetTenantId(),
		})
	}
	return requireTenantRead(ctx, policy.GetTenantId())
}

func requireRoutingPolicyList(ctx context.Context, tenantID, projectID string) error {
	if tenantID == "" {
		_, err := requireSystemAdmin(ctx)
		return err
	}
	if projectID != "" {
		return requireProjectRead(ctx, &llmv1.Project{Id: projectID, TenantId: tenantID})
	}
	return requireTenantRead(ctx, tenantID)
}

func requireRoutingPolicyWrite(ctx context.Context, tenantID, projectID string) (*actor, error) {
	if tenantID == "" {
		return requireSystemAdmin(ctx)
	}
	if projectID != "" {
		project := &llmv1.Project{Id: projectID, TenantId: tenantID}
		return requireProjectWrite(ctx, project)
	}
	return requireTenantWrite(ctx, tenantID)
}

func requirePolicyRead(ctx context.Context, tenantID, projectID string) error {
	return requireRoutingPolicyList(ctx, tenantID, projectID)
}

func requirePolicyList(ctx context.Context, tenantID, projectID string) error {
	return requireRoutingPolicyList(ctx, tenantID, projectID)
}

func requirePolicyWrite(ctx context.Context, tenantID, projectID string) (*actor, error) {
	return requireRoutingPolicyWrite(ctx, tenantID, projectID)
}

func hasString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func parseRoles(raw string) map[llmv1.MembershipRole]struct{} {
	roles := make(map[llmv1.MembershipRole]struct{})
	for _, item := range strings.Split(raw, ",") {
		role := strings.TrimSpace(strings.ToLower(item))
		switch role {
		case "owner":
			roles[llmv1.MembershipRole_MEMBERSHIP_ROLE_OWNER] = struct{}{}
		case "admin":
			roles[llmv1.MembershipRole_MEMBERSHIP_ROLE_ADMIN] = struct{}{}
		case "billing_admin", "billing-admin":
			roles[llmv1.MembershipRole_MEMBERSHIP_ROLE_BILLING_ADMIN] = struct{}{}
		case "developer":
			roles[llmv1.MembershipRole_MEMBERSHIP_ROLE_DEVELOPER] = struct{}{}
		case "viewer", "read_only", "read-only":
			roles[llmv1.MembershipRole_MEMBERSHIP_ROLE_VIEWER] = struct{}{}
		}
	}

	return roles
}

func hasAdminRole(roles map[llmv1.MembershipRole]struct{}) bool {
	_, owner := roles[llmv1.MembershipRole_MEMBERSHIP_ROLE_OWNER]
	_, admin := roles[llmv1.MembershipRole_MEMBERSHIP_ROLE_ADMIN]
	return owner || admin
}

func hasAnyRole(roles map[llmv1.MembershipRole]struct{}) bool {
	return len(roles) > 0
}

func ensureAuditInfo(audit *llmv1.AuditInfo, actorID string, create bool) *llmv1.AuditInfo {
	if audit == nil {
		audit = &llmv1.AuditInfo{}
	}
	now := timestamppb.Now()
	if create {
		audit.CreatedBy = actorID
		audit.CreatedAt = now
	}
	audit.UpdatedBy = actorID
	audit.UpdatedAt = now

	return audit
}

func initControlPlaneManager(conf *config.Config) *Manager {
	manager := NewManager(conf)
	if err := manager.Initialize(); err != nil {
		slog.Error("Failed to initialize control plane manager", "error", err)
		return nil
	}

	return manager
}

func shutdownControlPlaneManager(manager *Manager) {
	if manager == nil || manager.backend == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := manager.backend.Close(ctx); err != nil {
		slog.Error("Failed to close control plane manager", "error", err)
	}
}
