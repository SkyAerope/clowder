// Package config 负责 ~/.clowder/config.yaml 的加载、默认值填充与写回。
// 文件是唯一事实来源：面板上的任何改动都通过 Save 写回后 reload。
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config 对应 config.yaml 全量结构。
type Config struct {
	BindAddr string `yaml:"bind_addr"`
	Port     int    `yaml:"port"`
	APIKey   string `yaml:"api_key"`

	Dashboard DashboardConfig `yaml:"dashboard"`

	ProvidersDir string `yaml:"providers_dir"`

	Pool    PoolConfig    `yaml:"pool"`
	Routing RoutingConfig `yaml:"routing"`
	Backoff BackoffConfig `yaml:"backoff"`
	Capture CaptureConfig `yaml:"capture"`
}

type DashboardConfig struct {
	Public   bool   `yaml:"public"`
	Admin    bool   `yaml:"admin"`
	AdminKey string `yaml:"admin_key"`
}

type PoolConfig struct {
	MinActiveKeys              int  `yaml:"min_active_keys"`
	MaxTotalKeys               int  `yaml:"max_total_keys"`
	AutoRegister               bool `yaml:"auto_register"`
	RegisterInterval           int  `yaml:"register_interval"`
	RegisterBackoffBaseSeconds int  `yaml:"register_backoff_base_seconds"`
	RegisterBackoffMaxSeconds  int  `yaml:"register_backoff_max_seconds"` // 0 = 不设上限
}

type RoutingConfig struct {
	MaxRetries          int  `yaml:"max_retries"`
	MaxParallelAttempts int  `yaml:"max_parallel_attempts"`
	UpstreamStreaming   bool `yaml:"upstream_streaming"`
}

type BackoffConfig struct {
	BaseSeconds int `yaml:"base_seconds"`
	MaxSeconds  int `yaml:"max_seconds"` // 0 = 不设上限
}

type CaptureConfig struct {
	Redact           bool `yaml:"redact"`
	MaxRecords       int  `yaml:"max_records"`
	ResponseBufferKB int  `yaml:"response_buffer_kb"`
}

// Default 返回填好默认值的配置（api_key/admin_key 随机生成）。
func Default() *Config {
	return &Config{
		BindAddr: "0.0.0.0",
		Port:     12100,
		APIKey:   "clowder-local-" + randomToken(16),
		Dashboard: DashboardConfig{
			Public:   true,
			Admin:    true,
			AdminKey: "clowder-admin-" + randomToken(16),
		},
		ProvidersDir: "providers/",
		Pool: PoolConfig{
			MinActiveKeys:              5,
			MaxTotalKeys:               20,
			AutoRegister:               false,
			RegisterInterval:           300,
			RegisterBackoffBaseSeconds: 60,
			RegisterBackoffMaxSeconds:  3600,
		},
		Routing: RoutingConfig{
			MaxRetries:          10,
			MaxParallelAttempts: 2,
			UpstreamStreaming:   true,
		},
		Backoff: BackoffConfig{
			BaseSeconds: 5,
			MaxSeconds:  600,
		},
		Capture: CaptureConfig{
			Redact:           false,
			MaxRecords:       1000,
			ResponseBufferKB: 256,
		},
	}
}

// Dir 返回 ~/.clowder 目录（可被 CLOWDER_HOME 覆盖，便于测试）。
func Dir() (string, error) {
	if d := os.Getenv("CLOWDER_HOME"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".clowder"), nil
}

// Path 返回 config.yaml 的绝对路径。
func Path() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.yaml"), nil
}

// Load 加载配置；文件不存在时生成默认配置并写盘（首次运行体验）。
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		cfg := Default()
		if err := cfg.Save(); err != nil {
			return nil, fmt.Errorf("write default config: %w", err)
		}
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg := Default() // 以默认值为底，缺省字段不至于归零
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save 将配置写回 config.yaml（原子写：临时文件 + rename）。
func (c *Config) Save() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// ProvidersPath 解析 providers_dir：绝对路径原样返回，相对路径基于 ~/.clowder。
func (c *Config) ProvidersPath() (string, error) {
	if filepath.IsAbs(c.ProvidersDir) {
		return c.ProvidersDir, nil
	}
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, c.ProvidersDir), nil
}

func (c *Config) validate() error {
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("invalid port: %d", c.Port)
	}
	if c.Routing.MaxRetries < 0 {
		return fmt.Errorf("routing.max_retries must be >= 0")
	}
	if c.Routing.MaxParallelAttempts < 1 {
		return fmt.Errorf("routing.max_parallel_attempts must be >= 1")
	}
	return nil
}
