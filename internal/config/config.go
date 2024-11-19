package config

import (
	"strings"

	"github.com/spf13/viper"
)

const (
	defaultEndpoint = "https://api.openai.com"
)

type Config struct {
	OpenAIKey      string `mapstructure:"OPENAI_KEY"`
	OpenAIEndpoint string `mapstructure:"OPENAI_ENDPOINT"`
	ModelOverride  string `mapstructure:"MODEL_OVERRIDE"`
}

func New() (*Config, error) {
	path := "."
	viper.AddConfigPath(path)
	viper.SetConfigName("app")

	viper.SetConfigType("env")
	viper.AutomaticEnv()
	// viper.ReadInConfig()
	var config Config
	err := viper.Unmarshal(&config)
	if err != nil {
		return nil, err
	}
	if config.OpenAIEndpoint == "" {
		config.OpenAIEndpoint = defaultEndpoint
	}
	config.OpenAIEndpoint = strings.TrimSuffix(config.OpenAIEndpoint, "/")
	return &config, nil
}
