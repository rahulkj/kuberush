package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type HealthConfig struct {
	ListenAddress string `yaml:"listen_address"`
}

type Config struct {
	Health HealthConfig `yaml:"health"`
}

func Default() *Config {
	return &Config{
		Health: HealthConfig{
			ListenAddress: ":8080",
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}
