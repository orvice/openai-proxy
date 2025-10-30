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
	Name       string
	Host       string
	HideModels bool
	Key        string
	Keys       []string
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
