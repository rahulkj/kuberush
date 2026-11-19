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
	Enabled     bool              `yaml:"enabled"`
	Endpoint    string            `yaml:"endpoint"`    // e.g. http://localhost:4318
	Compression string            `yaml:"compression"` // none|gzip
	Headers     map[string]string `yaml:"headers"`
}

type OTLPTracesExporterConfig struct {
	Enabled     bool              `yaml:"enabled"`
	Endpoint    string            `yaml:"endpoint"`    // e.g. http://localhost:4318
	Compression string            `yaml:"compression"` // none|gzip
	Headers     map[string]string `yaml:"headers"`
}

type OTLPMetricsExporterConfig struct {
	Enabled     bool              `yaml:"enabled"`
	Endpoint    string            `yaml:"endpoint"`    // e.g. http://localhost:4318
	Compression string            `yaml:"compression"` // none|gzip
	Headers     map[string]string `yaml:"headers"`
}

type LokiExporterConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Endpoint string `yaml:"endpoint"` // e.g. http://localhost:3100
}

type SplunkHECExporterConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Endpoint   string `yaml:"endpoint"` // e.g. https://splunk:8088/services/collector/event
	Token      string `yaml:"token"`
	Source     string `yaml:"source"`
	Sourcetype string `yaml:"sourcetype"`
	Index      string `yaml:"index"`
}

type ElasticsearchExporterConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Endpoint string `yaml:"endpoint"` // e.g. http://localhost:9200
	Index    string `yaml:"index"`    // e.g. kuberush-logs
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type PrometheusRemoteWriteExporterConfig struct {
	Enabled  bool              `yaml:"enabled"`
	Endpoint string            `yaml:"endpoint"` // e.g. http://localhost:9009/api/v1/push
	Headers  map[string]string `yaml:"headers"`
}

type DatadogLogsExporterConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Endpoint string `yaml:"endpoint"` // e.g. https://http-intake.logs.datadoghq.com/api/v2/logs
	APIKey   string `yaml:"api_key"`
	Site     string `yaml:"site"` // optional, used to derive endpoint
	Service  string `yaml:"service"`
}

type CloudWatchLogsExporterConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Region    string `yaml:"region"`
	LogGroup  string `yaml:"log_group"`
	LogStream string `yaml:"log_stream"`
	Endpoint  string `yaml:"endpoint"` // optional custom endpoint
}

type GCPCloudLoggingExporterConfig struct {
	Enabled     bool              `yaml:"enabled"`
	ProjectID   string            `yaml:"project_id"`
	LogName     string            `yaml:"log_name"`
	Endpoint    string            `yaml:"endpoint"` // optional (https://logging.googleapis.com)
	AccessToken string            `yaml:"access_token"`
	Headers     map[string]string `yaml:"headers"`
}

type AzureMonitorLogsExporterConfig struct {
	Enabled     bool              `yaml:"enabled"`
	Endpoint    string            `yaml:"endpoint"` // Data Collection Endpoint (logs ingestion API base)
	RuleID      string            `yaml:"dcr_immutable_id"`
	StreamName  string            `yaml:"stream_name"`
	AccessToken string            `yaml:"access_token"` // Bearer token (AAD) if provided
	Headers     map[string]string `yaml:"headers"`
}

type NewRelicExporterConfig struct {
	Enabled  bool              `yaml:"enabled"`
	Endpoint string            `yaml:"endpoint"` // OTLP endpoint base (e.g. https://otlp.nr-data.net)
	APIKey   string            `yaml:"api_key"`
	Headers  map[string]string `yaml:"headers"`
}

type ExportersConfig struct {
	Stdout                bool                                `yaml:"stdout"`
	OTLPLogs              OTLPLogsExporterConfig              `yaml:"otlp_logs"`
	OTLPTraces            OTLPTracesExporterConfig            `yaml:"otlp_traces"`
	OTLPMetrics           OTLPMetricsExporterConfig           `yaml:"otlp_metrics"`
	Loki                  LokiExporterConfig                  `yaml:"loki"`
	SplunkHEC             SplunkHECExporterConfig             `yaml:"splunk_hec"`
	Elasticsearch         ElasticsearchExporterConfig         `yaml:"elasticsearch"`
	PrometheusRemoteWrite PrometheusRemoteWriteExporterConfig `yaml:"prometheus_remote_write"`
	DatadogLogs           DatadogLogsExporterConfig           `yaml:"datadog_logs"`
	CloudWatchLogs        CloudWatchLogsExporterConfig        `yaml:"cloudwatch_logs"`
	GCPCloudLogging       GCPCloudLoggingExporterConfig       `yaml:"gcp_cloud_logging"`
	AzureMonitorLogs      AzureMonitorLogsExporterConfig      `yaml:"azure_monitor_logs"`
	NewRelic              NewRelicExporterConfig              `yaml:"newrelic"`
}

type NamespacesConfig struct {
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}

type KubernetesConfig struct {
	Namespaces    NamespacesConfig `yaml:"namespaces"`
	LabelSelector string           `yaml:"label_selector"`
}

type MetricsConfig struct {
	ScrapeIntervalSeconds int      `yaml:"scrape_interval_seconds"`
	AllowMetricRegex      []string `yaml:"allow_metric_regex"`
	DropMetricRegex       []string `yaml:"drop_metric_regex"`
	AllowLabelRegex       []string `yaml:"allow_label_regex"`
	DropLabelRegex        []string `yaml:"drop_label_regex"`
	MaxSeriesPerScrape    int      `yaml:"max_series_per_scrape"`
}

type FeaturesConfig struct {
	Logs          bool `yaml:"logs"`
	MetricsScrape bool `yaml:"metrics_scrape"`
	TracesIngest  bool `yaml:"traces_ingest"`
	NodeLogTail   bool `yaml:"node_log_tail"`
}

type ShardingConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Strategy     string `yaml:"strategy"` // hashmod
	Replicas     int    `yaml:"replicas"`
	ReplicaIndex int    `yaml:"replica_index"`
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

func (k KubernetesConfig) PodAllowed(ns, name string, podLabels map[string]string, sharding ShardingConfig) bool {
	if !k.NamespaceAllowed(ns) {
		return false
	}
	if k.LabelSelector != "" {
		sel, err := labels.Parse(k.LabelSelector)
		if err != nil || !sel.Matches(labels.Set(podLabels)) {
			if err == nil {
				return false
			}
		}
	}
	if sharding.Enabled && sharding.Strategy == "hashmod" && sharding.Replicas > 0 {
		idx := sharding.ReplicaIndex
		if idx < 0 || idx >= sharding.Replicas {
			return false
		}
		h := fnv32(ns + "/" + name)
		if int(h%uint32(sharding.Replicas)) != idx {
			return false
		}
	}
	return true
}

func fnv32(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

type LogPipelineConfig struct {
	BatchSize       int    `yaml:"batch_size"`
	FlushIntervalMS int    `yaml:"flush_interval_ms"`
	QueueSize       int    `yaml:"queue_size"`
	MaxRetries      int    `yaml:"max_retries"`
	RetryBackoffMS  int    `yaml:"retry_backoff_ms"`
	DropPolicy      string `yaml:"drop_policy"` // drop_newest|drop_oldest
	DLQDir          string `yaml:"dlq_dir"`
	SpoolDir        string `yaml:"spool_dir"`
}

type Config struct {
	Health      HealthConfig      `yaml:"health"`
	Exporters   ExportersConfig   `yaml:"exporters"`
	Kubernetes  KubernetesConfig  `yaml:"kubernetes"`
	LogPipeline LogPipelineConfig `yaml:"log_pipeline"`
	Metrics     MetricsConfig     `yaml:"metrics"`
	Features    FeaturesConfig    `yaml:"features"`
	Sharding    ShardingConfig    `yaml:"sharding"`
}

func Default() *Config {
	return &Config{
		Health: HealthConfig{
			ListenAddress: ":8080",
		},
		Exporters: ExportersConfig{
			Stdout: true,
			OTLPLogs: OTLPLogsExporterConfig{
				Enabled:     false,
				Endpoint:    "http://localhost:4318",
				Compression: "none",
			},
			OTLPTraces: OTLPTracesExporterConfig{
				Enabled:     false,
				Endpoint:    "http://localhost:4318",
				Compression: "none",
			},
			OTLPMetrics: OTLPMetricsExporterConfig{
				Enabled:     false,
				Endpoint:    "http://localhost:4318",
				Compression: "none",
			},
			Loki: LokiExporterConfig{
				Enabled:  false,
				Endpoint: "http://localhost:3100",
			},
			SplunkHEC: SplunkHECExporterConfig{
				Enabled:    false,
				Endpoint:   "https://localhost:8088/services/collector/event",
				Sourcetype: "_json",
				Source:     "kuberush",
			},
			Elasticsearch: ElasticsearchExporterConfig{
				Enabled:  false,
				Endpoint: "http://localhost:9200",
				Index:    "kuberush-logs",
			},
			PrometheusRemoteWrite: PrometheusRemoteWriteExporterConfig{
				Enabled:  false,
				Endpoint: "http://localhost:9009/api/v1/push",
			},
		},
		Kubernetes: KubernetesConfig{
			Namespaces: NamespacesConfig{},
		},
		LogPipeline: LogPipelineConfig{
			BatchSize:       200,
			FlushIntervalMS: 2000,
			QueueSize:       10000,
			MaxRetries:      3,
			RetryBackoffMS:  500,
			DropPolicy:      "drop_newest",
		},
		Metrics: MetricsConfig{
			ScrapeIntervalSeconds: 30,
		},
		Features: FeaturesConfig{
			Logs:          true,
			MetricsScrape: true,
			TracesIngest:  true,
			NodeLogTail:   false,
		},
		Sharding: ShardingConfig{
			Enabled:  false,
			Strategy: "hashmod",
			Replicas: 1,
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
	if v := os.Getenv("KUBERUSH_LOKI_ENABLED"); v == "true" || v == "1" {
		cfg.Exporters.Loki.Enabled = true
	}
	if v := os.Getenv("KUBERUSH_LOKI_ENDPOINT"); v != "" {
		cfg.Exporters.Loki.Endpoint = v
	}
	if v := os.Getenv("KUBERUSH_SPLUNK_HEC_ENABLED"); v == "true" || v == "1" {
		cfg.Exporters.SplunkHEC.Enabled = true
	}
	if v := os.Getenv("KUBERUSH_SPLUNK_HEC_ENDPOINT"); v != "" {
		cfg.Exporters.SplunkHEC.Endpoint = v
	}
	if v := os.Getenv("KUBERUSH_SPLUNK_HEC_TOKEN"); v != "" {
		cfg.Exporters.SplunkHEC.Token = v
	}
	if v := os.Getenv("KUBERUSH_ELASTICSEARCH_ENABLED"); v == "true" || v == "1" {
		cfg.Exporters.Elasticsearch.Enabled = true
	}
	if v := os.Getenv("KUBERUSH_ELASTICSEARCH_ENDPOINT"); v != "" {
		cfg.Exporters.Elasticsearch.Endpoint = v
	}
	if v := os.Getenv("KUBERUSH_ELASTICSEARCH_INDEX"); v != "" {
		cfg.Exporters.Elasticsearch.Index = v
	}
	if v := os.Getenv("KUBERUSH_PROM_REMOTE_WRITE_ENABLED"); v == "true" || v == "1" {
		cfg.Exporters.PrometheusRemoteWrite.Enabled = true
	}
	if v := os.Getenv("KUBERUSH_PROM_REMOTE_WRITE_ENDPOINT"); v != "" {
		cfg.Exporters.PrometheusRemoteWrite.Endpoint = v
	}
	if v := os.Getenv("KUBERUSH_FEATURE_LOGS"); v == "false" || v == "0" {
		cfg.Features.Logs = false
	}
	if v := os.Getenv("KUBERUSH_FEATURE_METRICS_SCRAPE"); v == "false" || v == "0" {
		cfg.Features.MetricsScrape = false
	}
	if v := os.Getenv("KUBERUSH_FEATURE_TRACES_INGEST"); v == "false" || v == "0" {
		cfg.Features.TracesIngest = false
	}
	if v := os.Getenv("KUBERUSH_FEATURE_NODE_LOG_TAIL"); v == "true" || v == "1" {
		cfg.Features.NodeLogTail = true
	}
	return cfg, nil
}
