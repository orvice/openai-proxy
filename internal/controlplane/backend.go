package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	coremongo "butterfly.orx.me/core/store/mongo"
	coreredis "butterfly.orx.me/core/store/redis"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/orvice/aiproxy/internal/config"
)

const (
	tenantsCollection         = "tenants"
	projectsCollection        = "projects"
	membershipsCollection     = "memberships"
	gatewayKeysCollection     = "gateway_keys"
	upstreamCredsCollection   = "upstream_credentials"
	vendorsCollection         = "vendors"
	logicalModelsCollection   = "logical_models"
	routingPoliciesCollection = "routing_policies"
	quotaPoliciesCollection   = "quota_policies"
	spendPoliciesCollection   = "spend_policies"
	usageRecordsCollection    = "usage_records"
	auditEventsCollection     = "audit_events"
	requestTracesCollection   = "request_traces"
	exportJobsCollection      = "export_jobs"
)

type backend struct {
	mongoClient *mongo.Client
	database    *mongo.Database
	redisClient *redis.Client
	keyPrefix   string
	cacheTTL    time.Duration
}

func newBackend(ctx context.Context, conf config.ControlPlane) (*backend, error) {
	client := coremongo.GetClient(conf.Mongo.GetStoreKey())
	if client == nil {
		return nil, fmt.Errorf("mongo client %q is not initialized by butterfly store", conf.Mongo.GetStoreKey())
	}

	redisClient := coreredis.GetClient(conf.Redis.GetStoreKey())
	if redisClient == nil {
		return nil, fmt.Errorf("redis client %q is not initialized by butterfly store", conf.Redis.GetStoreKey())
	}
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis from butterfly store: %w", err)
	}

	return &backend{
		mongoClient: client,
		database:    client.Database(conf.Mongo.Database),
		redisClient: redisClient,
		keyPrefix:   conf.Redis.GetKeyPrefix(),
		cacheTTL:    conf.Redis.GetTTL(),
	}, nil
}

func (b *backend) Close(ctx context.Context) error {
	_ = ctx
	// mongo/redis lifecycle is owned by butterfly store init.
	return nil
}

func (b *backend) collection(name string) *mongo.Collection {
	return b.database.Collection(name)
}

func (b *backend) cacheKey(parts ...string) string {
	key := b.keyPrefix + ":controlplane"
	for _, part := range parts {
		key += ":" + part
	}

	return key
}

func (b *backend) cacheGet(ctx context.Context, key string, target any) (bool, error) {
	if b.redisClient == nil {
		return false, nil
	}

	payload, err := b.redisClient.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if err := json.Unmarshal([]byte(payload), target); err != nil {
		return false, err
	}

	return true, nil
}

func (b *backend) cacheSet(ctx context.Context, key string, value any) error {
	if b.redisClient == nil {
		return nil
	}

	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return b.redisClient.Set(ctx, key, payload, b.cacheTTL).Err()
}

func (b *backend) cacheDelete(ctx context.Context, keys ...string) error {
	if b.redisClient == nil || len(keys) == 0 {
		return nil
	}

	return b.redisClient.Del(ctx, keys...).Err()
}
