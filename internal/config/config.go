package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type HealthConfig struct {
	ListenAddress string `yaml:"listen_address"`
}

type OTLPLogsExporterConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Endpoint string `yaml:"endpoint"` // e.g. http://localhost:4318
}

type OTLPTracesExporterConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Endpoint string `yaml:"endpoint"` // e.g. http://localhost:4318
}

type ExportersConfig struct {
	Stdout     bool                    `yaml:"stdout"`
	OTLPLogs   OTLPLogsExporterConfig  `yaml:"otlp_logs"`
	OTLPTraces OTLPTracesExporterConfig `yaml:"otlp_traces"`
}

type Config struct {
	Health    HealthConfig    `yaml:"health"`
	Exporters ExportersConfig `yaml:"exporters"`
}

func Default() *Config {
	return &Config{
		Health: HealthConfig{
			ListenAddress: ":8080",
		},
		Exporters: ExportersConfig{
			Stdout: true,
			OTLPLogs: OTLPLogsExporterConfig{
				Enabled:  false,
				Endpoint: "http://localhost:4318",
			},
			OTLPTraces: OTLPTracesExporterConfig{
				Enabled:  false,
				Endpoint: "http://localhost:4318",
			},
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}
	// Env overrides (simple)
	if v := os.Getenv("KUBERUSH_OTLP_LOGS_ENABLED"); v == "true" || v == "1" {
		cfg.Exporters.OTLPLogs.Enabled = true
	}
	if v := os.Getenv("KUBERUSH_OTLP_LOGS_ENDPOINT"); v != "" {
		cfg.Exporters.OTLPLogs.Endpoint = v
	}
	if v := os.Getenv("KUBERUSH_OTLP_TRACES_ENABLED"); v == "true" || v == "1" {
		cfg.Exporters.OTLPTraces.Enabled = true
	}
	if v := os.Getenv("KUBERUSH_OTLP_TRACES_ENDPOINT"); v != "" {
		cfg.Exporters.OTLPTraces.Endpoint = v
	}
	return cfg, nil
}
