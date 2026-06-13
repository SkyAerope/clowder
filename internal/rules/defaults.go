package rules

import (
	"os"
	"regexp"
)

// BuiltinRules 内置规则：不可删、不可改，无 capture。
// transport 基础设施故障 → provider 级 unhealthy + next_provider。
// priority 65535：传输故障与状态码规则互斥，不与其他规则竞争，固定为最后。
func BuiltinRules() []*Rule {
	mk := func(name, transport string) *Rule {
		return &Rule{
			Name:     name,
			Priority: 65535,
			Source:   SourceBuiltin,
			Builtin:  true,
			When:     When{Transport: transport},
			Do:       []Action{{Kind: "mark_provider_unhealthy"}},
			Route:    RouteSpec{Single: RouteNextProvider},
		}
	}
	return []*Rule{
		mk("builtin-dns-failure", TransportDNSFailure),
		mk("builtin-conn-refused", TransportConnRefused),
		mk("builtin-tls-error", TransportTLSError),
	}
}

// DefaultRulesYAML 随发布写入 rules.yaml 的默认规则（可删可改）。
const DefaultRulesYAML = `# clowder 规则文件（全局）。管理面板规则页可视化编辑此文件。
# 规则结构：name + when（触发）+ if（可选条件）+ do（副作用）+ route（路由决策）
rules:
  - name: default-4xx-passthrough
    description: "4xx 客户端错误默认透传"
    priority: 8000
    when: { status_code: 400-499 }
    do:
      - capture: { fields: [response_headers, response_body, status_code] }
    route: passthrough

  - name: default-5xx-retry
    description: "5xx 服务端错误换 provider 重试"
    priority: 8000
    when: { status_code: 500-599 }
    do:
      - capture: { fields: [response_headers, response_body, status_code] }
    route: next_provider

  - name: default-transport-retry
    description: "任意传输错误换 provider 重试"
    priority: 8000
    when: { transport: true }
    do:
      - capture: { fields: [response_headers, response_body, status_code] }
    route: next_provider

  - name: default-ttft-timeout
    description: "TTFT 超 7s 切换 provider"
    priority: 8000
    when: { ttft_over_ms: 7000 }
    do:
      - capture: { fields: [response_headers, response_body, status_code] }
    route: next_provider

  - name: default-catch-all
    description: "兜底：任意未分类错误透传并留样本"
    priority: 10000
    when: { error: true }
    do:
      - capture: { fields: [response_headers, response_body, status_code] }
    route: passthrough
`

// PresetRulesYAML 预设规则（规则页「添加预设」入口的候选；不自动写入）。
const PresetRulesYAML = `rules:
  - name: preset-429-backoff
    description: "429 指数退避后换 key"
    when: { status_code: 429 }
    do:
      - backoff_key: {}
    route: next_key

  - name: preset-401-check-key
    description: "401 时跑 check 判定 key 状态后换 key"
    when: { status_code: 401 }
    do:
      - check_key: { chain: true }
    route: next_key

  - name: preset-on-check-invalid
    description: "check 返回 invalid 时永久禁用 key"
    when:
      check: { status: invalid }
    do:
      - mark_key: invalid

  - name: preset-on-check-rate-limited
    description: "check 返回 rate_limited 时临时退避 key"
    when:
      check: { status: rate_limited }
    do:
      - mark_key: rate_limited

  - name: preset-404-model-gone
    description: "404 标记模型不健康并刷新模型列表"
    when: { status_code: 404 }
    do:
      - mark_model_unhealthy: {}
      - refresh_models: {}
    route: next_provider
`

// EnsureUserRulesFile rules.yaml 不存在时写入默认规则。
func EnsureUserRulesFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte(DefaultRulesYAML), 0o644)
}

// LoadUserRules 加载 rules.yaml。
func LoadUserRules(path string) ([]*Rule, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseRules(data, SourceUserFile)
}

// PresetRules 解析预设规则列表。
func PresetRules() []*Rule {
	rs, err := ParseRules([]byte(PresetRulesYAML), SourceUserFile)
	if err != nil {
		panic("invalid preset rules: " + err.Error()) // 编译期常量，出错属于 bug
	}
	return rs
}

// 错误指纹归一化用的正则（包级编译一次）。
var (
	reUUID   = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	reISOTs  = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:?\d{2})?`)
	reNumber = regexp.MustCompile(`\d+`)
	reSpaces = regexp.MustCompile(`\s+`)
)
