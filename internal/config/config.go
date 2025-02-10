package config

import (
	"log/slog"
	"os"

	"github.com/spf13/viper"
)

type Config struct {
	Models        []Model  `mapstructure:"MODELS"`
	Vendors       []Vendor `mapstructure:"VENDORS"`
	DefaultVendor string   `mapstructure:"DEFAULT_VENDOR"`
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

var (
	defaultConfig *Config
)

const (
	defaultEndpoint = "https://api.openai.com"
)

func (c Config) GetDefaultVendor() Vendor {
	return Vendor{
		Host: defaultEndpoint,
		Key:  os.Getenv("OPENAI_KEY"),
	}
}

func Get() *Config {
	if defaultConfig == nil {
		defaultConfig, _ = New()
	}
	return defaultConfig
}

const (
	defaultConfigPath = "/app/config"
	configPathEnv     = "CONFIG_PATH"
)

func New() (*Config, error) {
	// Get config path from environment variable, fallback to default
	configPath := viper.GetString(configPathEnv)
	if configPath == "" {
		configPath = defaultConfigPath
	}

	slog.Default().Info("reading config file",
		"path", configPath)

	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")
	viper.AutomaticEnv()

	var config Config

	// Read configuration file
	if err := viper.ReadInConfig(); err != nil {
		slog.Error("read config error", "error", err)
		return nil, err
	}

	if err := viper.Unmarshal(&config); err != nil {
		slog.Error("read config error", "error", err)
		return nil, err
	}

	slog.Info("config readed",
		"config", config)

	return &config, nil
}
