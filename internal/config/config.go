package config

import (
	"os"
	"time"
)

var (
	Conf = new(Config)
)

type Config struct {
	GoogleAIAPIKey   string       `yaml:"googleAIAPIKey"`
	Models           []Model      `yaml:"models"`
	Vendors          []Vendor     `yaml:"vendors"`
	DefaultVendor    string       `yaml:"defaultVendor"`
	MCPServers       []MCPServer  `yaml:"mcpServers"`
	DefaultMCPServer string       `yaml:"defaultMcpServer"`
	ControlPlane     ControlPlane `yaml:"controlPlane"`

	WorkflowVender string `yaml:"workflowVender"`
}

type ControlPlane struct {
	Enabled bool        `yaml:"enabled"`
	Mongo   MongoConfig `yaml:"mongo"`
	Redis   RedisConfig `yaml:"redis"`
}

type MongoConfig struct {
	URI            string `yaml:"uri"`
	Database       string `yaml:"database"`
	ConnectTimeout string `yaml:"connectTimeout"`
}

type RedisConfig struct {
	Addr      string `yaml:"addr"`
	Password  string `yaml:"password"`
	DB        int    `yaml:"db"`
	KeyPrefix string `yaml:"keyPrefix"`
	TTL       string `yaml:"ttl"`
}

func (Config) Print() {
}

func (c ControlPlane) IsEnabled() bool {
	return c.Enabled || (c.Mongo.URI != "" && c.Mongo.Database != "" && c.Redis.Addr != "")
}

func (c MongoConfig) GetConnectTimeout() time.Duration {
	if c.ConnectTimeout == "" {
		return 5 * time.Second
	}

	timeout, err := time.ParseDuration(c.ConnectTimeout)
	if err != nil {
		return 5 * time.Second
	}

	return timeout
}

func (c RedisConfig) GetTTL() time.Duration {
	if c.TTL == "" {
		return 5 * time.Minute
	}

	ttl, err := time.ParseDuration(c.TTL)
	if err != nil {
		return 5 * time.Minute
	}

	return ttl
}

func (c RedisConfig) GetKeyPrefix() string {
	if c.KeyPrefix == "" {
		return "aiproxy"
	}

	return c.KeyPrefix
}

type Model struct {
	Name   string
	Regex  string
	Slug   string
	Vendor string
}

type Vendor struct {
	Name         string   `yaml:"name"`
	Host         string   `yaml:"host"`
	HideModels   bool     `yaml:"hideModels"`
	Key          string   `yaml:"key"`
	Keys         []string `yaml:"keys"`
	DefaultModel string   `yaml:"defaultModel"`
}

type MCPServer struct {
	Name    string            `yaml:"name"`
	Host    string            `yaml:"host"`
	Key     string            `yaml:"key"`
	Headers map[string]string `yaml:"headers"`
}

const (
	defaultEndpoint = "https://api.openai.com"
)

func (c Config) GetDefaultVendor() Vendor {
	return Vendor{
		Host: defaultEndpoint,
		Key:  os.Getenv("OPENAI_KEY"),
	}
}

func (c Config) GetWorkflowVender() Vendor {
	for _, v := range c.Vendors {
		if v.Name == c.WorkflowVender {
			return v
		}
	}
	return Vendor{
		Host: defaultEndpoint,
		Key:  os.Getenv("OPENAI_KEY"),
	}
}
