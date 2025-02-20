package config

import (
	"os"
)

var (
	Conf = new(Config)
)

type Config struct {
	Models        []Model  `mapstructure:"MODELS"`
	Vendors       []Vendor `mapstructure:"VENDORS"`
	DefaultVendor string   `mapstructure:"DEFAULT_VENDOR"`
}

func (Config) Print() {
}

type Model struct {
	Name   string
	Slug   string
	Vendor string
}

type Vendor struct {
	Name string
	Host string
	Path string
	Key  string
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
