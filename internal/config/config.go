package config

import (
	"os"
)

var (
	Conf = new(Config)
)

type Config struct {
	GoogleAIAPIKey string   `yaml:"googleAIAPIKey"`
	Models         []Model  `yaml:"models"`
	Vendors        []Vendor `yaml:"vendors"`
	DefaultVendor  string   `yaml:"defaultVendor"`

	WorkflowVender string `yaml:"workflowVender"`
}

func (Config) Print() {
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
