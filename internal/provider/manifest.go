// Package provider 负责 providers/ 目录扫描、manifest.yaml 解析与模型黑白名单过滤。
package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// KeySource 取值。
const (
	SourceScript = "script"
	SourceStatic = "static"
)

// KeyStrategy 取值。
const (
	StrategySequential = "sequential"
	StrategyRoundRobin = "round-robin"
)

// Manifest 对应 providers/<name>/manifest.yaml。
type Manifest struct {
	Name        string `yaml:"name"`
	DisplayName string `yaml:"display_name"`
	Description string `yaml:"description"`
	BaseURL     string `yaml:"base_url"`
	Enabled     bool   `yaml:"enabled"`
	Format      string `yaml:"format"` // openai-compatible

	KeySource   string `yaml:"key_source"`   // script | static
	KeyStrategy string `yaml:"key_strategy"` // sequential | round-robin

	ModelWhitelist []string `yaml:"model_whitelist"`
	ModelBlacklist []string `yaml:"model_blacklist"`

	Scripts ScriptsConfig `yaml:"scripts"`
	Routing RoutingPref   `yaml:"routing"`

	// Rules 为 manifest 内嵌规则，原样保留 YAML 节点，由规则引擎解析
	// （加载时自动附加 provider 作用域）。
	Rules []map[string]any `yaml:"rules"`

	// UpstreamStreaming 可覆写全局 routing.upstream_streaming；nil = 跟随全局。
	UpstreamStreaming *bool `yaml:"upstream_streaming"`

	// Dir 是 provider 目录绝对路径（加载时填充，不序列化）。
	Dir string `yaml:"-"`
}

type ScriptsConfig struct {
	Register        string `yaml:"register"`
	Check           string `yaml:"check"`
	RegisterTimeout int    `yaml:"register_timeout"` // 秒，默认 300
	CheckTimeout    int    `yaml:"check_timeout"`    // 秒，默认 60
}

type RoutingPref struct {
	Priority int  `yaml:"priority"` // 越小越优先，默认 100
	Weight   int  `yaml:"weight"`   // 同优先级下按权重，默认 1
	Hedge    bool `yaml:"hedge"`
}

// LoadManifest 解析单个 manifest.yaml 并填充默认值。
func LoadManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.yaml"))
	if err != nil {
		return nil, err
	}
	m := &Manifest{
		Enabled: true,
		Format:  "openai-compatible",
		Routing: RoutingPref{Priority: 100, Weight: 1},
	}
	if err := yaml.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	m.Dir = dir
	if m.Name == "" {
		m.Name = filepath.Base(dir)
	}
	if m.KeyStrategy == "" {
		m.KeyStrategy = StrategySequential
	}
	if m.Scripts.RegisterTimeout <= 0 {
		m.Scripts.RegisterTimeout = 300
	}
	if m.Scripts.CheckTimeout <= 0 {
		m.Scripts.CheckTimeout = 60
	}
	if m.Routing.Weight <= 0 {
		m.Routing.Weight = 1
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manifest) validate() error {
	if m.BaseURL == "" {
		return fmt.Errorf("provider %s: base_url is required", m.Name)
	}
	switch m.KeySource {
	case SourceScript:
		if m.Scripts.Register == "" {
			return fmt.Errorf("provider %s: scripts.register required for key_source=script", m.Name)
		}
	case SourceStatic:
	default:
		return fmt.Errorf("provider %s: invalid key_source %q", m.Name, m.KeySource)
	}
	switch m.KeyStrategy {
	case StrategySequential, StrategyRoundRobin:
	default:
		return fmt.Errorf("provider %s: invalid key_strategy %q", m.Name, m.KeyStrategy)
	}
	return nil
}

// FilterModels 对实时拉取的模型列表应用黑白名单。
// 白名单优先于黑名单：同时命中两者保留。
// fetched 为 nil（拉取失败）时 fallback 到白名单；无白名单返回空。
func (m *Manifest) FilterModels(fetched []string) []string {
	if fetched == nil {
		// fallback：白名单即模型列表
		return append([]string(nil), m.ModelWhitelist...)
	}
	white := toSet(m.ModelWhitelist)
	black := toSet(m.ModelBlacklist)
	var out []string
	for _, mm := range fetched {
		if len(white) > 0 {
			if white[mm] {
				out = append(out, mm) // 白名单命中即保留（优先于黑名单）
			}
			continue
		}
		if black[mm] {
			continue
		}
		out = append(out, mm)
	}
	return out
}

// StaticKeys 读取 static provider 的 keys.txt（每行一个 key，# 开头为注释）。
func (m *Manifest) StaticKeys() ([]string, error) {
	data, err := os.ReadFile(filepath.Join(m.Dir, "keys.txt"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		keys = append(keys, line)
	}
	return keys, nil
}

// ScanDir 扫描 providers/ 目录，加载所有含 manifest.yaml 的子目录。
// 单个 manifest 解析失败不致命：记入 errs，其余 provider 照常加载。
func ScanDir(dir string) (providers []*Manifest, errs []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []error{err}
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(dir, e.Name())
		if _, err := os.Stat(filepath.Join(sub, "manifest.yaml")); err != nil {
			continue
		}
		m, err := LoadManifest(sub)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		providers = append(providers, m)
	}
	return providers, errs
}

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}
