package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/labels"
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

type OTLPMetricsExporterConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Endpoint string `yaml:"endpoint"` // e.g. http://localhost:4318
}

type ExportersConfig struct {
	Stdout      bool                      `yaml:"stdout"`
	OTLPLogs    OTLPLogsExporterConfig    `yaml:"otlp_logs"`
	OTLPTraces  OTLPTracesExporterConfig  `yaml:"otlp_traces"`
	OTLPMetrics OTLPMetricsExporterConfig `yaml:"otlp_metrics"`
}

type NamespacesConfig struct {
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}

type KubernetesConfig struct {
	Namespaces    NamespacesConfig `yaml:"namespaces"`
	LabelSelector string           `yaml:"label_selector"`
}

func (k KubernetesConfig) NamespaceAllowed(ns string) bool {
	if len(k.Namespaces.Include) > 0 {
		found := false
		for _, n := range k.Namespaces.Include {
			if n == ns {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, n := range k.Namespaces.Exclude {
		if n == ns {
			return false
		}
	}
	return true
}

func (k KubernetesConfig) PodAllowed(ns string, podLabels map[string]string) bool {
	if !k.NamespaceAllowed(ns) {
		return false
	}
	if k.LabelSelector == "" {
		return true
	}
	sel, err := labels.Parse(k.LabelSelector)
	if err != nil {
		return true
	}
	return sel.Matches(labels.Set(podLabels))
}

type LogPipelineConfig struct {
	BatchSize       int `yaml:"batch_size"`
	FlushIntervalMS int `yaml:"flush_interval_ms"`
	QueueSize       int `yaml:"queue_size"`
}

type Config struct {
	Health      HealthConfig      `yaml:"health"`
	Exporters   ExportersConfig   `yaml:"exporters"`
	Kubernetes  KubernetesConfig  `yaml:"kubernetes"`
	LogPipeline LogPipelineConfig `yaml:"log_pipeline"`
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
			OTLPMetrics: OTLPMetricsExporterConfig{
				Enabled:  false,
				Endpoint: "http://localhost:4318",
			},
		},
		Kubernetes: KubernetesConfig{
			Namespaces: NamespacesConfig{},
		},
		LogPipeline: LogPipelineConfig{
			BatchSize:       200,
			FlushIntervalMS: 2000,
			QueueSize:       10000,
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
	if v := os.Getenv("KUBERUSH_OTLP_METRICS_ENABLED"); v == "true" || v == "1" {
		cfg.Exporters.OTLPMetrics.Enabled = true
	}
	if v := os.Getenv("KUBERUSH_OTLP_METRICS_ENDPOINT"); v != "" {
		cfg.Exporters.OTLPMetrics.Endpoint = v
	}
	return cfg, nil
}
